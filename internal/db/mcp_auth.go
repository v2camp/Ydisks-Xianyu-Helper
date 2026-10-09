// mcp_auth.go 提供对外开放 MCP 服务的持久化能力：
//   - 启用状态与 loopback 网络策略复用 system_settings 普通键（非敏感，明文存储）；
//   - mcp_tokens 只保存令牌 SHA-256 哈希，支持轮换 24 小时宽限与立即吊销；
//   - mcp_call_audit 保存键级脱敏后的调用审计，支撑调用追溯。
//
// 本仓储不读取环境引导令牌（由应用层合并），也不接触 Cookie、Token 等平台凭证。

package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MCP 开关在 system_settings 中的键名；均为非敏感普通键，可经 SystemSettings 明文读写。
const (
	// mcpSettingEnabled 是 MCP 服务是否启用的设置键，取值为 1/0 风格的布尔字符串。
	mcpSettingEnabled = "mcp.server.enabled"
	// mcpSettingAllowNonLoopback 控制是否放行非本机来源访问 /mcp；默认关闭。
	mcpSettingAllowNonLoopback = "mcp.server.allow_non_loopback"
)

// 令牌行类别与令牌来源的稳定常量；写入审计与返回状态时只能取下列值。
const (
	// MCPTokenKindCurrent 表示当前生效的持久化令牌，任意时刻至多一行。
	MCPTokenKindCurrent = "current"
	// MCPTokenKindPrevious 表示轮换后处于宽限期的旧令牌，任意时刻至多一行。
	MCPTokenKindPrevious = "previous"
	// MCPTokenSourcePersisted 表示调用使用的是数据库持久化令牌。
	MCPTokenSourcePersisted = "persisted"
	// MCPTokenSourceEnvironment 表示调用使用的是 XIANYU_MCP_TOKEN 环境引导令牌。
	MCPTokenSourceEnvironment = "environment"
)

// 审计记录类别常量，对应 MCP 协议三类可记录调用。
const (
	// MCPAuditCategoryTool 是 tools/call 调用类别。
	MCPAuditCategoryTool = "tool"
	// MCPAuditCategoryResource 是 resources/read 调用类别。
	MCPAuditCategoryResource = "resource"
	// MCPAuditCategoryPrompt 是 prompts/get 调用类别。
	MCPAuditCategoryPrompt = "prompt"
)

const (
	// mcpTokenGracePeriod 是轮换后旧令牌继续可用的宽限窗口，固定 24 小时。
	mcpTokenGracePeriod = 24 * time.Hour
	// mcpTokenRandomBytes 是明文令牌使用的随机字节数；256 bit 熵。
	mcpTokenRandomBytes = 32
	// mcpAuditValueLimit 是审计参数标量值按 rune 计的最大保留长度，防止超长内容写满审计行。
	mcpAuditValueLimit = 200
	// mcpAuditDefaultLimit 是审计查询未指定分页大小时的默认条数。
	mcpAuditDefaultLimit = 50
	// mcpAuditMaxLimit 是审计查询单页允许的最大条数。
	mcpAuditMaxLimit = 200
)

// mcpBoolTrue 是布尔设置落库时的真值字符串。
const mcpBoolTrue = "1"

// MCPTokenStatus 是持久化令牌配置态的非敏感视图，不含任何哈希或明文。
type MCPTokenStatus struct {
	// HasCurrent 表示当前是否存在生效中的持久化令牌。
	HasCurrent bool
	// CurrentCreatedAt 是当前令牌创建时间（Unix 秒），无令牌时为 0。
	CurrentCreatedAt int64
	// CurrentLastUsedAt 是当前令牌最后一次鉴权成功时间（Unix 秒），从未使用为 0。
	CurrentLastUsedAt int64
	// HasPrevious 表示是否存在宽限期内的旧令牌。
	HasPrevious bool
	// PreviousExpiresAt 是宽限期旧令牌的失效时刻（Unix 秒），无旧令牌时为 0。
	PreviousExpiresAt int64
}

