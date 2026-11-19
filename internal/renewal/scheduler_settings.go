package renewal

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	apirenew "xianyu-go/internal/xianyu/renew"
)

// apiRenewEnabled 读取 API Cookie 续期开关，并在设置缺失时保留安全默认值。
func (s *Scheduler) apiRenewEnabled(ctx context.Context) bool {
	if s.settingConfigured(ctx, apiCookieRenewEnabledSetting) {
		return s.settingEnabled(ctx, apiCookieRenewEnabledSetting, true)
	}
	return s.settingEnabled(ctx, cookiesRefreshEnabledSetting, true)
}

// apiRenewInterval 封装apiRenewInterval业务协调。
func (s *Scheduler) apiRenewInterval(ctx context.Context) time.Duration {
	if s.settingConfigured(ctx, apiCookieRenewIntervalSetting) {
		return s.settingInterval(ctx, apiCookieRenewIntervalSetting, apiCookieRenewInterval)
	}
	return s.settingInterval(ctx, cookiesRefreshIntervalSetting, apiCookieRenewInterval)
}

// settingConfigured 封装设置Configured业务协调。
func (s *Scheduler) settingConfigured(ctx context.Context, key string) bool {
	if s.store == nil || s.store.Settings == nil {
		return false
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	return err == nil && strings.TrimSpace(value) != ""
}

// reloadRenewalAccount 封装reloadRenewal账号业务协调。
func (s *Scheduler) reloadRenewalAccount(ctx context.Context, account db.RenewalRuntimeAccount) (db.RenewalRuntimeAccount, error) {
	// latest 是锁内重新读取的续期窄模型，用于避免批次列表中的旧 Cookie 覆盖新状态。
	latest, err := s.store.Cookies.GetRenewalRuntimeAccount(ctx, account.ID)
	if err != nil {
		return db.RenewalRuntimeAccount{}, err
	}
	return latest, nil
}

/*
reloadRenewalAccount 在每个账号续期前重新读取最新的 Cookie。
重新读取同时复核账号启用状态，避免批次快照覆盖并发更新。
repository 负责解密最小运行视图，不再展开登录用户名和密码。
续期任务仍在账号凭证锁内调用此边界。
Cookie 更新后的重启和自动化唤醒逻辑保持原有顺序。
本次切片只收窄读取字段，不改变续期请求参数。
禁用账号会由调用方根据 Enabled 字段跳过后续请求。
查询失败仍由当前任务记录失败并结束该账号处理。
后续的保存函数继续复用 UpdateRenewalCookie。
它不代表新增的业务分支或兼容行为。
窄模型中的 metadata 仍用于完整 Cookie Jar 恢复。
Cookie 明文只在本次调用链中短暂存在。
*/

// renewWriteOutcome 是续期凭证写回的结果分类。
// 调用方据此把「守卫拒绝降级写回」与「持久化真失败」拆成两种日志状态，
// 避免观测时把凭证保护误读成续期失效。
type renewWriteOutcome string

const (
	// renewWriteStored 表示新凭证已成功写入数据库。
	renewWriteStored renewWriteOutcome = "stored"
	// renewWriteRejected 表示守卫按 fail-closed 拒绝覆盖，库中凭证保持不变。
	renewWriteRejected renewWriteOutcome = "rejected"
	// renewWriteFailed 表示读取或写入数据库失败，属于真正的持久化故障。
	renewWriteFailed renewWriteOutcome = "failed"
)

// saveRenewedCookies 把续期响应 Cookie 写回账号凭证并返回写回分类。
// ctx 是本次写回的生命周期；cookieID 是账号主键；cookieStr 是响应合并后的扁平 Cookie；
// metadata 是浏览器快照。返回 renewWriteRejected 时凭证未变，调用方不得当成请求失败。
func (s *Scheduler) saveRenewedCookies(ctx context.Context, cookieID, cookieStr, metadata string) renewWriteOutcome {
	// 续期响应不经过 MTOP 签名流程，产出不含 _m_h5_tk 的 Cookie 属预期；但它会整体覆盖
	// 数据库凭证，使依赖该凭证的定时任务失去签名能力。此处只在缺失时告警，据此定位来源。
	// 库中凭证已带令牌时整体覆盖会把账号降级为无签名能力（定时任务报错、重连后必须重新登录），
	// 因此这里拒绝降级写回，保留库中更完整的凭证，由后续 MTOP 调用自行重签令牌。
	// 空值代表服务端显式删除或登出，不属于降级，必须照常写回。
	if strings.TrimSpace(cookieStr) != "" && !mtop.SignTokenPresent(cookieStr) {
		// current、readErr 是库中现有凭证明文与读取错误；读取失败时保留原行为并仅告警。
		current, readErr := s.store.Cookies.GetValue(ctx, cookieID)
		if readErr == nil && mtop.SignTokenPresent(current) {
			s.logger.Error("续期 Cookie 缺少签名令牌且库中已有令牌，拒绝降级写回",
				"account", cookieID, "source", "renewal")
			return renewWriteRejected
		}
		s.logger.Warn("续期写入的 Cookie 不含 MTOP 签名令牌", "account", cookieID, "source", "renewal")
	}
	// err 是本次凭证落库结果；失败属真实持久化故障，与守卫拒绝要分开上报。
	if err := s.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieStr, metadata, time.Now().Unix()); err != nil {
		s.logger.Warn("保存续期 Cookie 失败", "account", cookieID, "err", err)
		return renewWriteFailed
	}
	return renewWriteStored
}

