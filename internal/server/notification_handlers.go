package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/logsafe"
)

// notificationChannelCreateRequest 是创建通知渠道的具名 HTTP 请求 DTO；Config 只写入应用端口。
type notificationChannelCreateRequest struct {
	// Name 是渠道名称。
	Name string `json:"name"`
	// Type 是渠道协议类型。
	Type string `json:"type"`
	// Config 是渠道敏感配置 JSON，禁止进入响应或日志。
	Config string `json:"config"`
	// EventTypes 是渠道订阅事件类型编码。
	EventTypes string `json:"event_types"`
	// Enabled 表示渠道是否启用。
	Enabled bool `json:"enabled"`
}

// notificationChannelPatchRequest 是更新通知渠道的具名部分更新 DTO。
type notificationChannelPatchRequest struct {
	// Name 是可选的新渠道名称。
	Name *string `json:"name"`
	// Type 是可选的新渠道协议类型。
	Type *string `json:"type"`
	// Config 是可选的新敏感配置 JSON，禁止进入响应或日志。
	Config *string `json:"config"`
	// EventTypes 是可选的新订阅事件类型编码。
	EventTypes *string `json:"event_types"`
	// Enabled 是可选的新启用状态。
	Enabled *bool `json:"enabled"`
}

// notificationChannelsApplication 返回当前 Server 绑定的通知渠道应用服务。
func (s *Server) notificationChannelsApplication() NotificationChannelsPort {
	return s.applicationServiceSet().notificationChannels
}

// uncertainNotificationsApplication 返回当前 Server 绑定的通知不确定状态应用服务。
func (s *Server) uncertainNotificationsApplication() UncertainNotificationsPort {
	return s.applicationServiceSet().uncertainNotifications
}

// mountNotificationsReal 通知渠道。
func (s *Server) mountNotificationsReal(r chi.Router) {
	r.Get("/notification-channels", s.listChannels)
	r.Get("/notification-channels/{channel_id}", s.getChannelEditor)
	r.Post("/notification-channels", s.createChannel)
	r.Put("/notification-channels/{channel_id}", s.updateChannel)
	r.Delete("/notification-channels/{channel_id}", s.deleteChannel)
	r.Post("/notification-channels/{channel_id}/test", s.testChannel)
}

// getChannelEditor 返回当前用户编辑渠道所需的非敏感配置字段。
func (s *Server) getChannelEditor(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// editor、lookupErr 保存按用户归属读取的脱敏编辑器视图及读取错误。
	editor, lookupErr := s.notificationChannelsApplication().GetChannelEditor(r.Context(), authSess(r).UserID, id)
	if lookupErr != nil {
		if errors.Is(lookupErr, notificationsapp.ErrChannelForbidden) || errors.Is(lookupErr, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelEditorResponse(editor))
}

// listUncertainNotifications 返回当前用户渠道对应的不确定通知摘要，不暴露正文或凭证。
func (s *Server) listUncertainNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前已认证用户，用于限制通知渠道归属范围。
	sess := authSess(r)
	// limit 保存运维列表页请求的最大条数，超出范围时使用数据库默认上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	// items 保存当前用户可见的不确定通知摘要。
	items, total, err := s.uncertainNotificationsApplication().ListForUser(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知状态失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationUncertainOutboxResponse(items, total, false))
}

// listAdminUncertainNotifications 返回全局不确定通知摘要，仅管理员路由可访问。
func (s *Server) listAdminUncertainNotifications(w http.ResponseWriter, r *http.Request) {
	// limit 保存管理员运维查询的最大条数，超出范围时使用数据库默认上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// items 保存所有用户渠道的不确定通知摘要，但不包含正文和错误原文。
	items, total, err := s.uncertainNotificationsApplication().ListForAdmin(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知状态失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationUncertainOutboxResponse(items, total, true))
}

// listChannels 封装list渠道列表业务协调。
func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// chs、err 保存通知渠道非敏感摘要及查询错误。
	chs, err := s.notificationChannelsApplication().ListChannels(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelResponses(chs))
}

// createChannel 封装create渠道业务协调。
func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// req 保存具名通知渠道创建请求；Config 只写入应用端口。
	var req notificationChannelCreateRequest
	if // err 保存 JSON 解码错误。
	err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	// id、err 保存渠道创建结果及应用错误。
	id, err := s.notificationChannelsApplication().CreateChannel(r.Context(), sess.UserID, notificationsapp.ChannelInput{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateChannel 封装update渠道业务协调。
func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// req 保存具名通知渠道部分更新请求；Config 不会进入响应。
	var req notificationChannelPatchRequest
	if // err 保存 JSON 解码错误。
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// err 保存应用层部分更新结果。
	if err := s.notificationChannelsApplication().UpdateChannel(r.Context(), sess.UserID, id, notificationsapp.ChannelPatch{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled,
	}); err != nil {
		if errors.Is(err, notificationsapp.ErrChannelInvalidInput) {
			writeErr(w, http.StatusBadRequest, "通知渠道更新参数无效")
			return
		}
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteChannel 封装delete渠道业务协调。
func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// err 保存应用层删除结果。
	if err := s.notificationChannelsApplication().DeleteChannel(r.Context(), sess.UserID, id); err != nil {
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// testChannel 向指定渠道发送一条测试通知，便于用户验证配置是否正确。
func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// err 保存应用层测试发送结果。
	if err := s.notificationChannelsApplication().TestChannel(r.Context(), authSess(r).UserID, id, time.Now()); err != nil {
		if errors.Is(err, notificationsapp.ErrNotifierUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
			return
		}
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		if s.Logger != nil {
			s.Logger.Warn("通知渠道测试发送失败", "channel_id", id, "err", logsafe.ExternalError(err))
		}
		writeErr(w, http.StatusInternalServerError, "发送失败，请检查渠道配置和网络")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