// MCPAuditRecord 是一次 MCP 调用写入审计所需的非敏感字段集合。
type MCPAuditRecord struct {
	// ID 是审计行主键，写入时由数据库分配。
	ID int64
	// CreatedAt 是调用发生时间（Unix 秒）；由应用层注入时钟，测试可确定性驱动。
	CreatedAt int64
	// UserID 是执行调用的固定管理员本地用户标识。
	UserID int64
	// TokenSource 是令牌来源，取 MCPTokenSourcePersisted 或 MCPTokenSourceEnvironment。
	TokenSource string
	// Category 是调用类别，取 MCPAuditCategoryTool/Resource/Prompt。
	Category string
	// Name 是被调用的工具、资源或提示名称。
	Name string
	// CookieID 是调用目标账号标识；无账号归属的调用为空串。
	CookieID string
	// Arguments 是调用参数；写入前必须经 SanitizeMCPAuditArguments 脱敏。
	Arguments string
	// Success 表示调用是否成功完成。
	Success bool
	// ErrorClass 是失败类别的稳定标识；成功时为空串。
	ErrorClass string
	// DurationMS 是调用耗时（毫秒）。
	DurationMS int64
}

// MCPAuditFilter 是审计分页查询条件；零值字段不参与过滤。
type MCPAuditFilter struct {
	// Limit 是单页条数；非正时回落到默认值，超过上限时截断。
	Limit int
	// Offset 是跳过的记录数，用于分页。
	Offset int
	// Category 非空时只返回该调用类别。
	Category string
	// Name 非空时按名称精确过滤。
	Name string
	// CookieID 非空时按账号归属过滤。
	CookieID string
	// SuccessState 取 0 时不过滤成败（结构体零值即默认全部）；1 只看成功；-1 只看失败。
	SuccessState int
}

// MCPAuditPage 是审计分页查询结果。
type MCPAuditPage struct {
	// Total 是不过滤分页时满足条件的总记录数。
	Total int
	// Records 是当前页记录，按时间倒序排列。
	Records []MCPAuditRecord
}

// MCPAuthStore 聚合 MCP 开关、持久化令牌与调用审计的持久化访问，不持有平台客户端。
type MCPAuthStore struct {
	// DB 是共享数据库连接池，由 Store 统一持有与关停。
	DB *sql.DB
	// Dialect 是当前数据库方言，用于 system_settings 的列名引用与 upsert。
	Dialect Dialect
	// settings 复用系统设置仓储读写非敏感开关键。
	settings *SystemSettings
}

// NewMCPAuthStore 构造 MCP 持久化仓储；database 为 nil 时返回 nil，调用方据此跳过装配。
func NewMCPAuthStore(database *sql.DB, dialect Dialect) *MCPAuthStore {
	if database == nil {
		return nil
	}
	// settings 是复用的系统设置仓储；MCP 开关键不在敏感白名单内，按明文读写。
	settings := &SystemSettings{DB: database, Dialect: dialect}
	return &MCPAuthStore{DB: database, Dialect: dialect, settings: settings}
}

