// agent_conversation_handlers.go 提供登录用户可读的客服接管会话与消息接口。
//
// 会话列表只覆盖被 AI 回复过的会话，并附带接管轮次近似值；消息读取复用聊天历史用例的归属校验，
// 不新增任何跨用户读取路径。接口不返回 Cookie、Token 或其它凭证字段。
package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// agentTakeoverSessionDTO 是客服接管会话响应 DTO，在普通会话字段之外附带接管轮次。
type agentTakeoverSessionDTO struct {
	// chatSessionDTO 提供与聊天页一致的会话字段；匿名嵌入使 JSON 字段保持扁平。
	chatSessionDTO
	// TakeoverCount 是该会话中 AI 生成过回复的轮次数近似值。
	TakeoverCount int64 `json:"takeover_count"`
}

// agentTakeoverConversationPageResponse 是客服接管会话分页响应 DTO。
type agentTakeoverConversationPageResponse struct {
	// Sessions 是当前页接管会话。
	Sessions []agentTakeoverSessionDTO `json:"sessions"`
	// HasMore 表示是否还有下一页。
	HasMore bool `json:"has_more"`
	// NextBeforeLastMessageAt 是下一页的最后消息毫秒时间；没有下一页时为零。
	NextBeforeLastMessageAt int64 `json:"next_before_last_message_at"`
	// NextBeforeChatID 是下一页稳定排序使用的会话标识；没有下一页时为空。
	NextBeforeChatID string `json:"next_before_chat_id"`
}

// mountVersionedAgentConversationRoutes 把客服接管会话接口挂到已登录的客服 Agent 路由组。
func (s *Server) mountVersionedAgentConversationRoutes(r chi.Router) {
	r.Get("/api/v1/agent/support/conversations", s.agentTakeoverConversations)
	r.Get("/api/v1/agent/support/conversations/messages", s.agentTakeoverConversationMessages)
}

// agentTakeoverConversations 返回当前用户被 AI 回复过的会话分页。
func (s *Server) agentTakeoverConversations(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// limit 是本次请求的页大小。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// cursor、cursorErr 是解析后的键集游标及格式错误。
	cursor, cursorErr := parseAgentTakeoverCursor(r)
	if cursorErr != nil {
		writeErr(w, http.StatusBadRequest, "会话游标格式无效")
		return
	}
	// page、err 是应用层接管会话分页及查询错误。
	page, err := s.agentConversationApplication().ListTakeoverSessions(r.Context(), sess.UserID, cursor, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取客服接管会话失败")
		return
	}
	// response 是待填充的响应 DTO。
	response := agentTakeoverConversationPageResponse{
		Sessions: make([]agentTakeoverSessionDTO, 0, len(page.Sessions)), HasMore: page.HasMore,
	}
	// session 是当前待转换的接管会话。
	for _, session := range page.Sessions {
		response.Sessions = append(response.Sessions, agentTakeoverSessionDTO{
			chatSessionDTO: newChatSessionDTOFromApplication(session.Session), TakeoverCount: session.TakeoverCount,
		})
	}
	if page.NextCursor != nil {
		response.NextBeforeLastMessageAt = page.NextCursor.LastMessageAt
		response.NextBeforeChatID = page.NextCursor.ChatID
	}
	writeJSON(w, http.StatusOK, response)
}

// agentTakeoverConversationMessages 返回指定账号与会话的聊天消息页。
func (s *Server) agentTakeoverConversationMessages(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// accountID、chatID 是必需的会话定位参数。
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	// chatID 是必需的会话标识，与账号共同定位聊天历史。
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if accountID == "" || chatID == "" {
		writeErr(w, http.StatusBadRequest, "缺少账号或会话标识")
		return
	}
	// beforeID 是更早消息游标；非法或缺失时按最新一页读取。
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	// limit 是本次请求的条数上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// page、err 是聊天历史应用层消息页及查询错误。
	page, err := s.chatApplication().ListStoredMessages(r.Context(), sess.UserID, accountID, chatID, beforeID, limit)
	if err != nil {
		if errors.Is(err, chatapp.ErrInvalidInput) {
			writeErr(w, http.StatusBadRequest, "聊天历史查询参数无效")
			return
		}
		writeErr(w, http.StatusInternalServerError, "读取客服会话消息失败")
		return
	}
	writeJSON(w, http.StatusOK, chatMessagePageResponse{
		Messages: newChatMessageDTOsFromApplication(page.Messages), HasMore: page.HasMore,
		Session: newChatSessionDTOFromApplication(page.Session),
	})
}

// parseAgentTakeoverCursor 解析客服接管会话的键集游标；两个参数都为空时返回首页。
func parseAgentTakeoverCursor(r *http.Request) (*chatapp.SessionCursor, error) {
	// rawAt、rawChat 是游标的两个查询参数原文。
	rawAt := strings.TrimSpace(r.URL.Query().Get("before_last_message_at"))
	// rawChat 是游标中的会话标识，必须与时间同时提供。
	rawChat := strings.TrimSpace(r.URL.Query().Get("before_chat_id"))
	if rawAt == "" && rawChat == "" {
		return nil, nil
	}
	// lastMessageAt 是游标要求的最后消息毫秒时间。
	lastMessageAt, parseErr := strconv.ParseInt(rawAt, 10, 64)
	if parseErr != nil || rawChat == "" {
		return nil, errors.New("客服接管会话游标格式无效")
	}
	return &chatapp.SessionCursor{LastMessageAt: lastMessageAt, ChatID: rawChat}, nil
}

// 编译期断言：聊天用例必须覆盖客服接管会话只读端口，避免路由注册到残缺实现。
var _ AgentConversationPort = (*chatapp.Service)(nil)
