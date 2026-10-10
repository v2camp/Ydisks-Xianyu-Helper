// tools_chat.go 注册聊天域工具：会话分页、联系人/历史刷新、库内消息分页、发送文本与图片、
// 标记已读、删除会话、快捷回复增删查、买家备注读写与聊天商品卡片查询。
//
// 安全与语义红线：发送与删除复用 chat 应用服务，平台回显、结果不确定（uncertain）与
// 代次隔离语义不在 MCP 层重建；删除会话与发送消息必须有显式 confirm；
// 图片只接受公网 URL 或受大小上限约束的 base64，公网 URL 下载复用组合层受控出站策略。

package mcp

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	chatapp "xianyu-go/internal/application/chat"

	"xianyu-go/internal/capability"
)

const (
	// chatImageMaxBytes 是聊天单张图片的业务上限，与 HTTP 上传路径保持一致（10 MiB）。
	chatImageMaxBytes = 10 << 20
	// chatFilePrefix 是 base64 图片落库时使用的稳定文件名前缀。
	chatFilePrefix = "mcp-chat-image"
)

// chatSessionDTO 是聊天会话的非敏感视图。
type chatSessionDTO struct {
	// AccountID 是会话所属账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是平台聊天会话标识。
	ChatID string `json:"chat_id"`
	// PeerUserID 是会话对端平台标识。
	PeerUserID string `json:"peer_user_id,omitempty"`
	// PeerName 是对端展示名称。
	PeerName string `json:"peer_name,omitempty"`
	// PeerAvatar 是对端头像地址。
	PeerAvatar string `json:"peer_avatar,omitempty"`
	// AccountRole 是当前账号在该会话中的角色。
	AccountRole string `json:"account_role,omitempty"`
	// BuyerUserID 是已确认的买家平台标识。
	BuyerUserID string `json:"buyer_user_id,omitempty"`
	// SellerUserID 是已确认的卖家平台标识。
	SellerUserID string `json:"seller_user_id,omitempty"`
	// ItemID 是会话关联商品标识。
	ItemID string `json:"item_id,omitempty"`
	// ItemTitle 是会话关联商品标题。
	ItemTitle string `json:"item_title,omitempty"`
	// ItemImageURL 是会话关联商品主图地址。
	ItemImageURL string `json:"item_image_url,omitempty"`
	// LastMessage 是最近消息摘要。
	LastMessage string `json:"last_message,omitempty"`
	// LastMessageAt 是最近消息时间的 Unix 毫秒时间戳。
	LastMessageAt int64 `json:"last_message_at"`
	// UnreadCount 是会话未读消息数量。
	UnreadCount int `json:"unread_count"`
}

// chatSessionCursorDTO 是会话键集分页游标视图，供调用方原样回传。
type chatSessionCursorDTO struct {
	// LastMessageAt 是上一页最后一条会话的最后消息毫秒时间戳。
	LastMessageAt int64 `json:"last_message_at"`
	// ChatID 是同一时间戳下保证排序稳定的会话标识。
	ChatID string `json:"chat_id"`
}

// chatSessionPageDTO 是会话分页返回。
type chatSessionPageDTO struct {
	// Total 是当前页会话数量。
	Total int `json:"total"`
	// HasMore 表示本地缓存中是否还有下一页。
	HasMore bool `json:"has_more"`
	// NextCursor 是下一页游标；没有下一页时为空。
	NextCursor *chatSessionCursorDTO `json:"next_cursor,omitempty"`
	// Sessions 是当前页会话视图列表。
	Sessions []chatSessionDTO `json:"sessions"`
}

