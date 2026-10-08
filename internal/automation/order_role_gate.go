package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// roleVerificationTaskKeyField 保存未知角色待核验任务的稳定防重键；它会随任务快照保存，避免补齐订单号后改变延期任务身份。
const roleVerificationTaskKeyField = "automation_role_verification_key"

// roleVerificationRetryInterval 控制首条未知角色事件等待订单/商品同步的最短间隔。
const roleVerificationRetryInterval = time.Minute

// errSellerRoleEvidencePending 表示待核验任务暂时没有足够的本地卖家事实；调度器会按既有延期队列退避并最终通知人工。
var errSellerRoleEvidencePending = errors.New("未知角色事件暂缺本地卖家事实")

// isWebSocketShippingTask 判断任务是否来自 WebSocket 的付款后发货阶段；只有这类事件需要未知角色证据门禁。
func isWebSocketShippingTask(task Task) bool {
	// source、triggerType 保存经过规范化的事件来源和交易阶段，避免普通评价或人工任务误用卖家门禁。
	source, triggerType := strings.TrimSpace(task.Source), strings.TrimSpace(task.TriggerType)
	return source == "ws" && (triggerType == TriggerOrderPaid || triggerType == TriggerBargainPending)
}

// isWebSocketSellerOrderTask 判断需要确认当前账号卖家身份的 WebSocket 交易事件；评价历史兼容不在此门禁内。
func isWebSocketSellerOrderTask(task Task) bool {
	if isWebSocketShippingTask(task) {
		return true
	}
	return strings.TrimSpace(task.Source) == "ws" && strings.TrimSpace(task.TriggerType) == TriggerOrderCreated
}

// authorizeWebSocketSellerTask 校验缺少 role 的 WebSocket 发货事件是否能由本地卖家事实证明。
// 未通过校验时返回 false 且不写订单事实；数据库读取失败返回错误，调用方必须停止外部动作并保留后续扫描机会。
func (c *Center) authorizeWebSocketSellerTask(ctx context.Context, task Task) (Task, bool, string, error) {
	if !isWebSocketSellerOrderTask(task) {
		return task, true, "", nil
	}
	switch task.OrderRole {
	case OrderRoleSeller:
		return task, true, "", nil
	case OrderRoleBuyer:
		return task, false, "explicit_buyer_role", nil
	}
	if task.TriggerType == TriggerOrderCreated {
		if strings.TrimSpace(task.ItemID) == "" {
			// foreignReason 保存用会话内其它归属订单否定本机卖家身份的结论；非空时不再延期等待。
			if foreignReason := c.resolveBuyerRoleByForeignChatOrder(ctx, task); foreignReason != "" {
				return task, false, foreignReason, nil
			}
			return task, false, "missing_local_item", nil
		}
		if c == nil || c.store == nil || c.store.Items == nil {
			return task, false, "item_store_unavailable", nil
		}
		// owned、itemErr 保存拍下事件对应商品是否曾属于当前账号；商品详情不进入事件快照或日志。
		owned, itemErr := c.store.Items.ExistsByCookieItem(ctx, task.AccountID, task.ItemID)
		if itemErr != nil {
			return task, false, "", fmt.Errorf("核对未知角色拍下商品归属: %w", itemErr)
		}
		if !owned {
			return task, false, "missing_local_item", nil
		}
		task.OrderRole = OrderRoleSeller
		return task, true, "", nil
	}
	if c == nil || c.store == nil || c.store.Orders == nil {
		return task, false, "order_store_unavailable", nil
	}
	if strings.TrimSpace(task.OrderID) == "" {
		// foreignReason 保存用会话内其它归属订单否定本机卖家身份的结论；非空时不再延期等待。
		if foreignReason := c.resolveBuyerRoleByForeignChatOrder(ctx, task); foreignReason != "" {
			return task, false, foreignReason, nil
		}
		return task, false, "missing_order_id", nil
	}
	// order、orderErr 保存当前账号的本地订单事实；读取范围随后还会再次核对账号归属。
	order, orderErr := c.store.Orders.Get(ctx, task.OrderID)
	if errors.Is(orderErr, db.ErrNotFound) {
		return task, false, "missing_local_order", nil
	}
	if orderErr != nil {
		return task, false, "", fmt.Errorf("核对未知角色订单事实: %w", orderErr)
	}
	return c.authorizeWebSocketSellerTaskWithOrder(ctx, task, order)
}