// GenerateMCPToken 生成一枚新的明文令牌：32 字节随机数经 URL 安全 base64 无填充编码，
// 提供 256 bit 熵。明文只在生成/轮换响应中出现一次，持久化只保存其哈希。
func GenerateMCPToken() (string, error) {
	// raw 是密码学随机字节；读取失败必须中止生成，禁止回退到弱随机。
	raw := make([]byte, mcpTokenRandomBytes)
	if // err 是随机字节读取失败原因。
	_, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 MCP 令牌随机数失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashMCPToken 计算明文令牌的 SHA-256 十六进制摘要（64 字符），作为数据库唯一存储形态。
func hashMCPToken(token string) string {
	// sum 是令牌的 SHA-256 摘要；令牌先去空白，避免粘贴换行造成哈希不一致。
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// Enabled 读取 MCP 服务启用状态；键缺失或值非真时均视为关闭（默认安全）。
func (s *MCPAuthStore) Enabled(ctx context.Context) (bool, error) {
	if s == nil || s.settings == nil {
		return false, errors.New("MCP 仓储未初始化")
	}
	// raw 是设置键的原始字符串值；缺失时 SystemSettings.Get 返回空串。
	raw, err := s.settings.Get(ctx, mcpSettingEnabled)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(raw) == mcpBoolTrue, nil
}

// SetEnabled 保存 MCP 服务启用状态，变更对下一次请求立即生效，不要求重启进程。
func (s *MCPAuthStore) SetEnabled(ctx context.Context, enabled bool) error {
	if s == nil || s.settings == nil {
		return errors.New("MCP 仓储未初始化")
	}
	// value 是布尔状态的落库字符串。
	value := ""
	if enabled {
		value = mcpBoolTrue
	}
	return s.settings.Set(ctx, mcpSettingEnabled, value)
}

// AllowNonLoopback 读取是否放行非本机来源；默认关闭，仅在管理员显式打开时为真。
func (s *MCPAuthStore) AllowNonLoopback(ctx context.Context) (bool, error) {
	if s == nil || s.settings == nil {
		return false, errors.New("MCP 仓储未初始化")
	}
	// raw 是网络策略设置键的原始值。
	raw, err := s.settings.Get(ctx, mcpSettingAllowNonLoopback)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(raw) == mcpBoolTrue, nil
}

// SetAllowNonLoopback 保存是否允许非 loopback 来源访问 /mcp。
func (s *MCPAuthStore) SetAllowNonLoopback(ctx context.Context, allowed bool) error {
	if s == nil || s.settings == nil {
		return errors.New("MCP 仓储未初始化")
	}
	// value 是网络策略的落库字符串。
	value := ""
	if allowed {
		value = mcpBoolTrue
	}
	return s.settings.Set(ctx, mcpSettingAllowNonLoopback, value)
}

// RotateToken 生成新令牌并完成轮换：旧当前令牌标记为 24 小时宽限 previous（替换更早的 previous），
// 新令牌作为 current 落库。返回的明文只在本次调用出现，调用方必须立即安全转交管理员。
// now 由调用方注入，保证宽限截止时间在测试中可控。
func (s *MCPAuthStore) RotateToken(ctx context.Context, now time.Time) (string, error) {
	if s == nil || s.DB == nil {
		return "", errors.New("MCP 仓储未初始化")
	}
	// plaintext 是新令牌明文；哈希以外的任何形态都不得写库。
	plaintext, err := GenerateMCPToken()
	if err != nil {
		return "", err
	}
	// hash 是新令牌的 SHA-256 哈希。
	hash := hashMCPToken(plaintext)
	// currentUnix 是当前时刻 Unix 秒，作为新令牌创建时间。
	currentUnix := now.Unix()
	// expiresUnix 是旧令牌宽限截止 Unix 秒。
	expiresUnix := now.Add(mcpTokenGracePeriod).Unix()
	// tx 把清除更早旧令牌、旧令牌降级、新令牌插入三步收成一个事务。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("开启 MCP 令牌轮换事务失败: %w", err)
	}
	// rollback 保证任一步失败时事务不会悬挂；提交后回滚返回 ErrTxDone，被显式忽略。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// 宽限令牌只保留最近一枚：先清除历史 previous 行。
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_tokens WHERE kind=?`, MCPTokenKindPrevious); err != nil {
		return "", fmt.Errorf("清理旧宽限令牌失败: %w", err)
	}
	// 既有当前令牌降级为宽限令牌；首次生成时该语句影响 0 行属正常。
	if _, err := tx.ExecContext(ctx,
		`UPDATE mcp_tokens SET kind=?, expires_at=? WHERE kind=?`,
		MCPTokenKindPrevious, expiresUnix, MCPTokenKindCurrent); err != nil {
		return "", fmt.Errorf("旧令牌降级宽限失败: %w", err)
	}
	// 插入新当前令牌；last_used_at 初始为 0，expires_at 为 0 表示不过期。
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO mcp_tokens (token_hash, kind, created_at, last_used_at, expires_at) VALUES (?,?,?,0,0)`,
		hash, MCPTokenKindCurrent, currentUnix); err != nil {
		return "", fmt.Errorf("写入新 MCP 令牌失败: %w", err)
	}
	if // commitErr 是事务提交失败原因。
	err := tx.Commit(); err != nil {
		return "", fmt.Errorf("提交 MCP 令牌轮换事务失败: %w", err)
	}
	committed = true
	return plaintext, nil
}

// RevokeTokens 立即删除全部持久化令牌（含宽限令牌）；环境引导令牌不受此方法影响。
func (s *MCPAuthStore) RevokeTokens(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return errors.New("MCP 仓储未初始化")
	}
	if // err 是删除全部持久化令牌行的失败原因。
	_, err := s.DB.ExecContext(ctx, `DELETE FROM mcp_tokens`); err != nil {
		return fmt.Errorf("吊销 MCP 令牌失败: %w", err)
	}
	return nil
}