// chatMessageDTO 是聊天消息的非敏感视图。
type chatMessageDTO struct {
	// MessageID 是本地消息标识。
	MessageID int64 `json:"message_id"`
	// AccountID 是消息所属账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是会话标识。
	ChatID string `json:"chat_id"`
	// Direction 是消息方向：incoming 或 outgoing。
	Direction string `json:"direction"`
	// SenderID 是平台发送者标识。
	SenderID string `json:"sender_id,omitempty"`
	// SenderName 是发送者展示名称。
	SenderName string `json:"sender_name,omitempty"`
	// MessageType 是消息内容类型，例如 text 或 image。
	MessageType string `json:"message_type"`
	// Content 是文本内容或媒体地址，不含平台凭证。
	Content string `json:"content,omitempty"`
	// MediaDurationSeconds 是语音消息时长秒数；非语音为零。
	MediaDurationSeconds int64 `json:"media_duration_seconds,omitempty"`
	// Status 是投递状态：sending/sent/failed/uncertain。
	Status string `json:"status,omitempty"`
	// ReadStatus 是平台确认的读取状态；2 表示对方已读。
	ReadStatus int `json:"read_status,omitempty"`
	// ReadAt 是平台确认对方已读的 Unix 毫秒时间戳。
	ReadAt int64 `json:"read_at,omitempty"`
	// SentAt 是消息时间的 Unix 毫秒时间戳。
	SentAt int64 `json:"sent_at"`
}

// chatMessagePageDTO 是库内消息分页返回。
type chatMessagePageDTO struct {
	// Total 是当前页消息数量。
	Total int `json:"total"`
	// HasMore 表示是否可能还有更早消息。
	HasMore bool `json:"has_more"`
	// Session 是当前会话摘要；找不到时为空。
	Session *chatSessionDTO `json:"session,omitempty"`
	// Messages 是按时间正序排列的消息列表。
	Messages []chatMessageDTO `json:"messages"`
}

// chatSendResult 是发送类操作的返回。
type chatSendResult struct {
	// Message 是本地消息视图，status 反映平台确认结果。
	Message chatMessageDTO `json:"message"`
	// Note 是给 Harness 的下一步提示；结果不确定时要求人工核对。
	Note string `json:"note,omitempty"`
}

// chatRefreshResult 是平台刷新类操作的返回。
type chatRefreshResult struct {
	// HasMore 表示平台是否还存在更早的分页。
	HasMore bool `json:"has_more"`
	// NextCursor 是下一次刷新使用的平台游标。
	NextCursor int64 `json:"next_cursor"`
	// Refreshed 是本次落库的消息或会话数量。
	Refreshed int `json:"refreshed"`
	// Session 是刷新返回的会话摘要；联系人刷新时为空。
	Session *chatSessionDTO `json:"session,omitempty"`
	// Messages 是历史刷新落库的消息列表；联系人刷新时为空。
	Messages []chatMessageDTO `json:"messages,omitempty"`
}

// chatOperationResult 是标记已读、删除会话等操作的确认返回。
type chatOperationResult struct {
	// AccountID 是操作涉及的账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是操作涉及的会话标识。
	ChatID string `json:"chat_id"`
	// Applied 固定为 true，表示操作已在本地生效。
	Applied bool `json:"applied"`
}

// chatItemDTO 是聊天商品卡片的非敏感视图。
type chatItemDTO struct {
	// ItemID 是商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// ImageURL 是商品主图地址。
	ImageURL string `json:"image_url,omitempty"`
	// Price 是平台价格文本。
	Price string `json:"price,omitempty"`
	// Description 是商品摘要。
	Description string `json:"description,omitempty"`
}

// chatItemPageDTO 是聊天商品分页返回。
type chatItemPageDTO struct {
	// Page 是当前页码，从 1 开始。
	Page int `json:"page"`
	// HasMore 表示是否还有下一页。
	HasMore bool `json:"has_more"`
	// Items 是当前页商品列表。
	Items []chatItemDTO `json:"items"`
}

// clampLimit 归一工具页大小入参：非正数回落默认值，超过上限按上限收敛。
func clampLimit(value, fallback, maxValue int) int {
	if value <= 0 {
		value = fallback
	}
	if value > maxValue {
		value = maxValue
	}
	return value
}