// resolveBuyerRoleByForeignChatOrder 在缺少订单或商品事实时，用会话内的订单归属判断本机是否为买家或无关账号。
// 同一张平台交易卡片会同时送到卖家和买家登录账号：买家侧事件永远等不到属于它的卖家事实，若继续延期会在退避队列里
// 反复重放，最终还会按重试上限发出「需要人工处理」告警，而买家侧根本没有发货义务。
// 返回空串表示证据不足，调用方必须维持原行为继续延期；返回的拒绝原因都不在可重试集合内，事件会被直接收口。
func (c *Center) resolveBuyerRoleByForeignChatOrder(ctx context.Context, task Task) string {
	if c == nil || c.store == nil || c.store.Orders == nil || strings.TrimSpace(task.ChatID) == "" {
		return ""
	}
	// ownerExists 表示该会话内是否存在归属本账号的订单；存在时本机仍可能是卖家，必须继续等待订单同步。
	ownerExists, ownerErr := c.store.Orders.ExistsOpenSellerOrderByChat(ctx, task.ChatID, task.AccountID)
	if ownerErr != nil {
		// 读取失败时不得臆断身份，保留延期以免误杀真实的卖家发货义务。
		return ""
	}
	if ownerExists {
		return ""
	}
	// foreign 保存会话内最近一笔不归属本账号的订单；没有这类订单时同样没有否定卖家身份的依据。
	foreign, foreignErr := c.store.Orders.FindLatestForeignOrderByChat(ctx, task.ChatID, task.AccountID)
	if foreignErr != nil || foreign == nil {
		return ""
	}
	if sameOrderIdentity(foreign.BuyerID, task.AccountID) {
		// 买家标识与本账号一致：本机就是这笔订单的买家，不是卖家。
		return "explicit_buyer_role"
	}
	// 订单归属其它账号且买家也不是本机：本机与该笔订单无关，不需要执行任何发货动作。
	return "order_account_mismatch"
}

// authorizeWebSocketSellerTaskWithOrder 使用已读取的订单和当前账号商品表完成未知角色核验。
// order 必须属于当前账号且仍待发货；商品、买家、会话等事件字段若已提供则必须逐项一致。
func (c *Center) authorizeWebSocketSellerTaskWithOrder(ctx context.Context, task Task, order *db.Order) (Task, bool, string, error) {
	if order == nil {
		return task, false, "missing_local_order", nil
	}
	if !sameOrderIdentity(order.CookieID, task.AccountID) {
		return task, false, "order_account_mismatch", nil
	}
	if !isPendingShipOrder(order) || order.SystemShipped {
		return task, false, "order_not_pending_ship", nil
	}
	if strings.TrimSpace(order.ItemID) == "" {
		return task, false, "incomplete_local_order_identity", nil
	}
	if strings.TrimSpace(task.ItemID) != "" && !sameOrderIdentity(task.ItemID, order.ItemID) {
		return task, false, "item_mismatch", nil
	}
	if strings.TrimSpace(task.BuyerID) != "" && !sameOrderIdentity(task.BuyerID, order.BuyerID) {
		return task, false, "buyer_mismatch", nil
	}
	if strings.TrimSpace(task.ChatID) != "" && !sameOrderIdentity(task.ChatID, order.ChatID) {
		return task, false, "chat_mismatch", nil
	}
	if c == nil || c.store == nil || c.store.Items == nil {
		return task, false, "item_store_unavailable", nil
	}
	// owned、itemErr 保存当前账号历史商品归属查询结果；软删除商品仍是卖家证据，避免售出后商品同步移除导致漏发。
	owned, itemErr := c.store.Items.ExistsByCookieItem(ctx, task.AccountID, order.ItemID)
	if itemErr != nil {
		return task, false, "", fmt.Errorf("核对未知角色商品归属: %w", itemErr)
	}
	if !owned {
		return task, false, "missing_local_item", nil
	}
	task = mergeOrderIntoTask(task, order)
	task.OrderRole = OrderRoleSeller
	return task, true, "", nil
}

// isPendingShipOrder 判断本地订单是否仍处于付款后待发货阶段，并兼容同步接口写入的状态别名。
func isPendingShipOrder(order *db.Order) bool {
	if order == nil {
		return false
	}
	switch db.NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) {
	case "pending_ship", "pending_delivery", "partial_success", "partial_pending_finalize":
		return true
	default:
		return false
	}
}

