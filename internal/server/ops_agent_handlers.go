// ops_agent_handlers.go 提供运营 Agent 对话的管理员 HTTP 接口：会话列表、消息列表与 Web 人工发送。
//
// 处理器只做参数解析、应用用例调用与错误映射；SQL、凭据与平台发送全部留在下层。
package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	opschatapp "xianyu-go/internal/application/opschat"
	"xianyu-go/internal/auth"
)

// opsConversationDTO 是运营会话对外暴露的具名 DTO，不直接复用应用或数据库模型。
type opsConversationDTO struct {
	// ID 是会话主键。
	ID int64 `json:"id"`
	// Channel 是渠道标识，当前为 qq。
	Channel string `json:"channel"`
	// TargetOpenID 是平台会话目标 openid。
	TargetOpenID string `json:"target_openid"`
	// IsGroup 表示目标是否为群聊。
	IsGroup bool `json:"is_group"`
	// LastMessageAt 是最近消息的 Unix 毫秒时间。
	LastMessageAt int64 `json:"last_message_at"`
	// LastMessagePreview 是最近消息的截断预览。
	LastMessagePreview string `json:"last_message_preview"`
}

// opsMessageDTO 是运营消息对外暴露的具名 DTO。
type opsMessageDTO struct {
	// ID 是消息主键。
	ID int64 `json:"id"`
	// ConversationID 是所属会话主键。
	ConversationID int64 `json:"conversation_id"`
	// Direction 是消息方向：inbound 或 outbound。
	Direction string `json:"direction"`
	// Source 是消息来源：inbound、command 或 operator。
	Source string `json:"source"`
	// Content 是消息正文。
	Content string `json:"content"`
	// CreatedAt 是消息写入时的 Unix 毫秒时间。
	CreatedAt int64 `json:"created_at"`
}

// opsConversationListResponse 是运营会话分页响应 DTO。
type opsConversationListResponse struct {
	// Conversations 是当前页会话列表。
	Conversations []opsConversationDTO `json:"conversations"`
	// Total 是会话总数。
	Total int `json:"total"`
	// Page 是当前页码，从 1 开始。
	Page int `json:"page"`
	// PageSize 是本次请求使用的页大小。
	PageSize int `json:"page_size"`
	// TotalPages 是总页数，页大小无效时为 1。
	TotalPages int `json:"total_pages"`
}

// opsMessageListResponse 是运营消息分页响应 DTO。
type opsMessageListResponse struct {
	// Messages 是按时间正序排列的当前页消息。
	Messages []opsMessageDTO `json:"messages"`
	// HasMore 表示是否还有更早的消息。
	HasMore bool `json:"has_more"`
}

// opsSendMessageRequest 是运营人工发送的请求 DTO。
type opsSendMessageRequest struct {
	// Content 是纯文本正文；去空白后 1 到 1500 字符。
	Content string `json:"content"`
}

// opsSendMessageResponse 是运营人工发送成功响应 DTO。
type opsSendMessageResponse struct {
	// Message 是已落库的出站消息。
	Message opsMessageDTO `json:"message"`
	// Conversation 是更新摘要后的会话快照。
	Conversation opsConversationDTO `json:"conversation"`
}

// mountVersionedOpsAgentRoutes 挂载运营 Agent 对话的 `/api/v1` 管理员入口。
func (s *Server) mountVersionedOpsAgentRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Use(auth.RequireAdmin)
		r.Get("/api/v1/agent/ops/conversations", s.opsConversations)
		r.Get("/api/v1/agent/ops/conversations/{id}/messages", s.opsConversationMessages)
		r.Post("/api/v1/agent/ops/conversations/{id}/messages", s.sendOpsConversationMessage)
	})
}

// opsConversations 返回运营会话分页列表。
func (s *Server) opsConversations(w http.ResponseWriter, r *http.Request) {
	// page、pageSize 是收敛前的分页参数；非法或缺失时使用默认值。
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	// pageSize 是收敛前的页大小，非法或缺失时使用默认值。
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 20)
	// result、err 是应用层分页结果及查询错误。
	result, err := s.opsAgentApplication().List(r.Context(), page, pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取运营会话失败")
		return
	}
	// totalPages 是总页数；页大小无效时保持 1，避免除零。
	totalPages := 1
	if result.PageSize > 0 {
		totalPages = (result.Total + result.PageSize - 1) / result.PageSize
	}
	writeJSON(w, http.StatusOK, opsConversationListResponse{
		Conversations: newOpsConversationDTOs(result.Conversations), Total: result.Total,
		Page: result.Page, PageSize: result.PageSize, TotalPages: totalPages,
	})
}