// RegisterChatTools 注册聊天域全部工具。
func (e *Endpoint) RegisterChatTools(p capability.ChatPorts) {
	if p == nil {
		return
	}
	e.registerChatViewTools(p)
	e.registerChatSendTools(p)
	e.registerChatMetadataTools(p)
}

// registerChatViewTools 注册会话、消息与聊天商品查询工具。
func (e *Endpoint) registerChatViewTools(p capability.ChatPorts) {
	// accountArg 是聊天查询通用的账号入参定义。
	accountArg := ArgSpec{Name: "account_id", Type: ArgString, Required: true, Description: "闲鱼账号标识。"}
	e.RegisterTools(
		ToolDef{
			Name: "chat_session_list",
			Description: "分页查询本地缓存的聊天会话（键集分页，稳定不重不漏）。" +
				"下一页请原样回传 next_cursor 中的 last_message_at 与 chat_id。只读。",
			Args: []ArgSpec{
				accountArg,
				{Name: "cursor_last_message_at", Type: ArgInteger, Description: "上一页 next_cursor.last_message_at；首页不传。"},
				{Name: "cursor_chat_id", Type: ArgString, Description: "上一页 next_cursor.chat_id；首页不传。"},
				{Name: "limit", Type: ArgInteger, Description: "每页会话数，默认 20，最大 200。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// limit 是归一后的会话分页大小。
				limit := clampLimit(args.OptionalInt("limit", 20), 20, maxPageSize)
				// cursor 是可选键集游标；只传 chat_id 时视为非法游标。
				cursor := chatCursorFromArgs(args)
				// page、pageErr 是会话分页结果。
				page, pageErr := p.ListSessionPage(ctx, identity.UserID, accountID, cursor, limit)
				if pageErr != nil {
					return nil, pageErr
				}
				return chatSessionPageFromApp(page), nil
			},
		},
		ToolDef{
			Name: "chat_message_list",
			Description: "分页查询本地已落库的聊天消息（按时间正序）。" +
				"before_id 传当前页最早一条的 message_id 可继续向前翻页。只读。",
			Args: []ArgSpec{
				accountArg,
				{Name: "chat_id", Type: ArgString, Required: true, Description: "平台聊天会话标识。"},
				{Name: "before_id", Type: ArgInteger, Description: "只看该消息标识之前的更早消息；首页不传。"},
				{Name: "limit", Type: ArgInteger, Description: "每页消息数，默认 50，最大 500。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、chatID 是账号与会话标识。
				accountID, chatID, idErr := chatIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// page、pageErr 是库内消息分页结果。
				page, pageErr := p.ListStoredMessages(ctx, identity.UserID, accountID, chatID,
					int64(args.OptionalInt("before_id", 0)), clampLimit(args.OptionalInt("limit", 50), 50, 500))
				if pageErr != nil {
					return nil, pageErr
				}
				// dto 是消息分页视图。
				dto := chatMessagePageDTO{
					Total: len(page.Messages), HasMore: page.HasMore,
					Messages: chatMessageDTOsFromApp(page.Messages),
				}
				if page.Session.ChatID != "" {
					// session 是当前会话摘要视图。
					session := chatSessionDTOFromApp(page.Session)
					dto.Session = &session
				}
				return dto, nil
			},
		},
		ToolDef{
			Name:        "chat_session_refresh_contacts",
			Description: "向闲鱼拉取并落库账号的最新联系人页（真实平台触达，账号需在线）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountArg,
				{Name: "cursor", Type: ArgInteger, Description: "平台游标；首页不传或传 0。"},
				{Name: "limit", Type: ArgInteger, Description: "每页联系人数量，默认 50。"},
			},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// page、refreshErr 是联系人刷新结果。
				page, refreshErr := p.RefreshConversations(ctx, accountID,
					int64(args.OptionalInt("cursor", 0)), clampLimit(args.OptionalInt("limit", 50), 50, maxPageSize))
				if refreshErr != nil {
					return nil, refreshErr
				}
				return chatRefreshResult{HasMore: page.HasMore, NextCursor: page.NextCursor}, nil
			},
		},
		ToolDef{
			Name: "chat_history_refresh",
			Description: "向闲鱼拉取并落库指定会话的最新消息页（真实平台触达，账号需在线）。必须显式 confirm=true。" +
				"幂等：重复刷新同一页不会产生重复消息。",
			Destructive: true,
			Args: []ArgSpec{
				accountArg,
				{Name: "chat_id", Type: ArgString, Required: true, Description: "平台聊天会话标识。"},
				{Name: "cursor", Type: ArgInteger, Description: "平台游标；首页不传或传 0。"},
				{Name: "limit", Type: ArgInteger, Description: "每页消息数量，默认 50。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、chatID 是账号与会话标识。
				accountID, chatID, idErr := chatIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// session、lookupErr 是归属会话；刷新接口要求提交已知的对端身份。
				session, lookupErr := p.FindSession(ctx, identity.UserID, accountID, chatID)
				if lookupErr != nil {
					return nil, lookupErr
				}
				// page、refreshErr 是历史刷新结果。
				page, refreshErr := p.RefreshHistory(ctx, accountID, chatID,
					int64(args.OptionalInt("cursor", 0)), clampLimit(args.OptionalInt("limit", 50), 50, maxPageSize), session)
				if refreshErr != nil {
					return nil, refreshErr
				}
				return chatRefreshResult{
					HasMore: page.HasMore, NextCursor: page.NextCursor,
					Refreshed: len(page.Messages), Messages: chatMessageDTOsFromApp(page.Messages),
					Session: chatSessionPointer(page.Session),
				}, nil
			},
		},
		ToolDef{
			Name:        "chat_item_list",
			Description: "查询当前会话可发送的商品卡片：role=peer 查对方商品，role=self 查自己的商品。只读。",
			Args: []ArgSpec{
				accountArg,
				{Name: "chat_id", Type: ArgString, Required: true, Description: "平台聊天会话标识。"},
				{Name: "role", Type: ArgString, Required: true, Enum: []string{"peer", "self"},
					Description: "商品归属：peer=对方，self=当前账号。"},
				{Name: "query", Type: ArgString, Description: "商品搜索词，最多 100 字。"},
				{Name: "page", Type: ArgInteger, Description: "页码，从 1 开始，默认 1。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、chatID 是账号与会话标识。
				accountID, chatID, idErr := chatIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// role 是商品归属方向。
				role, roleErr := args.String("role")
				if roleErr != nil {
					return nil, roleErr
				}
				// page、pageErr 是聊天商品分页结果。
				page, pageErr := p.ListChatItems(ctx, chatapp.ChatItemQuery{
					UserID: identity.UserID, AccountID: accountID, ChatID: chatID, Role: role,
					Query: args.OptionalString("query", ""), Page: args.OptionalInt("page", 1),
				})
				if pageErr != nil {
					return nil, pageErr
				}
				// items 是商品视图列表。
				items := make([]chatItemDTO, 0, len(page.Items))
				// item 是当前待映射的商品。
				for _, item := range page.Items {
					items = append(items, chatItemDTO{
						ItemID: item.ItemID, Title: item.Title, ImageURL: item.ImageURL,
						Price: item.Price, Description: item.Description,
					})
				}
				return chatItemPageDTO{Page: page.Page, HasMore: page.HasMore, Items: items}, nil
			},
		},
	)
}

// registerChatSendTools 注册发送、标记已读与删除会话工具。
func (e *Endpoint) registerChatSendTools(p capability.ChatPorts) {
	e.RegisterTools(
		ToolDef{
			Name: "chat_send_text",
			Description: "向指定会话发送一条文本消息（真实平台触达）。发送前会校验会话归属与账号在线状态；" +
				"返回的 status 为 uncertain 时表示平台回执不明，必须先人工核对再决定是否重发。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "执行发送的账号标识。"},
				{Name: "chat_id", Type: ArgString, Required: true, Description: "目标会话标识。"},
				{Name: "text", Type: ArgString, Required: true, Description: "文本内容，最多 2000 字。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// session、text、prepareErr 是归属会话与待发送文本。
				session, text, prepareErr := chatSendInput(ctx, p, identity.UserID, args)
				if prepareErr != nil {
					return nil, prepareErr
				}
				// sent、sendErr 是发送结果与错误。
				sent, sendErr := p.SendText(ctx, chatapp.OutgoingInput{Session: session, Text: text})
				return chatSendOutcome(sent, sendErr)
			},
		},
		ToolDef{
			Name: "chat_send_image",
			Description: "向指定会话发送一张图片（真实平台触达）：image_url 只接受公网 http/https 地址，" +
				"image_base64 解码后不得超过 10 MiB，两者必须二选一。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "执行发送的账号标识。"},
				{Name: "chat_id", Type: ArgString, Required: true, Description: "目标会话标识。"},
				{Name: "image_url", Type: ArgString, Description: "公网可访问的图片地址；与 image_base64 二选一。"},
				{Name: "image_base64", Type: ArgString, Description: "标准 base64 图片内容，解码后不超过 10 MiB；与 image_url 二选一。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// session 是归属会话。
				session, _, prepareErr := chatSendInput(ctx, p, identity.UserID, args)
				if prepareErr != nil {
					return nil, prepareErr
				}
				// data、contentType、imageErr 是受控取得的图片字节与媒体类型。
				data, contentType, imageErr := chatImageInput(ctx, p, args)
				if imageErr != nil {
					return nil, imageErr
				}
				// sent、sendErr 是图片发送结果与错误。
				sent, sendErr := p.SendImage(ctx, chatapp.ImageInput{
					Session: session, Filename: chatImageFilename(contentType), ContentType: contentType, Data: data,
				})
				return chatSendOutcome(sent, sendErr)
			},
		},
		ToolDef{
			Name:        "chat_mark_read",
			Description: "把指定会话标记为已读（本地立即生效，平台已读上报为尽力而为）。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "chat_id", Type: ArgString, Required: true, Description: "会话标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、chatID 是账号与会话标识。
				accountID, chatID, idErr := chatIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// markErr 是标记已读用例返回的错误。
				if markErr := p.MarkRead(ctx, identity.UserID, accountID, chatID); markErr != nil {
					return nil, markErr
				}
				return chatOperationResult{AccountID: accountID, ChatID: chatID, Applied: true}, nil
			},
		},
		ToolDef{
			Name: "chat_session_delete",
			Description: "删除会话：隐藏会话并物理清空其展示消息（不可逆，残留 Automation 定位数据仍会保留）。" +
				"必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "chat_id", Type: ArgString, Required: true, Description: "要删除的会话标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、chatID 是账号与会话标识。
				accountID, chatID, idErr := chatIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// deleteErr 是删除会话用例返回的错误。
				if deleteErr := p.DeleteConversation(ctx, identity.UserID, accountID, chatID); deleteErr != nil {
					return nil, deleteErr
				}
				return chatOperationResult{AccountID: accountID, ChatID: chatID, Applied: true}, nil
			},
		},
	)
}