// isOrderTerminalWithoutDelivery 判断订单状态是否已明确终结且不需要再发货。
// 只承认取消、关闭、退款与已完成这几类明确终态；unknown、空值与待付款等事实不足或尚未结束
// 的状态一律返回 false，避免把「状态未知」误判为订单失效而漏发。
func isOrderTerminalWithoutDelivery(status string) bool {
	switch db.NormalizeOrderStatus(strings.TrimSpace(status)) {
	case "cancelled", "refunding", "completed":
		return true
	default:
		return false
	}
}

// roleVerificationRetryable 判断拒绝原因是否可能因订单或商品同步完成而恢复；身份冲突和已结束订单不应反复重放。
func roleVerificationRetryable(reason string) bool {
	switch reason {
	case "missing_local_order", "missing_order_id", "incomplete_local_order_identity", "missing_local_item", "order_store_unavailable", "item_store_unavailable":
		return true
	default:
		return false
	}
}

// deferUnknownRoleTask 将首条未知角色事件保存到统一延期队列；任务使用事件指纹去重，补齐订单号后仍沿用同一队列记录。
func (c *Center) deferUnknownRoleTask(ctx context.Context, task Task, reason string) error {
	if c == nil || c.store == nil || c.store.Automation == nil {
		return errors.New("未知角色事件缺少延期任务存储")
	}
	// key 保存本次事件跨订单同步前后的稳定任务身份。
	key := roleVerificationTaskKey(task)
	// task 保存带稳定键的副本；不修改调用方持有的原始 Raw 映射。
	task = taskWithRoleVerificationKey(task, key)
	task.CookieStr = ""
	// raw、marshalErr 保存去除凭证后的任务快照和序列化错误。
	raw, marshalErr := json.Marshal(task)
	if marshalErr != nil {
		return fmt.Errorf("序列化未知角色待核验任务: %w", marshalErr)
	}
	// dueAt 控制订单同步完成前的重试节奏，避免系统卡片高峰持续打数据库。
	dueAt := time.Now().UTC().Add(roleVerificationRetryInterval).Unix()
	// deferErr 保存未知角色待核验任务写入延期队列的错误。
	if deferErr := c.store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
		TaskKey: task.AccountID + ":" + key, CookieID: task.AccountID, TriggerType: task.TriggerType,
		TaskJSON: string(raw), DueAt: dueAt, ErrorMessage: "等待本地卖家事实：" + reason,
	}); deferErr != nil {
		return fmt.Errorf("保存未知角色待核验任务: %w", deferErr)
	}
	return nil
}

// roleVerificationTaskKey 为未知角色事件生成稳定键；优先使用平台事件键，缺失时对脱敏事实和原始事件做哈希。
func roleVerificationTaskKey(task Task) string {
	// marker 是历史待核验任务中已固化的稳定键；沿用可避免补齐订单号后键发生变化。
	if marker := roleVerificationMarker(task); marker != "" {
		return marker
	}
	// key 保存平台事件已有的稳定业务键；存在时无需再次计算哈希。
	if key := buildTriggerKey(task); key != "" {
		return key
	}
	// fingerprintSource 只包含订单事件事实和原始平台报文，不包含 CookieStr 等凭证字段。
	fingerprintSource := struct {
		TriggerType string         `json:"trigger_type"`
		AccountID   string         `json:"account_id"`
		ChatID      string         `json:"chat_id"`
		OrderID     string         `json:"order_id"`
		ItemID      string         `json:"item_id"`
		BuyerID     string         `json:"buyer_id"`
		Text        string         `json:"text"`
		Raw         map[string]any `json:"raw"`
	}{
		TriggerType: task.TriggerType, AccountID: task.AccountID, ChatID: task.ChatID,
		OrderID: task.OrderID, ItemID: task.ItemID, BuyerID: task.BuyerID, Text: task.Text, Raw: task.Raw,
	}
	// encoded、marshalErr 保存用于哈希的脱敏事件字节；异常类型只影响键内容，不开放执行。
	encoded, marshalErr := json.Marshal(fingerprintSource)
	if marshalErr != nil {
		encoded = []byte(strings.Join([]string{task.TriggerType, task.AccountID, task.ChatID, task.OrderID, task.ItemID, task.BuyerID, task.Text}, "\x00"))
	}
	// digest 保存脱敏事件指纹，作为没有平台业务键时的幂等身份。
	digest := sha256.Sum256(encoded)
	return "role_verification:" + task.TriggerType + ":" + hex.EncodeToString(digest[:])
}

