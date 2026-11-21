package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// notificationEventResponse 是站内通知事件的具名 HTTP 响应 DTO。
type notificationEventResponse struct {
	// ID 是事件稳定标识。
	ID int64 `json:"id"`
	// Scope 是事件作用域：tenant 或 system。
	Scope string `json:"scope"`
	// AccountID 是租户事件关联账号；系统事件省略。
	AccountID string `json:"account_id,omitempty"`
	// EventType 是通知事件编码。
	EventType string `json:"event_type"`
	// Level 是事件级别。
	Level string `json:"level"`
	// Title 是事件标题。
	Title string `json:"title"`
	// Body 是事件正文。
	Body string `json:"body"`
	// Read 表示事件是否已读。
	Read bool `json:"read"`
	// CreatedAt 是事件发生时间的 Unix 秒时间戳。
	CreatedAt int64 `json:"created_at"`
}

// notificationEventListResponse 是站内通知事件列表与未读数的具名响应 DTO。
type notificationEventListResponse struct {
	// Items 是当前视图可见的事件摘要。
	Items []notificationEventResponse `json:"items"`
	// UnreadCount 是当前视图的未读事件数。
	UnreadCount int `json:"unread_count"`
}

// notificationEventsReadAllResponse 是全部标记已读的具名响应 DTO。
type notificationEventsReadAllResponse struct {
	// Success 表示操作完成。
	Success bool `json:"success"`
	// Updated 是实际更新的事件数量。
	Updated int64 `json:"updated"`
}

// notificationEventsApplication 返回当前 Server 绑定的站内通知事件应用服务。
func (s *Server) notificationEventsApplication() NotificationEventsPort {
	return s.applicationServiceSet().notificationEvents
}

// systemAlertChannelsApplication 返回当前 Server 绑定的系统告警渠道应用服务。
func (s *Server) systemAlertChannelsApplication() SystemAlertChannelsPort {
	return s.applicationServiceSet().systemAlertChannels
}

// newNotificationEventResponse 把应用层事件摘要转换为传输 DTO。
func newNotificationEventResponse(event notificationsapp.EventSummary) notificationEventResponse {
	return notificationEventResponse{
		ID: event.ID, Scope: event.Scope, AccountID: event.AccountID, EventType: event.EventType,
		Level: event.Level, Title: event.Title, Body: event.Body, Read: event.Read, CreatedAt: event.CreatedAt,
	}
}

// newNotificationEventResponses 批量转换事件摘要，保持列表顺序稳定。
func newNotificationEventResponses(events []notificationsapp.EventSummary) []notificationEventResponse {
	// responses 保存转换后的事件响应列表。
	responses := make([]notificationEventResponse, 0, len(events))
	// event 是当前待转换的应用层事件摘要。
	for _, event := range events {
		responses = append(responses, newNotificationEventResponse(event))
	}
	return responses
}

// listNotificationEvents 返回当前用户的租户站内通知事件与未读数。
func (s *Server) listNotificationEvents(w http.ResponseWriter, r *http.Request) {
	// limit 保存列表页请求的最大条数，超出范围时由应用服务收口。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// items、unread、err 保存事件列表、未读数与查询错误。
	items, unread, err := s.notificationEventsApplication().ListForUser(r.Context(), authSess(r).UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知事件失败")
		return
	}
	writeJSON(w, http.StatusOK, notificationEventListResponse{Items: newNotificationEventResponses(items), UnreadCount: unread})
}

// markNotificationEventRead 标记当前用户的单条租户事件已读。
func (s *Server) markNotificationEventRead(w http.ResponseWriter, r *http.Request) {
	// eventID、err 保存路径中的事件 ID 及解析错误。
	eventID, err := strconv.ParseInt(chi.URLParam(r, "event_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// err 保存应用层标记结果。
	if err := s.notificationEventsApplication().MarkReadForUser(r.Context(), authSess(r).UserID, eventID); err != nil {
		writeNotificationEventError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// markAllNotificationEventsRead 标记当前用户的全部租户事件已读。
func (s *Server) markAllNotificationEventsRead(w http.ResponseWriter, r *http.Request) {
	// updated、err 保存实际更新数量与数据库错误。
	updated, err := s.notificationEventsApplication().MarkAllReadForUser(r.Context(), authSess(r).UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "标记通知已读失败")
		return
	}
	writeJSON(w, http.StatusOK, notificationEventsReadAllResponse{Success: true, Updated: updated})
}

// listAdminSystemEvents 返回管理员可见的系统级站内通知事件与未读数。
func (s *Server) listAdminSystemEvents(w http.ResponseWriter, r *http.Request) {
	// limit 保存管理员列表页请求的最大条数，超出范围时由应用服务收口。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// items、unread、err 保存系统事件列表、未读数与查询错误。
	items, unread, err := s.notificationEventsApplication().ListForAdmin(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询系统告警失败")
		return
	}
	writeJSON(w, http.StatusOK, notificationEventListResponse{Items: newNotificationEventResponses(items), UnreadCount: unread})
}

// markAdminSystemEventRead 标记单条系统级事件已读。
func (s *Server) markAdminSystemEventRead(w http.ResponseWriter, r *http.Request) {
	// eventID、err 保存路径中的事件 ID 及解析错误。
	eventID, err := strconv.ParseInt(chi.URLParam(r, "event_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// err 保存应用层标记结果。
	if err := s.notificationEventsApplication().MarkReadForAdmin(r.Context(), eventID); err != nil {
		writeNotificationEventError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// markAllAdminSystemEventsRead 标记全部系统级事件已读。
func (s *Server) markAllAdminSystemEventsRead(w http.ResponseWriter, r *http.Request) {
	// updated、err 保存实际更新数量与数据库错误。
	updated, err := s.notificationEventsApplication().MarkAllReadForAdmin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "标记系统告警已读失败")
		return
	}
	writeJSON(w, http.StatusOK, notificationEventsReadAllResponse{Success: true, Updated: updated})
}

// writeNotificationEventError 把站内事件应用错误映射为统一 HTTP 错误响应。
func writeNotificationEventError(w http.ResponseWriter, err error) {
	if errors.Is(err, notificationsapp.ErrEventInvalidInput) {
		writeErr(w, http.StatusBadRequest, "通知事件参数无效")
		return
	}
	if errors.Is(err, notificationsapp.ErrEventNotFound) {
		writeErr(w, http.StatusNotFound, "通知事件不存在")
		return
	}
	writeErr(w, http.StatusInternalServerError, "操作通知事件失败")
}