// chatCursorFromArgs 读取会话键集分页游标；只提交一半游标时返回空游标，由服务端按首页处理。
func chatCursorFromArgs(args Arguments) *chatapp.SessionCursor {
	// chatID 是游标中的会话标识。
	chatID := strings.TrimSpace(args.OptionalString("cursor_chat_id", ""))
	// lastMessageAt 是游标中的毫秒时间戳。
	lastMessageAt := int64(args.OptionalInt("cursor_last_message_at", 0))
	if chatID == "" || lastMessageAt <= 0 {
		return nil
	}
	return &chatapp.SessionCursor{LastMessageAt: lastMessageAt, ChatID: chatID}
}

// chatIDs 读取聊天查询与操作共用的账号与会话标识。
func chatIDs(args Arguments) (accountID, chatID string, err error) {
	// accountID 是账号标识。
	accountID, err = args.String("account_id")
	if err != nil {
		return "", "", err
	}
	// chatID 是会话标识。
	chatID, err = args.String("chat_id")
	if err != nil {
		return "", "", err
	}
	return accountID, chatID, nil
}

// chatSendInput 解析发送类工具的归属会话与文本内容；发送前必须确认会话存在且已知对端。
func chatSendInput(ctx context.Context, p capability.ChatPorts, userID int64, args Arguments) (chatapp.Session, string, error) {
	// accountID、chatID 是账号与会话标识。
	accountID, chatID, idErr := chatIDs(args)
	if idErr != nil {
		return chatapp.Session{}, "", idErr
	}
	// session、lookupErr 是归属会话。
	session, lookupErr := p.FindSession(ctx, userID, accountID, chatID)
	if lookupErr != nil {
		return chatapp.Session{}, "", lookupErr
	}
	if strings.TrimSpace(session.ChatID) == "" || strings.TrimSpace(session.PeerUserID) == "" {
		return chatapp.Session{}, "", Fail(ClassNotFound, "会话不存在或尚未同步对端身份，请先调用 chat_session_refresh_contacts", nil)
	}
	// text 是文本入参；图片发送场景允许为空。
	text := args.OptionalString("text", "")
	if strings.TrimSpace(text) == "" && args.OptionalString("image_url", "") == "" && args.OptionalString("image_base64", "") == "" {
		return chatapp.Session{}, "", InvalidArgument("缺少消息内容：请提供 text、image_url 或 image_base64")
	}
	return session, text, nil
}

