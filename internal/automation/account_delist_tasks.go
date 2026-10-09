package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// runAutoDelist 按账号下架白名单执行每日定时下架；它只处理用户显式勾选的商品，绝不按平台列表批量下架。
// manual 为 true 表示用户手动触发，可立即抢占失败记录而不等待冷却时间。
func (c *accountTaskCoordinator) runAutoDelist(ctx context.Context, settings db.AccountTaskSettings, now time.Time, manual bool) (AccountTaskSummary, error) {
	// summary 用于本次流程后续判断的summary
	summary := AccountTaskSummary{TaskType: TaskAutoDelist}
	if c.client() == nil {
		return summary, fmt.Errorf("下架客户端未初始化")
	}
	// date 用于本次流程后续判断的日期
	date := now.Format("2006-01-02")
	// runKey 用于本次流程后续判断的运行Key
	runKey := "delist:" + settings.CookieID + ":" + date
	// run 用于本次流程后续判断的运行
	run := db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID, TaskType: TaskAutoDelist, RunDate: date}
	// claimed 用于本次流程后续判断的claimed
	var claimed bool
	// err 用于本次流程后续判断的err
	var err error
	if manual {
		claimed, err = c.repository.ClaimRunImmediately(ctx, run, time.Now().UTC().Unix())
	} else {
		claimed, err = c.repository.ClaimRun(ctx, run, time.Now().UTC().Unix())
	}
	if err != nil || !claimed {
		if !claimed && err == nil {
			summary.Skipped = 1
			summary.Message = "今天已经执行过下架"
		}
		return summary, err
	}
	summary.Found = len(settings.DelistItemIDs)
	if summary.Found == 0 {
		// emptyMessage 说明账号未勾选任何下架商品；这属于当日任务成功，不应重复请求平台。
		const emptyMessage = "未配置下架白名单商品，今日下架任务已完成"
		// completedAt 保存本次空名单成功结果对应的 UTC Unix 时间，供每日去重判断使用。
		completedAt := time.Now().UTC().Unix()
		// markErr 表示空名单成功态的当日完成标记写入失败；没有完成持久化时需要人工核对数据库状态。
		markErr := c.repository.MarkDelisted(ctx, settings.CookieID, date, completedAt)
		if markErr != nil {
			// persistenceErr 为日期索引失败补充业务上下文，避免把内部落库失败伪装成平台成功。
			persistenceErr := fmt.Errorf("保存商品下架日期: %w", markErr)
			return summary, errors.Join(persistenceErr, c.quarantineAccountTaskRun(ctx, runKey, 0, 0, persistenceErr))
		}
		c.logger.Info("每日下架完成，未配置下架商品", "account", settings.CookieID)
		// finishErr 是保存空名单成功运行状态时产生的数据库错误。
		finishErr := c.finishAccountTaskRun(ctx, runKey, "success", 0, 0, emptyMessage, 0)
		if finishErr != nil {
			return summary, finishErr
		}
		summary.Message = emptyMessage
		return summary, nil
	}
	// credential、err 保存本轮下架使用的 Cookie 会话及其读取错误。
	credential, err := c.openAccountTaskCredentialSession(ctx, settings.CookieID)
	if err != nil {
		return summary, errors.Join(err, c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix()))
	}
	// current 保存下架接口返回前可继续使用的最新 Cookie。
	current := credential.cookieValue
	// lastError 用于本次流程后续判断的last错误
	var lastError string
	// itemID 表示当前由白名单指定、需要下架的平台商品。
	for _, itemID := range settings.DelistItemIDs {
		// result、delistErr 用于本次流程后续判断的result、delistErr
		result, delistErr := c.client().DownshelfItem(credential.requestContext, current, itemID)
		if delistErr != nil || result == nil || !result.Success {
			// persistErr 保存下架失败前已收到的响应 Cookie，避免错误路径丢弃平台轮换的凭证。
			_, persistErr := c.persistTaskCookieSession(ctx, settings.CookieID, current, "", &credential)
			summary.Failed++
			lastError = errorString(delistErr)
			if result != nil && result.Message != "" {
				lastError = result.Message
			}
			// Token 内部刷新耗尽后停止当前批次，后续恢复仍由 MTOP 客户端负责。
			if mtop.IsSessionExpiredErr(delistErr) || mtop.IsMTopTokenExpiredErr(delistErr) {
				return summary, errors.Join(delistErr, persistErr, c.finishAccountTaskRun(ctx, runKey, "failed", summary.Success, summary.Failed, lastError, 0))
			}
			if persistErr != nil {
				// isolateErr 将平台失败与凭证落库失败后的运行记录收口到人工核对，避免运行永久停留在 running。
				isolateErr := c.quarantineAccountTaskRun(ctx, runKey, summary.Success, summary.Failed, persistErr)
				return summary, errors.Join(delistErr, persistErr, isolateErr)
			}
			continue
		}
		// updatedCookies 保存下架接口返回的最新 Cookie；cookieErr 表示外部动作成功后同步 Cookie 的落库错误。
		updatedCookies, cookieErr := c.persistTaskCookieSession(ctx, settings.CookieID, current, result.UpdatedCookies, &credential)
		if cookieErr != nil {
			return summary, c.quarantineAccountTaskRun(ctx, runKey, summary.Success+1, summary.Failed, cookieErr)
		}
		current = updatedCookies
		summary.Success++
	}
	// status、retryAt 用于本次流程后续判断的status、retryAt
	status, retryAt := "success", int64(0)
	if summary.Failed > 0 {
		status, retryAt = "failed", time.Now().UTC().Add(10*time.Minute).Unix()
	} else {
		// markErr 表示所有白名单商品已下架但日期索引写入失败；返回错误以便运维补偿而不伪装成成功。
		markErr := c.repository.MarkDelisted(ctx, settings.CookieID, date, time.Now().UTC().Unix())
		if markErr != nil {
			// persistenceErr 为日期索引失败补充业务上下文，便于人工核对具体失败边界。
			persistenceErr := fmt.Errorf("保存商品下架日期: %w", markErr)
			return summary, errors.Join(persistenceErr, c.quarantineAccountTaskRun(ctx, runKey, summary.Success, summary.Failed, persistenceErr))
		}
	}
	// finishErr 保存下架任务汇总结果的落库错误；失败时将运行记录隔离，避免下一次重放已完成商品。
	finishErr := c.finishAccountTaskRun(ctx, runKey, status, summary.Success, summary.Failed, lastError, retryAt)
	if finishErr != nil {
		return summary, finishErr
	}
	if summary.Failed > 0 {
		return summary, fmt.Errorf("%d 个商品下架失败: %s", summary.Failed, lastError)
	}
	return summary, nil
}

// delistDue 判断账号当天是否已到定时下架时间且尚未执行；时间格式非法时回落到默认下架时间。
func delistDue(settings db.AccountTaskSettings, now time.Time) bool {
	if settings.LastDelistDate == now.Format("2006-01-02") {
		return false
	}
	// target、err 用于本次流程后续判断的target、err
	target, err := time.Parse("15:04", settings.DelistTime)
	if err != nil {
		target, _ = time.Parse("15:04", "09:00")
	}
	return now.Hour() > target.Hour() || now.Hour() == target.Hour() && now.Minute() >= target.Minute()
}
