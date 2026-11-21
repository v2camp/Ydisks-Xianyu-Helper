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

// listSystemAlertChannels 返回管理员配置的系统告警渠道摘要。
func (s *Server) listSystemAlertChannels(w http.ResponseWriter, r *http.Request) {
	// channels、err 保存系统告警渠道摘要与查询错误。
	channels, err := s.systemAlertChannelsApplication().List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询系统告警渠道失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelResponses(channels))
}

// getSystemAlertChannelEditor 返回系统告警渠道的非敏感编辑器视图。
func (s *Server) getSystemAlertChannelEditor(w http.ResponseWriter, r *http.Request) {
	// channelID、err 保存路径中的渠道 ID 及解析错误。
	channelID, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// editor、lookupErr 保存系统渠道编辑器视图与查询错误。
	editor, lookupErr := s.systemAlertChannelsApplication().GetEditor(r.Context(), channelID)
	if lookupErr != nil {
		writeSystemAlertChannelError(w, lookupErr, "查询系统告警渠道失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelEditorResponse(editor))
}

// createSystemAlertChannel 创建管理员系统告警渠道。
func (s *Server) createSystemAlertChannel(w http.ResponseWriter, r *http.Request) {
	// req 保存具名系统渠道创建请求；Config 只写入应用端口。
	var req notificationChannelCreateRequest
	// err 保存 JSON 解码错误。
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	// channelID、err 保存创建结果与应用错误。
	channelID, err := s.systemAlertChannelsApplication().Create(r.Context(), authSess(r).UserID, notificationsapp.ChannelInput{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled,
	})
	if err != nil {
		writeSystemAlertChannelError(w, err, "创建系统告警渠道失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: channelID})
}

// updateSystemAlertChannel 部分更新系统告警渠道。
func (s *Server) updateSystemAlertChannel(w http.ResponseWriter, r *http.Request) {
	// channelID、err 保存路径中的渠道 ID 及解析错误。
	channelID, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// req 保存具名系统渠道部分更新请求；Config 不会进入响应。
	var req notificationChannelPatchRequest
	// err 保存 JSON 解码错误。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 保存应用层部分更新结果。
	if err := s.systemAlertChannelsApplication().Update(r.Context(), authSess(r).UserID, channelID, notificationsapp.ChannelPatch{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled,
	}); err != nil {
		writeSystemAlertChannelError(w, err, "更新系统告警渠道失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteSystemAlertChannel 删除系统告警渠道。
func (s *Server) deleteSystemAlertChannel(w http.ResponseWriter, r *http.Request) {
	// channelID、err 保存路径中的渠道 ID 及解析错误。
	channelID, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// err 保存应用层删除结果。
	if err := s.systemAlertChannelsApplication().Delete(r.Context(), channelID); err != nil {
		writeSystemAlertChannelError(w, err, "删除系统告警渠道失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// testSystemAlertChannel 向指定系统告警渠道发送一条测试通知。
func (s *Server) testSystemAlertChannel(w http.ResponseWriter, r *http.Request) {
	// channelID、err 保存路径中的渠道 ID 及解析错误。
	channelID, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// err 保存应用层测试发送结果。
	if err := s.systemAlertChannelsApplication().Test(r.Context(), channelID, time.Now()); err != nil {
		if errors.Is(err, notificationsapp.ErrNotifierUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
			return
		}
		if errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusNotFound, "系统告警渠道不存在")
			return
		}
		if s.Logger != nil {
			s.Logger.Warn("系统告警渠道测试发送失败", "channel_id", channelID, "err", logsafe.ExternalError(err))
		}
		writeErr(w, http.StatusInternalServerError, "发送失败，请检查渠道配置和网络")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// writeSystemAlertChannelError 把系统告警渠道应用错误映射为统一 HTTP 错误响应。
func writeSystemAlertChannelError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, notificationsapp.ErrChannelInvalidInput) {
		writeErr(w, http.StatusBadRequest, "系统告警渠道参数无效")
		return
	}
	if errors.Is(err, notificationsapp.ErrChannelNotFound) || errors.Is(err, notificationsapp.ErrChannelForbidden) {
		writeErr(w, http.StatusNotFound, "系统告警渠道不存在")
		return
	}
	writeErr(w, http.StatusInternalServerError, fallback)
}