// chatImageInput 取得待发送图片的字节与媒体类型：公网 URL 走组合层受控下载，base64 受大小上限约束。
func chatImageInput(ctx context.Context, p capability.ChatPorts, args Arguments) ([]byte, string, error) {
	// rawURL 是公网图片地址。
	rawURL := strings.TrimSpace(args.OptionalString("image_url", ""))
	// encoded 是 base64 图片内容。
	encoded := strings.TrimSpace(args.OptionalString("image_base64", ""))
	if rawURL != "" && encoded != "" {
		return nil, "", InvalidArgument("image_url 与 image_base64 只能二选一")
	}
	if rawURL != "" {
		// data、contentType、fetchErr 是受控下载得到的图片字节与媒体类型。
		data, contentType, fetchErr := p.FetchChatImage(ctx, rawURL)
		if fetchErr != nil {
			return nil, "", Fail(ClassInvalidArgument,
				"图片地址无法下载：仅支持公网可访问、大小不超过 10 MiB 的图片", fetchErr)
		}
		return data, contentType, nil
	}
	if encoded == "" {
		return nil, "", InvalidArgument("缺少图片内容：请提供 image_url 或 image_base64")
	}
	// data、decodeErr 是 base64 解码结果。
	data, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil {
		return nil, "", InvalidArgument("image_base64 不是合法的 base64 编码")
	}
	if len(data) == 0 || len(data) > chatImageMaxBytes {
		return nil, "", InvalidArgument("image_base64 解码后必须非空且不超过 10 MiB")
	}
	// contentType 是按字节探测的媒体类型，拒绝伪装成图片的其它文件。
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", InvalidArgument("image_base64 内容不是有效图片")
	}
	return data, contentType, nil
}