// taskWithRoleVerificationKey 复制任务的 Raw 映射并写入待核验键，保证后续恢复和动作延期使用同一防重身份。
func taskWithRoleVerificationKey(task Task, key string) Task {
	// raw 保存任务原始映射的浅拷贝，避免在事件接收线程中修改平台报文引用。
	raw := make(map[string]any, len(task.Raw)+1)
	// field、value 遍历原始任务字段并复制其值，避免修改接收线程共享的映射。
	for field, value := range task.Raw {
		raw[field] = value
	}
	raw[roleVerificationTaskKeyField] = key
	task.Raw = raw
	return task
}

// sameOrderIdentity 比较订单、商品、买家或会话标识，并兼容历史协议的 @goofish 后缀。
func sameOrderIdentity(left, right string) bool {
	left = strings.TrimSuffix(strings.TrimSpace(left), "@goofish")
	right = strings.TrimSuffix(strings.TrimSpace(right), "@goofish")
	return left != "" && right != "" && left == right
}

// roleVerificationMarker 取出历史未知角色任务已固化的防重键；不存在或非字符串时返回空串。
// buildTriggerKey 与 roleVerificationTaskKey 优先沿用它，防止补齐订单号后防重键漂移。
func roleVerificationMarker(task Task) string {
	if task.Raw == nil {
		return ""
	}
	// marker、ok 保存历史任务快照中的稳定键及其类型判断结果。
	marker, ok := task.Raw[roleVerificationTaskKeyField].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(marker)
}

// authorizeWebSocketSellerRoleGate 对 WebSocket 交易事件执行一次卖家身份门禁，并承担未放行时的收口动作。
// verifiedTask 是核验通过后的任务；proceed 表示调用方是否应视为已处理成功；handled 为真时调用方必须立即返回，不再写订单事实。
// 数据库等外部错误经 err 上抛，不得降级为「安全地忽略」，以免丢失后续重试机会。
func (c *Center) authorizeWebSocketSellerRoleGate(ctx context.Context, task Task) (verifiedTask Task, proceed bool, handled bool, err error) {
	// unknownRoleTask 标记本次事件是否未携带明确角色；仅该类事件需要记录本地卖家事实核验的放行日志。
	unknownRoleTask := task.OrderRole == OrderRoleUnknown && isWebSocketSellerOrderTask(task)
	// checked、sellerVerified、rejectReason 保存本地卖家核验结果；失败时在事实写入前停止，避免把买家订单落到卖家账号。
	checked, sellerVerified, rejectReason, roleErr := c.authorizeWebSocketSellerTask(ctx, task)
	if roleErr != nil {
		return task, false, false, roleErr
	}
	if sellerVerified {
		if unknownRoleTask && c.logger != nil {
			c.logger.Info("未知角色交易事件已通过本地卖家核验，继续执行自动化", "account", checked.AccountID, "order_id", checked.OrderID, "item_id", checked.ItemID, "trigger", checked.TriggerType, "source", checked.Source)
		}
		return checked, true, false, nil
	}
	if roleVerificationRetryable(rejectReason) {
		if isDeferredReplay(task) {
			// 未知角色延期任务再次没有本地证据时交给统一退避；达到上限后由调度器发送人工处理通知。
			return task, false, false, errSellerRoleEvidencePending
		}
		// deferErr 保存首条未知角色事件写入延期队列的错误；写入失败不能伪装成已安全处理。
		if deferErr := c.deferUnknownRoleTask(ctx, task, rejectReason); deferErr != nil {
			return task, false, false, deferErr
		}
		if c.logger != nil {
			c.logger.Info("未知角色发货事件已保存，等待本地卖家事实", "account", task.AccountID, "order_id", task.OrderID, "trigger", task.TriggerType, "reason", rejectReason)
		}
		return task, true, true, nil
	}
	if c.logger != nil {
		c.logger.Info("未知角色发货事件未通过本地卖家核验，未执行外部动作", "account", task.AccountID, "order_id", task.OrderID, "trigger", task.TriggerType, "reason", rejectReason)
	}
	return task, false, true, nil
}