// saveAndLogRenewedCredential 写回续期响应凭证，并把写回结果写入接口续期日志。
// ctx 是本次写回的生命周期；batchID 是续期批次；cookieID 是账号主键；res 是续期响应；
// updated 是发生变化的 Cookie 名；stepDetails 是本次步骤明细；metadata 是待落库的浏览器快照；
// started 是本次续期起始时间。返回 false 表示凭证未写回且已留痕，调用方必须结束本次续期。
func (s *Scheduler) saveAndLogRenewedCredential(ctx context.Context, batchID, cookieID string, res *apirenew.Result, updated, stepDetails []string, metadata string, started time.Time) bool {
	// outcome 是凭证写回分类，决定本次日志记「写回被拒」还是「请求失败」。
	outcome := s.saveRenewedCookies(ctx, cookieID, res.NewCookies, metadata)
	// logEntry 是两种失败状态共用的日志载荷，只覆盖状态与错误说明。
	logEntry := db.RenewalLog{
		BatchID:            batchID,
		CookieID:           cookieID,
		UpdatedCookieNames: updated,
		StepDetails:        strings.Join(stepDetails, " | "),
		RenewMethod:        res.RenewMethod,
		DurationMS:         time.Since(started).Milliseconds(),
		RequestCount:       res.RequestCount,
	}
	switch outcome {
	case renewWriteRejected:
		logEntry.Status = "write_rejected"
		logEntry.ErrorMessage = "响应缺少 MTOP 签名令牌，拒绝降级写回，库中凭证保持不变"
		s.addAPILog(ctx, logEntry)
		s.logger.Warn("接口续期凭证写回被拒，保留库中已有签名令牌", "account", cookieID, "method", res.RenewMethod)
		return false
	case renewWriteFailed:
		logEntry.Status = "failed"
		logEntry.ErrorMessage = "保存 Cookie 失败"
		s.addAPILog(ctx, logEntry)
		s.logger.Warn("接口续期任务失败", "account", cookieID, "method", res.RenewMethod, "err", "保存 Cookie 失败")
		return false
	}
	return true
}

// addLoginLog 封装add登录Log业务协调。
func (s *Scheduler) addLoginLog(ctx context.Context, batchID, cookieID, status, message string, updated []string, duration time.Duration) {
	_ = s.store.Renewal.AddLoginRenewLog(ctx, db.RenewalLog{
		BatchID:            batchID,
		CookieID:           cookieID,
		Status:             status,
		Message:            message,
		UpdatedCookieNames: updated,
		RenewMethod:        "loginuser.get",
		StepDetails:        fmt.Sprintf("loginuser.get status=%s message=%s updated=%d", status, message, len(updated)),
		DurationMS:         duration.Milliseconds(),
		RequestCount:       1,
	})
}

// renewStreakJustReachedThree 判断最新连续三次是否同为 want，且更早一次不是 want。
// 用于让每个连续段落只通知一次，故障持续期间不重复打扰。
func renewStreakJustReachedThree(statuses []string, want string) bool {
	if len(statuses) < 3 || statuses[0] != want || statuses[1] != want || statuses[2] != want {
		return false
	}
	return len(statuses) < 4 || statuses[3] != want
}