// chatImageFilename 按媒体类型生成稳定的上传文件名，避免把调用方字符串带进平台请求。
func chatImageFilename(contentType string) string {
	// suffix 是媒体类型对应的文件扩展名。
	suffix := ".png"
	switch contentType {
	case "image/jpeg":
		suffix = ".jpg"
	case "image/gif":
		suffix = ".gif"
	case "image/webp":
		suffix = ".webp"
	}
	return chatFilePrefix + suffix
}

// chatSendOutcome 把发送结果归一为 MCP 结果；错误类别由统一错误映射负责，
// 结果不确定时额外提示人工核对，禁止自动重试。
func chatSendOutcome(sent *chatapp.Message, sendErr error) (any, error) {
	if sendErr != nil {
		return nil, sendErr
	}
	// result 是发送成功的返回。
	result := chatSendResult{Message: chatMessageDTOFromAppPointer(sent)}
	if sent != nil && sent.Status == "uncertain" {
		result.Note = "平台回执不明确，请人工核对后再决定是否重发。"
	}
	return result, nil
}

// chatSessionPointer 把会话摘要映射为可空视图；空会话返回 nil。
func chatSessionPointer(session chatapp.Session) *chatSessionDTO {
	if strings.TrimSpace(session.ChatID) == "" {
		return nil
	}
	// dto 是会话视图。
	dto := chatSessionDTOFromApp(session)
	return &dto
}

