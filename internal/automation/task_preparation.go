package automation

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/db"
)

// resolvePaidTaskOrder 为交易系统消息回填本账号已有订单事实，避免未知角色事件因协议字段缺失而无法完成卖家核验。
// 付款与待刀成阶段按待发货订单回填；拍下（待付款）阶段按含待付款的未终结订单回填，否则拍下事件会永久缺商品事实并死信。
func (c *Center) resolvePaidTaskOrder(ctx context.Context, task Task) (Task, error) {
	if c == nil || c.store == nil || c.store.Orders == nil || (task.TriggerType != TriggerOrderCreated && task.TriggerType != TriggerOrderPaid && task.TriggerType != TriggerBargainPending) {
		return task, nil
	}
	if task.OrderID != "" {
		// order、err 保存已有订单的最小本地事实；不存在时保留事件，交给角色门禁决定是否延期。
		order, err := c.store.Orders.Get(ctx, task.OrderID)
		if errors.Is(err, db.ErrNotFound) {
			return task, nil
		}
		if err != nil {
			return task, fmt.Errorf("按订单回填自动化事实: %w", err)
		}
		return mergeOrderIntoTask(task, order), nil
	}
	if task.ChatID == "" {
		return task, nil
	}
	// order 保存按账号、会话以及可选买家和商品条件命中的本地订单。
	var order *db.Order
	// err 表示按会话回填订单时的查询错误。
	var err error
	if task.TriggerType == TriggerOrderCreated {
		// 拍下事件对应订单尚未付款，须按含待付款的未终结订单回填商品与订单事实。
		order, err = c.store.Orders.FindLatestOpenByChat(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID)
	} else {
		// 付款与待刀成事件只认待发货订单，避免把未付款订单当成可发货事实。
		order, err = c.store.Orders.FindLatestPendingByChat(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return task, nil
		}
		return task, fmt.Errorf("按会话回填自动发货订单: %w", err)
	}
	if order == nil {
		return task, nil
	}
	return mergeOrderIntoTask(task, order), nil
}

// prepareBuyerNickname 补齐模板渲染所需的买家昵称摘要。
func (c *Center) prepareBuyerNickname(ctx context.Context, task Task) (Task, error) {
	if task.BuyerNickname != "" || task.ChatID == "" {
		return task, nil
	}
	// nickname 保存聊天会话中可用于模板渲染的买家昵称。
	nickname, nicknameErr := c.store.Chats.BuyerNicknameForAutomation(ctx, task.AccountID, task.ChatID)
	if nicknameErr != nil {
		return task, fmt.Errorf("读取买家昵称: %w", nicknameErr)
	}
	task.BuyerNickname = nickname
	return task, nil
}

// mergeOrderIntoTask 用本地订单事实补全自动化任务中尚未获得的字段。
func mergeOrderIntoTask(task Task, order *db.Order) Task {
	if task.OrderID == "" {
		task.OrderID = order.OrderID
	}
	if task.ItemID == "" {
		task.ItemID = order.ItemID
	}
	if task.BuyerID == "" {
		task.BuyerID = order.BuyerID
	}
	if task.ChatID == "" {
		task.ChatID = order.ChatID
	}
	if task.SpecName == "" {
		task.SpecName = order.SpecName
	}
	if task.SpecValue == "" {
		task.SpecValue = order.SpecValue
	}
	if task.Quantity == "" {
		task.Quantity = order.Quantity
	}
	if task.Amount == "" {
		task.Amount = order.Amount
	}
	if task.OrderStatus == "" {
		task.OrderStatus = order.OrderStatus
	}
	// 本地订单同步只会在平台明确识别砍价活动时写入真值；任务一旦携带真值不得在后续补全中降级。
	if order.IsBargain != 0 {
		task.IsBargain = true
	}
	return task
}