// addAPILog 封装addAPILog业务协调。
func (s *Scheduler) addAPILog(ctx context.Context, log db.RenewalLog) {
	if // err 用于本次流程后续判断的err
	err := s.store.Renewal.AddAPICookieRenewLog(ctx, log); err != nil {
		s.logger.Warn("记录 API Cookie 续期日志失败", "account", log.CookieID, "status", log.Status, "err", err)
		return
	}
	// notifyStatus 是参与连续三次告警判定的状态；写回被拒与请求失败分开统计，其余状态不通知。
	notifyStatus := log.Status
	if notifyStatus != "failed" && notifyStatus != "write_rejected" {
		return
	}
	if s.notifier == nil || s.store == nil || s.store.Renewal == nil {
		return
	}
	// statuses、err 用于本次流程后续判断的statuses、err
	statuses, err := s.store.Renewal.RecentAPICookieRenewStatuses(ctx, log.CookieID, 4)
	if err != nil || !renewStreakJustReachedThree(statuses, notifyStatus) {
		return
	}
	// reason 用于本次流程后续判断的原因
	reason := strings.TrimSpace(log.ErrorMessage)
	if reason == "" {
		reason = strings.TrimSpace(log.Message)
	}
	if reason == "" {
		reason = "未知错误"
	}
	// 写回被拒是凭证保护生效而非续期中断：只发提示级通知，避免与真实故障同级刷屏。
	if notifyStatus == "write_rejected" {
		s.notifier.NotifyAccountEvent(log.CookieID, "token_renewal", "info", "闲鱼 Cookie 续期写回被拒",
			fmt.Sprintf("账号 %s 的续期请求成功，但响应缺少 MTOP 签名令牌，已连续 3 次拒绝降级写回以保留库中凭证；若需刷新签名令牌请重新登录该账号", log.CookieID))
		return
	}
	s.notifier.NotifyAccountEvent(log.CookieID, "token_renewal", "warn", "闲鱼 Cookie 自动续期连续失败", fmt.Sprintf("账号 %s 的 API 自动续期已连续失败 3 次，最近错误：%s", log.CookieID, reason))
}

// cleanupExpiredLogs 按统一保留天数清理续期日志；未配置统一项时回落旧键与默认值。
func (s *Scheduler) cleanupExpiredLogs(ctx context.Context) {
	if s.store == nil || s.store.Renewal == nil {
		return
	}
	// days 优先取统一的日志保留天数，未配置时回落续期日志专用键，最后回落 10 天。
	days := s.settingInt(ctx, db.RetentionPolicyDaysKey, 0)
	if days <= 0 {
		days = s.settingInt(ctx, "renewal_log_retention_days", 10)
	}
	if // err 用于本次流程后续判断的err
	err := s.store.Renewal.CleanupLogs(ctx, days); err != nil {
		s.logger.Warn("清理续期日志失败", "err", err)
	}
}

// markSessionExpired 封装mark会话Expired业务协调。
func (s *Scheduler) markSessionExpired(cookieID string) {
	s.cooldown.MarkSessionExpired(cookieID)
}

// isSessionCooled 封装is会话Cooled业务协调。
func (s *Scheduler) isSessionCooled(cookieID string) bool {
	// ok 用于本次流程后续判断的ok
	ok, _ := s.cooldown.IsSessionCooled(cookieID)
	return ok
}

// settingEnabled 封装设置启用状态业务协调。
func (s *Scheduler) settingEnabled(ctx context.Context, key string, defaultEnabled bool) bool {
	if s.store == nil || s.store.Settings == nil {
		return defaultEnabled
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultEnabled
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return defaultEnabled
	}
}

// settingInterval 封装设置Interval业务协调。
func (s *Scheduler) settingInterval(ctx context.Context, key string, defaultInterval time.Duration) time.Duration {
	if s.store == nil || s.store.Settings == nil {
		return defaultInterval
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultInterval
	}
	value = strings.TrimSpace(value)
	if // seconds、err 用于本次流程后续判断的seconds、err
	seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if // d、err 用于本次流程后续判断的d、err
	d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	return defaultInterval
}

// settingInt 封装设置Int业务协调。
func (s *Scheduler) settingInt(ctx context.Context, key string, defaultValue int) int {
	if s.store == nil || s.store.Settings == nil {
		return defaultValue
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultValue
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return defaultValue
	}
	return n
}

// newBatchID 封装new批次ID业务协调。
func newBatchID() string {
	// b 用于本次流程后续判断的b
	var b [16]byte
	if // err 用于本次流程后续判断的err
	_, err := crand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// sleepCtx 封装sleepCtx业务协调。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	// timer 用于本次流程后续判断的定时器
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