// opsConversationMessages 返回指定运营会话的消息页。
func (s *Server) opsConversationMessages(w http.ResponseWriter, r *http.Request) {
	// conversationID 是路径中的会话主键；非法值属于请求错误。
	conversationID, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil || conversationID <= 0 {
		writeErr(w, http.StatusBadRequest, "运营会话标识无效")
		return
	}
	// beforeID 是更早消息的游标；缺失时读取最新一页。
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	// limit 是本次请求的条数上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// page、err 是应用层消息页及查询错误。
	page, err := s.opsAgentApplication().Messages(r.Context(), conversationID, beforeID, limit)
	if err != nil {
		writeOpsAgentError(w, err, "读取运营消息失败")
		return
	}
	writeJSON(w, http.StatusOK, opsMessageListResponse{Messages: newOpsMessageDTOs(page.Messages), HasMore: page.HasMore})
}

// sendOpsConversationMessage 向指定运营会话发送一条 Web 人工消息。
func (s *Server) sendOpsConversationMessage(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话，用于读取该用户的 QQ 凭据。
	sess := auth.SessionFromContext(r.Context())
	// conversationID 是路径中的会话主键；非法值属于请求错误。
	conversationID, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil || conversationID <= 0 {
		writeErr(w, http.StatusBadRequest, "运营会话标识无效")
		return
	}
	// payload 是本次发送请求 DTO。
	var payload opsSendMessageRequest
	// err 是请求体 JSON 解析错误。
	if err := decodeJSON(r, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	// message、conversation、sendErr 是发送结果及失败原因。
	message, conversation, sendErr := s.opsAgentApplication().Send(r.Context(), opschatapp.SendInput{
		UserID: sess.UserID, ConversationID: conversationID, Content: payload.Content,
	})
	if sendErr != nil {
		writeOpsAgentError(w, sendErr, "运营消息发送失败")
		return
	}
	writeJSON(w, http.StatusCreated, opsSendMessageResponse{
		Message: newOpsMessageDTO(message), Conversation: newOpsConversationDTO(conversation),
	})
}

// writeOpsAgentError 把运营对话应用错误映射为统一 HTTP 响应。
func writeOpsAgentError(w http.ResponseWriter, err error, fallbackMessage string) {
	switch {
	case errors.Is(err, opschatapp.ErrInvalidContent):
		writeErr(w, http.StatusBadRequest, "消息内容为空或超过 1500 字符")
	case errors.Is(err, opschatapp.ErrConversationNotFound):
		writeErr(w, http.StatusNotFound, "运营会话不存在")
	case errors.Is(err, opschatapp.ErrChannelNotConfigured):
		writeErr(w, http.StatusConflict, "QQ 渠道凭据未配置")
	case errors.Is(err, opschatapp.ErrSendFailed):
		writeErr(w, http.StatusBadGateway, "QQ 消息发送失败")
	default:
		writeErr(w, http.StatusInternalServerError, fallbackMessage)
	}
}

// newOpsConversationDTO 把应用层运营会话转换为 HTTP DTO。
func newOpsConversationDTO(conversation opschatapp.Conversation) opsConversationDTO {
	return opsConversationDTO{
		ID: conversation.ID, Channel: conversation.Channel, TargetOpenID: conversation.TargetOpenID,
		IsGroup: conversation.IsGroup, LastMessageAt: conversation.LastMessageAt, LastMessagePreview: conversation.LastMessagePreview,
	}
}

// newOpsConversationDTOs 批量转换运营会话，保持响应不暴露应用或数据库模型。
func newOpsConversationDTOs(conversations []opschatapp.Conversation) []opsConversationDTO {
	// result 是转换后的会话 DTO 列表。
	result := make([]opsConversationDTO, 0, len(conversations))
	// conversation 是当前待转换的应用层会话。
	for _, conversation := range conversations {
		result = append(result, newOpsConversationDTO(conversation))
	}
	return result
}

// newOpsMessageDTO 把应用层运营消息转换为 HTTP DTO。
func newOpsMessageDTO(message opschatapp.Message) opsMessageDTO {
	return opsMessageDTO{
		ID: message.ID, ConversationID: message.ConversationID, Direction: message.Direction,
		Source: message.Source, Content: message.Content, CreatedAt: message.CreatedAt,
	}
}

// newOpsMessageDTOs 批量转换运营消息。
func newOpsMessageDTOs(messages []opschatapp.Message) []opsMessageDTO {
	// result 是转换后的消息 DTO 列表。
	result := make([]opsMessageDTO, 0, len(messages))
	// message 是当前待转换的应用层消息。
	for _, message := range messages {
		result = append(result, newOpsMessageDTO(message))
	}
	return result
}
