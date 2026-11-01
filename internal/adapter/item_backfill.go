package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// ItemBackfillRepository 是单商品回补的基础设施适配器。
// 它把「读凭证快照 → 调 MTOP 单商品详情 → 核验发布人 → 写入本地商品表」收敛在一处，
// 引擎侧只通过可选接口决定何时触发，不感知平台调用细节。
type ItemBackfillRepository struct {
	// store 提供账号凭证与商品持久化能力；构造后不可替换。
	store *db.Store
	// client 返回平台客户端；测试可注入同时实现 mtop.ItemDetailSource 的替身。
	client func() mtop.Client
	// logger 记录不含凭证的回补阶段信息。
	logger *slog.Logger
}

// NewItemBackfillRepository 构造单商品回补适配器；client 为空时使用默认 MTOP 客户端，logger 为空时使用默认日志器。
func NewItemBackfillRepository(store *db.Store, client func() mtop.Client, logger *slog.Logger) *ItemBackfillRepository {
	if client == nil {
		client = func() mtop.Client { return mtop.NewClient() }
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ItemBackfillRepository{store: store, client: client, logger: logger}
}

// BackfillItem 读取 accountID 名下 itemID 的平台详情，核验发布人后写入本地商品基础信息。
// ctx 控制整次回补的预算；账号凭证只在请求签名时使用，不写入日志或错误信息。
// 任何一步失败都返回错误：调用方（引擎侧协调器）据此写失败负缓存，绝不把缺失商品当作已归属。
func (r *ItemBackfillRepository) BackfillItem(ctx context.Context, accountID, itemID string) error {
	if r == nil || r.store == nil || r.store.Items == nil || r.store.Cookies == nil {
		return errors.New("商品回补存储未初始化")
	}
	// accountID、itemID 是本次回补的账号与商品标识，去除空白后必须非空。
	accountID = strings.TrimSpace(accountID)
	itemID = strings.TrimSpace(itemID)
	if accountID == "" || itemID == "" {
		return errors.New("商品回补需要账号与商品标识")
	}
	// unlock 仅保护凭证快照读取；读到快照后立即释放，避免慢平台 I/O 占用账号凭证锁。
	unlock := r.store.LockAccountCredentials(accountID)
	// detail、loadErr 保存账号平台运行时视图与读取错误；视图内含明文 Cookie，禁止外泄。
	detail, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	unlock()
	if errors.Is(loadErr, db.ErrNotFound) {
		return fmt.Errorf("商品回补账号不存在: %w", loadErr)
	}
	if loadErr != nil {
		return fmt.Errorf("读取商品回补账号凭证失败: %w", loadErr)
	}
	if !hasStoredCredential(detail) {
		return errors.New("商品回补账号缺少可用凭证")
	}
	// selfID 是当前账号的非敏感平台用户标识，用于核验商品发布人归属。
	selfID := strings.TrimSpace(protocol.TransCookies(detail.Value)["unb"])
	if selfID == "" {
		return errors.New("商品回补缺少当前账号身份")
	}
	// source 只接受实现了可选单商品详情能力的客户端；旧替身缺该能力时 fail-closed。
	source, supported := r.mtopClient().(mtop.ItemDetailSource)
	if !supported {
		return errors.New("商品回补客户端不支持单商品详情")
	}
	// requestCtx 是带账号 Cookie 会话的请求上下文，用于吸收平台下发的 Set-Cookie。
	requestCtx, session := withCookieSnapshot(ctx, detail)
	// snapshot、fetchErr 保存平台详情快照与请求错误。
	snapshot, fetchErr := source.FetchItemDetailSnapshot(requestCtx, detail.Value, itemID)
	// 会话 Cookie 变化先落盘再判错：Token 轮换与请求失败可能同时发生。
	r.persistBackfillSession(ctx, accountID, detail, session)
	if fetchErr != nil {
		return fmt.Errorf("读取商品详情失败: %w", fetchErr)
	}
	// 归属核验：只有发布人是当前账号才允许写入本地商品表，
	// 否则会把他人商品（买家咨询的商品）误判成本账号库存，进而错误放行卖家自动回复。
	if !samePlatformUser(snapshot.SellerID, selfID) {
		return errors.New("商品发布人与当前账号不一致，拒绝回补")
	}
	// row 是写入本地商品表的基础记录；多规格与多数量发货设置由列表同步链路维护，此处不覆盖。
	row := db.ItemInfoRow{
		CookieID:        accountID,
		ItemID:          itemID,
		ItemTitle:       snapshot.Title,
		ItemDescription: snapshot.Description,
		ItemCategory:    snapshot.CategoryID,
		ItemPrice:       snapshot.Price,
		ItemDetail:      snapshot.DetailJSON,
	}
	if // saveErr 是本地商品写入错误。
	saveErr := r.store.Items.UpsertBasic(ctx, &row); saveErr != nil {
		return fmt.Errorf("写入本地商品失败: %w", saveErr)
	}
	if r.logger != nil {
		r.logger.Info("商品回补已写入本地商品库", "account_id", accountID, "item_id", itemID, "multi_spec", snapshot.IsMultiSpec)
	}
	return nil
}

// persistBackfillSession 写回详情请求期间平台下发的 Cookie 变化。
// 凭证复核与写回在账号凭证锁内完成（与商品同步链路一致），但锁内不执行任何网络 I/O。
func (r *ItemBackfillRepository) persistBackfillSession(ctx context.Context, accountID string, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession) {
	if r == nil || r.store == nil || r.store.Cookies == nil || session == nil {
		return
	}
	// value、snapshot、changed 保存平台会话当前状态；未变化时无需写库。
	value, snapshot, changed := session.State()
	if !changed {
		return
	}
	// metadata 是移除旧快照后待写入的新 metadata，有完整 Cookie Jar 时以新快照覆盖。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	if snapshot != nil {
		metadata = cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	}
	// unlock 保护「复核凭证未变 + 写回」的读改写序列。
	unlock := r.store.LockAccountCredentials(accountID)
	defer unlock()
	// latest、loadErr 保存写回前的最新凭证视图；读取失败或凭证已被并发改写时放弃写回。
	latest, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if loadErr != nil || latest.Value != detail.Value || latest.MetadataJSON != detail.MetadataJSON {
		return
	}
	if // err 是 Cookie 会话持久化错误。
	err := r.store.Cookies.UpdateRenewalCookie(ctx, accountID, value, metadata, time.Now().Unix()); err != nil && r.logger != nil {
		r.logger.Debug("商品回补写回会话 Cookie 失败", "account_id", accountID, "err", err)
	}
}

