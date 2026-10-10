// tools_chat_metadata.go 注册聊天元数据工具：人工快捷回复增删查与买家备注读写。
//
// 安全与语义红线：备注与快捷回复均为运营文本，直接回显不含凭证；
// 账号归属与长度校验由 chat 应用服务负责，MCP 层不复制业务规则。

package mcp

import (
	"context"

	"xianyu-go/internal/capability"
)

// quickReplyDTO 是人工快捷回复的非敏感视图。
type quickReplyDTO struct {
	// QuickReplyID 是快捷回复标识。
	QuickReplyID int64 `json:"quick_reply_id"`
	// AccountID 是所属账号标识。
	AccountID string `json:"account_id"`
	// Content 是文本模板。
	Content string `json:"content"`
	// CreatedAt 是创建时间的 Unix 秒时间戳。
	CreatedAt int64 `json:"created_at,omitempty"`
}

// quickReplyListResult 是快捷回复列表返回。
type quickReplyListResult struct {
	// Total 是快捷回复数量。
	Total int `json:"total"`
	// QuickReplies 是快捷回复视图列表。
	QuickReplies []quickReplyDTO `json:"quick_replies"`
}

// buyerNoteDTO 是买家备注的非敏感视图。
type buyerNoteDTO struct {
	// AccountID 是备注所属账号标识。
	AccountID string `json:"account_id"`
	// BuyerID 是买家平台标识。
	BuyerID string `json:"buyer_id"`
	// Content 是完整备注正文；空值表示尚未填写。
	Content string `json:"content"`
	// UpdatedAt 是最近保存的 Unix 秒时间戳；空备注为零。
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

// registerChatMetadataTools 注册快捷回复与买家备注工具。
func (e *Endpoint) registerChatMetadataTools(p capability.ChatPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "chat_quick_reply_list",
			Description: "列出指定账号的人工快捷回复（按创建时间倒序）。只读。",
			Args:        []ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// rows、listErr 是快捷回复列表。
				rows, listErr := p.ListQuickReplies(ctx, identity.UserID, accountID)
				if listErr != nil {
					return nil, listErr
				}
				// replies 是快捷回复视图列表。
				replies := make([]quickReplyDTO, 0, len(rows))
				// row 是当前待映射的快捷回复。
				for _, row := range rows {
					replies = append(replies, quickReplyDTO{
						QuickReplyID: row.ID, AccountID: row.AccountID, Content: row.Content, CreatedAt: row.CreatedAt,
					})
				}
				return quickReplyListResult{Total: len(replies), QuickReplies: replies}, nil
			},
		},
		ToolDef{
			Name:        "chat_quick_reply_add",
			Description: "为指定账号新增一条人工快捷回复；达到账号上限时会被拒绝。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "content", Type: ArgString, Required: true, Description: "文本模板，最多 2000 字。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// content 是文本模板。
				content, contentErr := args.String("content")
				if contentErr != nil {
					return nil, contentErr
				}
				// reply、createErr 是新建的快捷回复。
				reply, createErr := p.CreateQuickReply(ctx, identity.UserID, accountID, content)
				if createErr != nil {
					return nil, createErr
				}
				return quickReplyDTO{
					QuickReplyID: reply.ID, AccountID: reply.AccountID, Content: reply.Content, CreatedAt: reply.CreatedAt,
				}, nil
			},
		},
		ToolDef{
			Name:        "chat_quick_reply_delete",
			Description: "删除指定账号下的一条人工快捷回复（不可逆）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "quick_reply_id", Type: ArgInteger, Required: true, Description: "快捷回复标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// id 是快捷回复标识。
				id, err := args.Int("quick_reply_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是删除快捷回复用例返回的错误。
				if deleteErr := p.DeleteQuickReply(ctx, identity.UserID, accountID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return quickReplyDTO{QuickReplyID: int64(id), AccountID: accountID}, nil
			},
		},
		ToolDef{
			Name:        "chat_buyer_note_get",
			Description: "读取指定账号下买家的聊天备注；尚未填写时返回空正文。只读。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "buyer_id", Type: ArgString, Required: true, Description: "买家平台标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、buyerID 是账号与买家标识。
				accountID, buyerID, idErr := chatBuyerIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// note、getErr 是买家备注。
				note, getErr := p.GetBuyerNote(ctx, identity.UserID, accountID, buyerID)
				if getErr != nil {
					return nil, getErr
				}
				return buyerNoteDTO{AccountID: accountID, BuyerID: buyerID, Content: note.Content, UpdatedAt: note.UpdatedAt}, nil
			},
		},
		ToolDef{
			Name:        "chat_buyer_note_set",
			Description: "保存指定账号下买家的聊天备注；传空正文会删除已有备注（仅影响备注，不影响会话）。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "buyer_id", Type: ArgString, Required: true, Description: "买家平台标识。"},
				{Name: "content", Type: ArgString, Description: "备注正文，最多 2000 字；留空表示删除备注。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、buyerID 是账号与买家标识。
				accountID, buyerID, idErr := chatBuyerIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// note、saveErr 是保存后的买家备注。
				note, saveErr := p.SaveBuyerNote(ctx, identity.UserID, accountID, buyerID, args.OptionalString("content", ""))
				if saveErr != nil {
					return nil, saveErr
				}
				return buyerNoteDTO{AccountID: accountID, BuyerID: buyerID, Content: note.Content, UpdatedAt: note.UpdatedAt}, nil
			},
		},
	)
}

// chatBuyerIDs 读取买家备注用例的账号与买家标识。
func chatBuyerIDs(args Arguments) (accountID, buyerID string, err error) {
	// accountID 是账号标识。
	accountID, err = args.String("account_id")
	if err != nil {
		return "", "", err
	}
	// buyerID 是买家平台标识。
	buyerID, err = args.String("buyer_id")
	if err != nil {
		return "", "", err
	}
	return accountID, buyerID, nil
}