// chatSessionPageFromApp 把会话分页结果映射为视图。
func chatSessionPageFromApp(page chatapp.SessionPage) chatSessionPageDTO {
	// sessions 是会话视图列表。
	sessions := make([]chatSessionDTO, 0, len(page.Sessions))
	// session 是当前待映射的会话摘要。
	for _, session := range page.Sessions {
		sessions = append(sessions, chatSessionDTOFromApp(session))
	}
	// dto 是分页视图。
	dto := chatSessionPageDTO{Total: len(sessions), HasMore: page.HasMore, Sessions: sessions}
	if page.NextCursor != nil && strings.TrimSpace(page.NextCursor.ChatID) != "" {
		dto.NextCursor = &chatSessionCursorDTO{
			LastMessageAt: page.NextCursor.LastMessageAt, ChatID: page.NextCursor.ChatID,
		}
	}
	return dto
}

// chatSessionDTOFromApp 把会话应用模型映射为非敏感视图。
func chatSessionDTOFromApp(session chatapp.Session) chatSessionDTO {
	return chatSessionDTO{
		AccountID: session.AccountID, ChatID: session.ChatID, PeerUserID: session.PeerUserID,
		PeerName: session.PeerName, PeerAvatar: session.PeerAvatar, AccountRole: session.AccountRole,
		BuyerUserID: session.BuyerUserID, SellerUserID: session.SellerUserID,
		ItemID: session.ItemID, ItemTitle: session.ItemTitle, ItemImageURL: session.ItemImageURL,
		LastMessage: session.LastMessage, LastMessageAt: session.LastMessageAt, UnreadCount: session.UnreadCount,
	}
}

// chatMessageDTOsFromApp 批量映射聊天消息为非敏感视图。
func chatMessageDTOsFromApp(messages []chatapp.Message) []chatMessageDTO {
	// dtos 是消息视图列表。
	dtos := make([]chatMessageDTO, 0, len(messages))
	// message 是当前待映射的消息。
	for _, message := range messages {
		dtos = append(dtos, chatMessageDTOFromApp(message))
	}
	return dtos
}

// chatMessageDTOFromApp 把聊天消息应用模型映射为非敏感视图。
func chatMessageDTOFromApp(message chatapp.Message) chatMessageDTO {
	return chatMessageDTO{
		MessageID: message.ID, AccountID: message.AccountID, ChatID: message.ChatID,
		Direction: message.Direction, SenderID: message.SenderID, SenderName: message.SenderName,
		MessageType: message.MessageType, Content: message.Content,
		MediaDurationSeconds: message.MediaDuration, Status: message.Status,
		ReadStatus: message.ReadStatus, ReadAt: message.ReadAt, SentAt: message.SentAt,
	}
}

// chatMessageDTOFromAppPointer 映射可空消息；空消息返回零值视图。
func chatMessageDTOFromAppPointer(message *chatapp.Message) chatMessageDTO {
	if message == nil {
		return chatMessageDTO{}
	}
	return chatMessageDTOFromApp(*message)
}
