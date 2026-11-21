package notifications

import (
	"context"
	"errors"
)

var (
	// ErrEventNotFound 表示站内通知事件不存在或不属于当前用户。
	ErrEventNotFound = errors.New("通知事件不存在")
	// ErrEventInvalidInput 表示站内通知事件的查询或标记参数无效。
	ErrEventInvalidInput = errors.New("通知事件参数无效")
)

// EventSummary 是站内通知中心展示的非敏感事件摘要。
type EventSummary struct {
	// ID 是事件稳定标识，用于标记已读。
	ID int64
	// Scope 是事件作用域，区分租户视图与系统视图。
	Scope string
	// AccountID 是租户事件关联的闲鱼账号；系统事件为空。
	AccountID string
	// EventType 是通知事件编码。
	EventType string
	// Level 是事件级别。
	Level string
	// Title 是事件标题。
	Title string
	// Body 是事件正文；只返回给有权查看该事件范围的调用方。
	Body string
	// Read 表示事件是否已读。
	Read bool
	// CreatedAt 是事件发生时间的 Unix 秒时间戳。
	CreatedAt int64
}

// EventRepository 定义站内通知事件用例所需的最小持久化端口。
type EventRepository interface {
	// ListForUser 查询指定用户的租户事件。
	ListForUser(ctx context.Context, userID int64, limit int) ([]EventSummary, error)
	// CountUnreadForUser 统计指定用户的未读租户事件。
	CountUnreadForUser(ctx context.Context, userID int64) (int, error)
	// MarkReadForUser 标记指定用户的单条租户事件已读。
	MarkReadForUser(ctx context.Context, userID, eventID int64) (bool, error)
	// MarkAllReadForUser 标记指定用户的全部租户事件已读。
	MarkAllReadForUser(ctx context.Context, userID int64) (int64, error)
	// ListForAdmin 查询系统级事件。
	ListForAdmin(ctx context.Context, limit int) ([]EventSummary, error)
	// CountUnreadForAdmin 统计未读系统级事件。
	CountUnreadForAdmin(ctx context.Context) (int, error)
	// MarkReadForAdmin 标记单条系统级事件已读。
	MarkReadForAdmin(ctx context.Context, eventID int64) (bool, error)
	// MarkAllReadForAdmin 标记全部系统级事件已读。
	MarkAllReadForAdmin(ctx context.Context) (int64, error)
}

// EventInboxService 编排租户与系统两个站内通知视图，不依赖 HTTP 或数据库模型。
type EventInboxService struct {
	// repository 保存站内事件的最小持久化端口。
	repository EventRepository
}

// NewEventInboxService 构造站内通知中心应用服务。
func NewEventInboxService(repository EventRepository) *EventInboxService {
	return &EventInboxService{repository: repository}
}

// ListForUser 返回当前用户的租户事件与未读数。
func (s *EventInboxService) ListForUser(ctx context.Context, userID int64, limit int) ([]EventSummary, int, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, 0, ErrEventInvalidInput
	}
	// items、listErr 保存当前用户可见的事件与读取错误。
	items, listErr := s.repository.ListForUser(ctx, userID, clampEventLimit(limit))
	if listErr != nil {
		return nil, 0, listErr
	}
	// unread、countErr 保存当前用户的未读事件数。
	unread, countErr := s.repository.CountUnreadForUser(ctx, userID)
	if countErr != nil {
		return nil, 0, countErr
	}
	return items, unread, nil
}

// MarkReadForUser 标记当前用户的单条事件已读；事件不存在或不属于该用户时返回统一错误。
func (s *EventInboxService) MarkReadForUser(ctx context.Context, userID, eventID int64) error {
	if s == nil || s.repository == nil || userID <= 0 || eventID <= 0 {
		return ErrEventInvalidInput
	}
	// updated、markErr 保存标记结果与持久化错误。
	updated, markErr := s.repository.MarkReadForUser(ctx, userID, eventID)
	if markErr != nil {
		return markErr
	}
	if !updated {
		return ErrEventNotFound
	}
	return nil
}

// MarkAllReadForUser 标记当前用户全部事件已读，返回实际更新数量。
func (s *EventInboxService) MarkAllReadForUser(ctx context.Context, userID int64) (int64, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return 0, ErrEventInvalidInput
	}
	return s.repository.MarkAllReadForUser(ctx, userID)
}

// ListForAdmin 返回系统级事件与未读数，仅管理员路由可调用。
func (s *EventInboxService) ListForAdmin(ctx context.Context, limit int) ([]EventSummary, int, error) {
	if s == nil || s.repository == nil {
		return nil, 0, ErrEventInvalidInput
	}
	// items、listErr 保存系统视图事件与读取错误。
	items, listErr := s.repository.ListForAdmin(ctx, clampEventLimit(limit))
	if listErr != nil {
		return nil, 0, listErr
	}
	// unread、countErr 保存系统视图未读数。
	unread, countErr := s.repository.CountUnreadForAdmin(ctx)
	if countErr != nil {
		return nil, 0, countErr
	}
	return items, unread, nil
}

// MarkReadForAdmin 标记单条系统级事件已读；事件不存在时返回统一错误。
func (s *EventInboxService) MarkReadForAdmin(ctx context.Context, eventID int64) error {
	if s == nil || s.repository == nil || eventID <= 0 {
		return ErrEventInvalidInput
	}
	// updated、markErr 保存标记结果与持久化错误。
	updated, markErr := s.repository.MarkReadForAdmin(ctx, eventID)
	if markErr != nil {
		return markErr
	}
	if !updated {
		return ErrEventNotFound
	}
	return nil
}

// MarkAllReadForAdmin 标记全部系统级事件已读，返回实际更新数量。
func (s *EventInboxService) MarkAllReadForAdmin(ctx context.Context) (int64, error) {
	if s == nil || s.repository == nil {
		return 0, ErrEventInvalidInput
	}
	return s.repository.MarkAllReadForAdmin(ctx)
}

// clampEventLimit 把站内事件列表上限限制在 1 到 100 之间，超范围时使用默认 50。
func clampEventLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 50
	}
	return limit
}