// VerifyPersistedToken 校验明文令牌是否命中当前或未到期宽限令牌。
// 命中时常量时间比对通过并刷新最后使用时间；过期宽限令牌被惰性清理；任何不匹配都返回 false。
// now 由调用方注入，保证到期判定在测试中可控。
func (s *MCPAuthStore) VerifyPersistedToken(ctx context.Context, token string, now time.Time) (bool, error) {
	if s == nil || s.DB == nil {
		return false, errors.New("MCP 仓储未初始化")
	}
	// trimmed 是去空白后的待校验令牌；空令牌直接判定失败且不查库。
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return false, nil
	}
	// 先惰性清理已到期的宽限令牌，保证后续查询不会再命中它们。
	if // err 是到期宽限令牌清理语句的失败原因。
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM mcp_tokens WHERE kind=? AND expires_at<>0 AND expires_at<=?`,
		MCPTokenKindPrevious, now.Unix()); err != nil {
		return false, fmt.Errorf("清理到期宽限令牌失败: %w", err)
	}
	// hash 是待校验令牌的哈希，按唯一索引取行；攻击者不掌握令牌即无法构造该哈希。
	hash := hashMCPToken(trimmed)
	// rowHash 是命中行存储的令牌哈希，用于与计算哈希做常量时间比较。
	var rowHash string
	// kind 是命中行令牌类别（current/previous）。
	var kind string
	// expiresAt 是命中行失效 Unix 秒；0 表示不过期。
	var expiresAt int64
	// queryErr 是按哈希取行的结果；sql.ErrNoRows 与其他错误分开处理。
	queryErr := s.DB.QueryRowContext(ctx,
		`SELECT token_hash, kind, expires_at FROM mcp_tokens WHERE token_hash=?`, hash).
		Scan(&rowHash, &kind, &expiresAt)
	if queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("查询 MCP 令牌失败: %w", queryErr)
	}
	// hashMatch 以常量时间比较存储哈希与计算哈希，避免时序侧信道。
	hashMatch := subtle.ConstantTimeCompare([]byte(rowHash), []byte(hash)) == 1
	// notExpired 对设置了到期时间的宽限令牌做二次防御性检查。
	notExpired := expiresAt == 0 || expiresAt > now.Unix()
	if !hashMatch || !notExpired {
		return false, nil
	}
	if // err 是令牌最后使用时间刷新语句的失败原因；失败不否决本次鉴权但必须上抛。
	_, err := s.DB.ExecContext(ctx,
		`UPDATE mcp_tokens SET last_used_at=? WHERE token_hash=?`, now.Unix(), hash); err != nil {
		return false, fmt.Errorf("刷新 MCP 令牌使用时间失败: %w", err)
	}
	return true, nil
}

// TokenStatus 返回持久化令牌的非敏感配置态，供状态接口展示当前/宽限令牌是否存在及其时间。
func (s *MCPAuthStore) TokenStatus(ctx context.Context) (MCPTokenStatus, error) {
	if s == nil || s.DB == nil {
		return MCPTokenStatus{}, errors.New("MCP 仓储未初始化")
	}
	// status 是逐步填充的非敏感状态视图。
	status := MCPTokenStatus{}
	// rows 是全部令牌行；正常情况下至多两行（current、previous 各一行）。
	rows, err := s.DB.QueryContext(ctx,
		`SELECT kind, created_at, last_used_at, expires_at FROM mcp_tokens ORDER BY id`)
	if err != nil {
		return MCPTokenStatus{}, fmt.Errorf("查询 MCP 令牌状态失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		// kind 是当前令牌行的类别（current/previous）。
		kind := ""
		// createdAt 是当前令牌行的创建 Unix 秒。
		createdAt := int64(0)
		// lastUsedAt 是当前令牌行最后鉴权成功 Unix 秒。
		lastUsedAt := int64(0)
		// expiresAt 是当前令牌行的失效 Unix 秒，0 表示不过期。
		expiresAt := int64(0)
		if // err 是当前令牌行的扫描失败原因。
		err := rows.Scan(&kind, &createdAt, &lastUsedAt, &expiresAt); err != nil {
			return MCPTokenStatus{}, err
		}
		if kind == MCPTokenKindPrevious {
			status.HasPrevious = true
			status.PreviousExpiresAt = expiresAt
			continue
		}
		status.HasCurrent = true
		status.CurrentCreatedAt = createdAt
		status.CurrentLastUsedAt = lastUsedAt
	}
	return status, rows.Err()
}

// AddMCPAudit 写入一条调用审计；参数摘要在落库前强制脱敏，任何秘密值与明文卡密都不得入库。
func (s *MCPAuthStore) AddMCPAudit(ctx context.Context, record MCPAuditRecord) error {
	if s == nil || s.DB == nil {
		return errors.New("MCP 仓储未初始化")
	}
	// successValue 是布尔成功态的三方言整数形态，统一按 BIGINT 写入与扫描。
	successValue := 0
	if record.Success {
		successValue = 1
	}
	// safeArguments 是脱敏与截断后的参数 JSON；非 JSON 入参统一落空串，绝不原样落库。
	safeArguments := SanitizeMCPAuditArguments(record.Arguments)
	if // err 是审计行插入语句的失败原因。
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO mcp_call_audit
			(created_at, user_id, token_source, category, name, cookie_id, arguments, success, error_class, duration_ms)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		record.CreatedAt, record.UserID, record.TokenSource, record.Category, record.Name,
		strings.TrimSpace(record.CookieID), safeArguments, successValue,
		strings.TrimSpace(record.ErrorClass), record.DurationMS); err != nil {
		return fmt.Errorf("写入 MCP 调用审计失败: %w", err)
	}
	return nil
}

// ListMCPAudit 按条件分页查询调用审计，按时间倒序、ID 倒序返回，并给出满足条件的总行数。
func (s *MCPAuthStore) ListMCPAudit(ctx context.Context, filter MCPAuditFilter) (MCPAuditPage, error) {
	if s == nil || s.DB == nil {
		return MCPAuditPage{}, errors.New("MCP 仓储未初始化")
	}
	// clauses 保存动态 WHERE 片段，args 保存与其顺序一致的绑定参数。
	clauses := make([]string, 0, 4)
	// args 收集过滤参数，计数与查询共用。
	args := make([]any, 0, 4)
	if strings.TrimSpace(filter.Category) != "" {
		clauses = append(clauses, "category=?")
		args = append(args, strings.TrimSpace(filter.Category))
	}
	if strings.TrimSpace(filter.Name) != "" {
		clauses = append(clauses, "name=?")
		args = append(args, strings.TrimSpace(filter.Name))
	}
	if strings.TrimSpace(filter.CookieID) != "" {
		clauses = append(clauses, "cookie_id=?")
		args = append(args, strings.TrimSpace(filter.CookieID))
	}
	// 仅在显式指定 1（成功）或 -1（失败）时加过滤，零值默认返回全部。
	if filter.SuccessState == 1 || filter.SuccessState == -1 {
		// successValue 把 -1 失败过滤换算为数据库整数 0，1 保持为成功整数 1。
		successValue := 0
		if filter.SuccessState == 1 {
			successValue = 1
		}
		clauses = append(clauses, "success=?")
		args = append(args, successValue)
	}
	// where 是拼接后的 WHERE 子句；无条件时为空串。
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	// total 是满足过滤条件的总记录数。
	var total int
	if // err 是审计总数统计查询的失败原因。
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_call_audit`+where, args...).Scan(&total); err != nil {
		return MCPAuditPage{}, fmt.Errorf("统计 MCP 审计总数失败: %w", err)
	}
	// limit 是归一化后的分页大小。
	limit := filter.Limit
	if limit <= 0 {
		limit = mcpAuditDefaultLimit
	}
	if limit > mcpAuditMaxLimit {
		limit = mcpAuditMaxLimit
	}
	// offset 不允许为负。
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	// queryArgs 是在过滤参数后追加分页参数的完整参数列表。
	queryArgs := append(append([]any{}, args...), limit, offset)
	// rows 是当前页审计行。
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, created_at, user_id, token_source, category, name, cookie_id, arguments, success, error_class, duration_ms
		 FROM mcp_call_audit`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return MCPAuditPage{}, fmt.Errorf("查询 MCP 审计列表失败: %w", err)
	}
	defer rows.Close()
	// page 是即将填充的分页结果。
	page := MCPAuditPage{Total: total, Records: make([]MCPAuditRecord, 0, limit)}
	for rows.Next() {
		// record 是当前扫描出的审计行。
		var record MCPAuditRecord
		// successValue 是当前行的整数成功态，非 0 即成功。
		successValue := 0
		if // err 是当前审计行的扫描失败原因。
		err := rows.Scan(&record.ID, &record.CreatedAt, &record.UserID, &record.TokenSource,
			&record.Category, &record.Name, &record.CookieID, &record.Arguments,
			&successValue, &record.ErrorClass, &record.DurationMS); err != nil {
			return MCPAuditPage{}, err
		}
		record.Success = successValue != 0
		page.Records = append(page.Records, record)
	}
	return page, rows.Err()
}

// mcpAuditAllowedKeys 是审计参数白名单：只有这些非业务秘密的标识、分页与开关键允许保留值。
// 列表之外的键（消息正文、卡密数据、密钥、URL、图片等）一律丢弃，从源头阻断秘密写审计。
var mcpAuditAllowedKeys = map[string]struct{}{
	"account_id": {}, "cid": {}, "cookie_id": {}, "order_id": {}, "item_id": {},
	"rule_id": {}, "template_id": {}, "card_id": {}, "channel_id": {}, "binding_id": {},
	"user_id": {}, "target_user_id": {}, "job_id": {}, "batch_id": {}, "quick_reply_id": {},
	"buyer_id": {}, "notification_id": {}, "session_id": {}, "task_id": {}, "run_id": {},
	"keyword_id": {}, "item_reply_id": {}, "issue_kind": {}, "task_type": {}, "trigger": {},
	"trigger_type": {}, "index": {}, "status": {}, "state": {}, "kind": {}, "category": {},
	"type": {}, "page": {}, "page_size": {}, "limit": {}, "offset": {}, "confirm": {},
	"enabled": {}, "disabled": {}, "auto_rate_enabled": {}, "auto_polish_enabled": {},
	"polish_time": {}, "auto_delist_enabled": {}, "delist_time": {}, "multi_spec": {}, "multi_quantity_delivery": {}, "model": {},
	"start_time": {}, "end_time": {}, "date": {}, "resource": {}, "uri": {}, "prompt": {},
}

// SanitizeMCPAuditArguments 把工具入参 JSON 裁剪为只含白名单标量键的短 JSON 摘要。
// 非对象 JSON、解析失败输入一律返回空串；白名单字符串值按 rune 截断；嵌套结构与数组整体丢弃值。
func SanitizeMCPAuditArguments(raw string) string {
	// trimmed 是去空白后的原始参数；空串无需脱敏。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// decoded 仅接受顶层对象，数组与裸标量不记录。
	var decoded map[string]json.RawMessage
	if // err 是原始参数的 JSON 解析失败原因；非法输入按无摘要处理。
	err := json.Unmarshal([]byte(trimmed), &decoded); err != nil || decoded == nil {
		return ""
	}
	// safe 收集白名单键的标量值，类型由 json 编解码器保持。
	safe := make(map[string]any, len(decoded))
	// keys 用于排序输出，保证同一参数的审计摘要字节稳定。
	keys := make([]string, 0, len(decoded))
	// key、rawValue 是当前遍历到的入参键与其原始 JSON 值。
	for key, rawValue := range decoded {
		if // ok 表示该键是否在审计白名单内；非白名单键直接丢弃。
		_, ok := mcpAuditAllowedKeys[key]; !ok {
			continue
		}
		// scalar 仅保留布尔、数字与字符串三类标量。
		var scalar any
		if // err 是白名单值的 JSON 解码失败原因；解码失败的键不写入摘要。
		err := json.Unmarshal(rawValue, &scalar); err != nil {
			continue
		}
		// value 是当前标量值的类型分支视图。
		switch value := scalar.(type) {
		case bool:
			safe[key] = value
			keys = append(keys, key)
		case float64:
			safe[key] = value
			keys = append(keys, key)
		case string:
			safe[key] = truncateAuditString(value, mcpAuditValueLimit)
			keys = append(keys, key)
		default:
			// 嵌套对象与数组不展开、不计数，避免间接写入卡密或消息内容。
		}
	}
	if len(safe) == 0 {
		return ""
	}
	sort.Strings(keys)
	// ordered 保证输出键顺序确定；json.Marshal 对 map 本身也排序，这里有序遍历仅为可读性。
	ordered := make(map[string]any, len(keys))
	// key 是按字典序遍历到的白名单键名。
	for _, key := range keys {
		ordered[key] = safe[key]
	}
	// encoded 是最终落库的稳定 JSON；编码失败属于不应发生的分支，回落空串。
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// truncateAuditString 按 rune 截断审计字符串，超长时以省略号收尾，避免多字节字符被截断成乱码。
func truncateAuditString(value string, limit int) string {
	// runes 是字符串的 rune 切片视图。
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
