package qqbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// commandTimeout 是单个命令读取与渲染的最长等待时间，避免慢查询拖住入站网关。
const commandTimeout = 15 * time.Second

// defaultChatsPerAccount 是「会话」命令默认每个账号提取的会话数量。
const defaultChatsPerAccount = 3

// maxChatsPerAccount 是「会话」命令每个账号允许提取的会话数量上限。
const maxChatsPerAccount = 10

// ErrCommandsDisabled 表示入站命令总开关未打开，调用方不应回复任何内容。
var ErrCommandsDisabled = errors.New("qq 入站命令未启用")

// ErrUnauthorized 表示发送者不在命令白名单内，调用方应回复授权提示。
var ErrUnauthorized = errors.New("qq 入站命令发送者未授权")

// Service 编排入站命令：授权校验、命令解析、数据读取与文本渲染。
type Service struct {
	// sales 提供当日账号级销量读取。
	sales SalesReader
	// health 提供系统健康快照读取。
	health HealthReader
	// chats 提供分组会话提取。
	chats ChatReader
	// identity 解析命令使用的固定管理员身份。
	identity IdentityResolver
	// authorizer 判断命令是否对当前发送者开放。
	authorizer Authorizer
	// now 返回当前时刻，便于测试固定时间。
	now func() time.Time
}

// NewService 构造入站命令服务；任一端口为 nil 时命令执行会返回明确的不可用错误。
func NewService(sales SalesReader, health HealthReader, chats ChatReader, identity IdentityResolver, authorizer Authorizer) *Service {
	return &Service{sales: sales, health: health, chats: chats, identity: identity, authorizer: authorizer, now: time.Now}
}

// Handle 处理一条入站消息，返回待回复的中文文本。
// openID 是发送者的平台标识，用于白名单校验；text 是用户输入的原始文本。
// 返回 ErrCommandsDisabled 表示开关关闭，调用方不应回复；返回 ErrUnauthorized 表示未授权。
func (s *Service) Handle(ctx context.Context, openID, text string) (string, error) {
	if s == nil || s.authorizer == nil {
		return "", ErrCommandsDisabled
	}
	if !s.authorizer.Enabled(ctx) {
		return "", ErrCommandsDisabled
	}
	// sender 是去空白后的发送者标识。
	sender := strings.TrimSpace(openID)
	if !s.authorizer.Allowed(ctx, sender) {
		return "", ErrUnauthorized
	}
	// command 是归一化文本解析出的命令标识。
	command := ParseCommand(text)
	// userID 是命令使用的固定管理员身份；err 表示身份不可用。
	userID, err := s.resolveAdmin(ctx)
	if err != nil {
		return "", err
	}
	// reply 是命令执行并渲染后的回复文本；execErr 表示数据读取失败。
	reply, execErr := s.execute(ctx, command, userID)
	if execErr != nil {
		return "", execErr
	}
	return truncateReply(reply), nil
}

// SetClock 注入时间源，便于测试固定健康文案中的观测时刻。
func (s *Service) SetClock(clock func() time.Time) {
	if s != nil {
		s.now = clock
	}
}

// FormatUnauthorized 渲染未授权发送者的提示文案，并回显其 openid 便于管理员加入白名单。
func FormatUnauthorized(openID string) string {
	// sender 是去空白后的发送者标识。
	sender := strings.TrimSpace(openID)
	if sender == "" {
		return "未授权执行 QQ 命令，请联系管理员开通。"
	}
	return fmt.Sprintf("未授权执行 QQ 命令。如需开通，请把标识 %s 加入系统设置「QQ 连接器」的命令白名单。", sender)
}

// FormatFailure 渲染命令执行失败时的兜底文案，不泄露内部错误细节。
func FormatFailure() string {
	return "命令执行失败，请稍后重试或查看服务日志。"
}

// execute 按命令标识读取数据并渲染文本；未知命令回退到命令清单。
func (s *Service) execute(ctx context.Context, command Command, userID int64) (string, error) {
	switch command {
	case CommandSales:
		// snapshot、err 保存当日销量快照及其读取错误。
		snapshot, err := s.readSales(ctx, userID)
		if err != nil {
			return "", err
		}
		return FormatSales(snapshot), nil
	case CommandHealth:
		// snapshot、err 保存系统健康快照及其读取错误。
		snapshot, err := s.readHealth(ctx, userID)
		if err != nil {
			return "", err
		}
		return FormatHealth(snapshot, s.currentTime()), nil
	case CommandChats:
		// digest、err 保存分组会话摘要及其读取错误。
		digest, err := s.readChats(ctx, userID)
		if err != nil {
			return "", err
		}
		return FormatChats(digest), nil
	default:
		return "未识别的命令。\n" + FormatCommandHelp(), nil
	}
}

// resolveAdmin 解析命令使用的固定管理员身份；身份不可用时返回错误。
func (s *Service) resolveAdmin(ctx context.Context) (int64, error) {
	if s.identity == nil {
		return 0, fmt.Errorf("qq 入站命令缺少管理员身份解析能力")
	}
	// userID 是解析出的管理员标识；err 表示管理员不存在。
	userID, err := s.identity.AdminUserID(ctx)
	if err != nil {
		return 0, fmt.Errorf("解析管理员身份失败: %w", err)
	}
	if userID <= 0 {
		return 0, fmt.Errorf("管理员身份无效")
	}
	return userID, nil
}

// readSales 在有界超时内读取当日销量。
func (s *Service) readSales(ctx context.Context, userID int64) (SalesSnapshot, error) {
	if s.sales == nil {
		return SalesSnapshot{}, fmt.Errorf("qq 入站命令缺少销量读取能力")
	}
	// queryCtx、cancel 为本次读取提供有界取消路径。
	queryCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return s.sales.TodaySales(queryCtx, userID)
}

// readHealth 在有界超时内读取系统健康快照。
func (s *Service) readHealth(ctx context.Context, userID int64) (HealthSnapshot, error) {
	if s.health == nil {
		return HealthSnapshot{}, fmt.Errorf("qq 入站命令缺少健康读取能力")
	}
	// queryCtx、cancel 为本次读取提供有界取消路径。
	queryCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return s.health.Snapshot(queryCtx, userID)
}

// readChats 在有界超时内读取分组会话摘要。
func (s *Service) readChats(ctx context.Context, userID int64) (ChatDigest, error) {
	if s.chats == nil {
		return ChatDigest{}, fmt.Errorf("qq 入站命令缺少会话读取能力")
	}
	// queryCtx、cancel 为本次读取提供有界取消路径。
	queryCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return s.chats.RecentChats(queryCtx, userID, defaultChatsPerAccount)
}

// currentTime 返回当前时刻；未注入时间源时回落系统时间。
func (s *Service) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}