// mtopClient 返回当前回补使用的平台客户端，缺省时回落到默认实现。
func (r *ItemBackfillRepository) mtopClient() mtop.Client {
	if r != nil && r.client != nil {
		if // client 是回调返回的平台客户端。
		client := r.client(); client != nil {
			return client
		}
	}
	return mtop.NewClient()
}

// BackfillItem 是引擎可选回补接口在 Adapter 上的实现，委托给已注入的回补仓储。
// 返回的错误只用于引擎的失败负缓存，不影响当前消息「不回复」的结论。
func (a *Adapter) BackfillItem(ctx context.Context, accountID, itemID string) error {
	if a == nil || a.itemBackfill == nil {
		return errors.New("商品回补仓储未初始化")
	}
	return a.itemBackfill.BackfillItem(ctx, accountID, itemID)
}

// samePlatformUser 比较两个平台用户标识是否指向同一账号，忽略前后空白与 @goofish 后缀。
func samePlatformUser(left, right string) bool {
	// normalize 去除空白与平台后缀并转为小写，供大小写不敏感比较。
	normalize := func(value string) string {
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "@goofish"))
	}
	// leftID、rightID 是归一化后的标识；任一为空都视为无法确认同一账号。
	leftID, rightID := normalize(left), normalize(right)
	if leftID == "" || rightID == "" {
		return false
	}
	return leftID == rightID
}
