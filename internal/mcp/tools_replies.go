// tools_replies.go 注册回复配置域工具：默认回复、关键词回复与指定商品回复。
//
// 安全与语义红线：回复内容与图片地址属于运营配置，直接回显不含任何凭证；
// 删除、批量替换与投递记录清理均有不可逆影响，必须显式 confirm=true；
// 校验完全复用对应应用服务（含账号归属校验），MCP 层不复制业务规则。

package mcp

import (
	"context"

	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	keywordsapp "xianyu-go/internal/application/keywords"

	"xianyu-go/internal/capability"
)

// defaultReplyDTO 是默认回复配置的非敏感视图。
type defaultReplyDTO struct {
	// AccountID 是配置所属账号标识。
	AccountID string `json:"account_id"`
	// Enabled 表示是否启用默认回复。
	Enabled bool `json:"enabled"`
	// ReplyContent 是默认回复文字内容。
	ReplyContent string `json:"reply_content,omitempty"`
	// ReplyImageURL 是默认回复图片地址。
	ReplyImageURL string `json:"reply_image_url,omitempty"`
	// ReplyOnce 表示同一聊天是否只发送一次默认回复。
	ReplyOnce bool `json:"reply_once"`
}

// defaultReplyListResult 是默认回复列表返回。
type defaultReplyListResult struct {
	// Total 是已配置默认回复的账号数量。
	Total int `json:"total"`
	// Replies 是默认回复视图列表。
	Replies []defaultReplyDTO `json:"replies"`
}

// keywordDTO 是关键词回复的非敏感视图。
type keywordDTO struct {
	// KeywordID 是关键词规则标识。
	KeywordID int64 `json:"keyword_id"`
	// AccountID 是规则所属账号标识。
	AccountID string `json:"account_id"`
	// Keyword 是触发匹配文本。
	Keyword string `json:"keyword"`
	// Reply 是文字回复内容；image 类型时为空。
	Reply string `json:"reply,omitempty"`
	// ItemID 是规则限定的商品标识；为空表示账号级规则。
	ItemID string `json:"item_id,omitempty"`
	// Type 是回复类型：text 或 image。
	Type string `json:"type"`
	// ImageURL 是 image 类型回复使用的图片地址。
	ImageURL string `json:"image_url,omitempty"`
}

// keywordListResult 是关键词回复列表返回。
type keywordListResult struct {
	// Total 是关键词规则数量。
	Total int `json:"total"`
	// Keywords 是关键词回复视图列表。
	Keywords []keywordDTO `json:"keywords"`
}

// itemReplyDTO 是指定商品回复的非敏感视图。
type itemReplyDTO struct {
	// ItemID 是指定商品标识。
	ItemID string `json:"item_id"`
	// AccountID 是回复所属账号标识。
	AccountID string `json:"account_id"`
	// ReplyContent 是商品命中后的回复正文。
	ReplyContent string `json:"reply_content"`
}

// itemReplyListResult 是指定商品回复列表返回。
type itemReplyListResult struct {
	// Total 是指定商品回复数量。
	Total int `json:"total"`
	// Replies 是指定商品回复视图列表。
	Replies []itemReplyDTO `json:"replies"`
}

// keywordMutationResult 是关键词写入操作的确认返回。
type keywordMutationResult struct {
	// KeywordID 是被写入的关键词规则标识。
	KeywordID int64 `json:"keyword_id"`
}

// keywordIndexMutationResult 是按列表序号删除关键词的确认返回。
type keywordIndexMutationResult struct {
	// Index 是已删除规则在列表中的零基序号。
	Index int `json:"index"`
}

// keywordReplaceResult 是关键词批量替换的确认返回。
type keywordReplaceResult struct {
	// Replaced 是替换后生效的规则数量。
	Replaced int `json:"replaced"`
	// Note 提示该账号原有未提交的规则已被删除。
	Note string `json:"note"`
}

// keywordDraftRequest 是与 MCP 入参对应的关键词草稿解码结构。
type keywordDraftRequest struct {
	// Keyword 是触发匹配文本。
	Keyword string `json:"keyword"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
	// ItemID 是可选商品标识。
	ItemID string `json:"item_id"`
	// Type 是回复类型：text 或 image；省略按 text 处理。
	Type string `json:"type"`
	// ImageURL 是 image 类型回复使用的图片地址。
	ImageURL string `json:"image_url"`
}

// keywordReplaceRequest 是批量替换入参解码结构。
type keywordReplaceRequest struct {
	// AccountID 是目标账号标识。
	AccountID string `json:"account_id"`
	// Keywords 是替换后的完整关键词规则列表。
	Keywords []keywordDraftRequest `json:"keywords"`
}

// RegisterDefaultReplyTools 注册默认回复域全部工具。
func (e *Endpoint) RegisterDefaultReplyTools(p capability.DefaultReplyPorts) {
	if p == nil {
		return
	}
	e.RegisterTools(
		ToolDef{
			Name:        "default_reply_list",
			Description: "列出全部账号的默认回复配置（启用状态、文字、图片地址与单次发送开关）。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是默认回复配置列表。
				rows, listErr := p.ListDefaultReplies(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				return defaultReplyListResult{Total: len(rows), Replies: defaultReplyDTOsFromApp(rows)}, nil
			},
		},
		ToolDef{
			Name:        "default_reply_get",
			Description: "读取指定账号的默认回复配置。只读；账号尚未配置时返回未找到。",
			Args:        []ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "目标账号标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// reply、getErr 是默认回复配置。
				reply, getErr := p.GetDefaultReply(ctx, identity.UserID, accountID)
				if getErr != nil {
					return nil, getErr
				}
				return defaultReplyDTO{AccountID: accountID, Enabled: reply.Enabled, ReplyContent: reply.ReplyContent,
					ReplyImageURL: reply.ReplyImageURL, ReplyOnce: reply.ReplyOnce}, nil
			},
		},
		ToolDef{
			Name:        "default_reply_set",
			Description: "保存或覆盖指定账号的默认回复配置。文字与图片可同时配置；reply_once 为 true 时同一聊天只回复一次。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "目标账号标识。"},
				{Name: "enabled", Type: ArgBoolean, Required: true, Description: "是否启用默认回复。"},
				{Name: "reply_content", Type: ArgString, Description: "默认回复文字内容。"},
				{Name: "reply_image_url", Type: ArgString, Description: "默认回复图片公网地址。"},
				{Name: "reply_once", Type: ArgBoolean, Description: "同一聊天是否只发送一次，默认 false。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// enabled 是启用开关。
				enabled, err := args.Bool("enabled")
				if err != nil {
					return nil, err
				}
				// reply 是要保存的配置。
				reply := defaultreplyapp.Reply{
					Enabled: enabled, ReplyContent: args.OptionalString("reply_content", ""),
					ReplyImageURL: args.OptionalString("reply_image_url", ""),
					ReplyOnce:     args.OptionalBool("reply_once", false),
				}
				// upsertErr 是保存用例返回的错误。
				if upsertErr := p.UpsertDefaultReply(ctx, identity.UserID, accountID, reply); upsertErr != nil {
					return nil, upsertErr
				}
				return defaultReplyDTO{AccountID: accountID, Enabled: reply.Enabled, ReplyContent: reply.ReplyContent,
					ReplyImageURL: reply.ReplyImageURL, ReplyOnce: reply.ReplyOnce}, nil
			},
		},
		ToolDef{
			Name:        "default_reply_delete",
			Description: "删除指定账号的默认回复配置（不可逆）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "目标账号标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是删除用例返回的错误。
				if deleteErr := p.DeleteDefaultReply(ctx, identity.UserID, accountID); deleteErr != nil {
					return nil, deleteErr
				}
				return defaultReplyDTO{AccountID: accountID}, nil
			},
		},
		ToolDef{
			Name: "default_reply_clear_records",
			Description: "清空指定账号的默认回复投递记录，使已回复过的聊天可以重新触发默认回复（不影响配置本身，不可逆）。" +
				"必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "目标账号标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// clearErr 是投递记录清理用例返回的错误。
				if clearErr := p.ClearDefaultReplyRecords(ctx, identity.UserID, accountID); clearErr != nil {
					return nil, clearErr
				}
				return defaultReplyDTO{AccountID: accountID}, nil
			},
		},
	)
}

// RegisterKeywordTools 注册关键词回复与指定商品回复域全部工具。
func (e *Endpoint) RegisterKeywordTools(p capability.KeywordPorts) {
	if p == nil {
		return
	}
	e.registerKeywordTools(p)
	e.registerItemReplyTools(p)
}

// registerKeywordTools 注册关键词回复工具。
func (e *Endpoint) registerKeywordTools(p capability.KeywordPorts) {
	// draftArgs 是关键词草稿的公共入参定义。
	draftArgs := []ArgSpec{
		{Name: "keyword", Type: ArgString, Required: true, Description: "触发匹配文本。"},
		{Name: "reply", Type: ArgString, Description: "文字回复内容；type=text 时必填。"},
		{Name: "type", Type: ArgString, Enum: []string{"text", "image"}, Description: "回复类型，默认 text。"},
		{Name: "image_url", Type: ArgString, Description: "图片回复公网地址；type=image 时必填。"},
		{Name: "item_id", Type: ArgString, Description: "可选商品标识；省略表示账号级规则。"},
	}
	e.RegisterTools(
		ToolDef{
			Name:        "keyword_list",
			Description: "列出指定账号的全部关键词回复规则（含商品级规则）。只读。",
			Args:        []ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// rows、listErr 是关键词规则列表。
				rows, listErr := p.ListKeywords(ctx, identity.UserID, accountID)
				if listErr != nil {
					return nil, listErr
				}
				return keywordListResult{Total: len(rows), Keywords: keywordDTOsFromApp(accountID, rows)}, nil
			},
		},
		ToolDef{
			Name:        "keyword_add",
			Description: "新增一条关键词回复规则，命中关键词时自动回复。账号级与商品级规则可共存。",
			Args:        append([]ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"}}, draftArgs...),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// request 是关键词草稿入参。
				var request keywordDraftRequest
				// err 是入参解码错误。
				if err := decodeToolArguments(args, &request); err != nil {
					return nil, err
				}
				// id、addErr 是新关键词标识与错误。
				id, addErr := p.AddKeyword(ctx, identity.UserID, accountID, request.draft())
				if addErr != nil {
					return nil, addErr
				}
				return keywordMutationResult{KeywordID: id}, nil
			},
		},
		ToolDef{
			Name: "keyword_replace",
			Description: "原子替换指定账号的全部关键词回复：未提交的规则会被删除，影响范围大且不可逆。必须显式 confirm=true。" +
				"建议先 keyword_list 读取现状后再提交完整列表。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "keywords", Type: ArgArray, Required: true,
					Description: "替换后的完整关键词规则对象数组，每项含 keyword、reply、type、image_url、item_id。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// request 是批量替换入参。
				var request keywordReplaceRequest
				// err 是入参解码错误。
				if err := decodeToolArguments(args, &request); err != nil {
					return nil, err
				}
				// drafts 是转换后的规则草稿列表。
				drafts := make([]keywordsapp.Draft, 0, len(request.Keywords))
				// item 是当前待转换的规则入参。
				for _, item := range request.Keywords {
					drafts = append(drafts, item.draft())
				}
				// replaceErr 是批量替换用例返回的错误。
				if replaceErr := p.ReplaceKeywords(ctx, identity.UserID, request.AccountID, drafts); replaceErr != nil {
					return nil, replaceErr
				}
				return keywordReplaceResult{
					Replaced: len(drafts),
					Note:     "该账号原有未提交的关键词规则已被删除；如需保留请先列表核对后重新提交完整集合。",
				}, nil
			},
		},
		ToolDef{
			Name:        "keyword_update",
			Description: "更新指定关键词规则：关键字、回复内容与商品范围都会被整体覆盖。",
			Args: append([]ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "keyword_id", Type: ArgInteger, Required: true, Description: "关键词规则标识。"},
			}, draftArgs...),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// id 是关键词标识。
				id, err := args.Int("keyword_id")
				if err != nil {
					return nil, err
				}
				// request 是关键词草稿入参。
				var request keywordDraftRequest
				// err 是入参解码错误。
				if err := decodeToolArguments(args, &request); err != nil {
					return nil, err
				}
				// updateErr 是关键词更新用例返回的错误。
				if updateErr := p.UpdateKeyword(ctx, identity.UserID, accountID, int64(id), request.draft()); updateErr != nil {
					return nil, updateErr
				}
				return keywordMutationResult{KeywordID: int64(id)}, nil
			},
		},
		ToolDef{
			Name:        "keyword_delete",
			Description: "按标识删除关键词回复规则（不可逆）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "keyword_id", Type: ArgInteger, Required: true, Description: "关键词规则标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// id 是关键词标识。
				id, err := args.Int("keyword_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是按标识删除用例返回的错误。
				if deleteErr := p.DeleteKeywordByID(ctx, identity.UserID, accountID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return keywordMutationResult{KeywordID: int64(id)}, nil
			},
		},
		ToolDef{
			Name: "keyword_delete_by_index",
			Description: "按 keyword_list 返回顺序中的零基索引删除关键词规则（不可逆，索引易变请先列表确认）。" +
				"必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "index", Type: ArgInteger, Required: true, Description: "关键词规则在列表中的零基序号。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// index 是列表零基序号。
				index, err := args.Int("index")
				if err != nil {
					return nil, err
				}
				// deleteErr 是按序号删除用例返回的错误。
				if deleteErr := p.DeleteKeywordByIndex(ctx, identity.UserID, accountID, index); deleteErr != nil {
					return nil, deleteErr
				}
				return keywordIndexMutationResult{Index: index}, nil
			},
		},
	)
}

// registerItemReplyTools 注册指定商品回复工具。
func (e *Endpoint) registerItemReplyTools(p capability.KeywordPorts) {
	// itemArgs 是指定商品回复的公共账号与商品入参。
	itemArgs := []ArgSpec{
		{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
		{Name: "item_id", Type: ArgString, Required: true, Description: "商品标识。"},
	}
	e.RegisterTools(
		ToolDef{
			Name:        "item_reply_list",
			Description: "列出全部账号的指定商品回复配置。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是指定商品回复列表。
				rows, listErr := p.ListItemReplies(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				return itemReplyListResult{Total: len(rows), Replies: itemReplyDTOsFromApp(rows)}, nil
			},
		},
		ToolDef{
			Name:        "item_reply_get",
			Description: "读取指定账号与商品的回复配置。只读；未配置时返回未找到。",
			Args:        itemArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、itemID 是账号与商品标识。
				accountID, itemID, idErr := itemReplyIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// reply、getErr 是指定商品回复。
				reply, getErr := p.GetItemReply(ctx, identity.UserID, accountID, itemID)
				if getErr != nil {
					return nil, getErr
				}
				return itemReplyDTO{ItemID: reply.ItemID, AccountID: reply.CookieID, ReplyContent: reply.ReplyContent}, nil
			},
		},
		ToolDef{
			Name:        "item_reply_set",
			Description: "保存或覆盖指定账号与商品的回复内容。",
			Args: append(append([]ArgSpec{}, itemArgs...),
				ArgSpec{Name: "reply_content", Type: ArgString, Required: true, Description: "商品命中后的回复正文。"}),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、itemID 是账号与商品标识。
				accountID, itemID, idErr := itemReplyIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// content 是回复正文。
				content, contentErr := args.String("reply_content")
				if contentErr != nil {
					return nil, contentErr
				}
				// setErr 是商品回复写入用例返回的错误。
				if setErr := p.SetItemReply(ctx, identity.UserID, accountID, itemID, content); setErr != nil {
					return nil, setErr
				}
				return itemReplyDTO{ItemID: itemID, AccountID: accountID, ReplyContent: content}, nil
			},
		},
		ToolDef{
			Name:        "item_reply_delete",
			Description: "删除指定账号与商品的回复配置（不可逆）。必须显式 confirm=true。",
			Destructive: true,
			Args:        itemArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID、itemID 是账号与商品标识。
				accountID, itemID, idErr := itemReplyIDs(args)
				if idErr != nil {
					return nil, idErr
				}
				// deleteErr 是商品回复删除用例返回的错误。
				if deleteErr := p.DeleteItemReply(ctx, identity.UserID, accountID, itemID); deleteErr != nil {
					return nil, deleteErr
				}
				return itemReplyDTO{ItemID: itemID, AccountID: accountID}, nil
			},
		},
	)
}

// itemReplyIDs 读取指定商品回复的账号与商品标识。
func itemReplyIDs(args Arguments) (accountID, itemID string, err error) {
	// accountID 是账号标识。
	accountID, err = args.String("account_id")
	if err != nil {
		return "", "", err
	}
	// itemID 是商品标识。
	itemID, err = args.String("item_id")
	if err != nil {
		return "", "", err
	}
	return accountID, itemID, nil
}

// draft 把关键词入参转换为应用层草稿。
func (r keywordDraftRequest) draft() keywordsapp.Draft {
	return keywordsapp.Draft{
		Keyword: r.Keyword, Reply: r.Reply, ItemID: r.ItemID, Type: r.Type, ImageURL: r.ImageURL,
	}
}

// defaultReplyDTOsFromApp 批量映射默认回复配置为非敏感视图。
func defaultReplyDTOsFromApp(summaries []defaultreplyapp.Summary) []defaultReplyDTO {
	// dtos 是默认回复视图列表。
	dtos := make([]defaultReplyDTO, 0, len(summaries))
	// summary 是当前待映射的配置。
	for _, summary := range summaries {
		dtos = append(dtos, defaultReplyDTO{
			AccountID: summary.CookieID, Enabled: summary.Reply.Enabled,
			ReplyContent: summary.Reply.ReplyContent, ReplyImageURL: summary.Reply.ReplyImageURL,
			ReplyOnce: summary.Reply.ReplyOnce,
		})
	}
	return dtos
}

// keywordDTOsFromApp 批量映射关键词规则为非敏感视图。
func keywordDTOsFromApp(accountID string, rows []keywordsapp.Keyword) []keywordDTO {
	// dtos 是关键词视图列表。
	dtos := make([]keywordDTO, 0, len(rows))
	// row 是当前待映射的关键词规则。
	for _, row := range rows {
		// account 是规则所属账号；应用模型未回填时使用查询账号。
		account := row.CookieID
		if account == "" {
			account = accountID
		}
		dtos = append(dtos, keywordDTO{
			KeywordID: row.ID, AccountID: account, Keyword: row.Keyword, Reply: row.Reply,
			ItemID: row.ItemID, Type: row.Type, ImageURL: row.ImageURL,
		})
	}
	return dtos
}

// itemReplyDTOsFromApp 批量映射指定商品回复为非敏感视图。
func itemReplyDTOsFromApp(rows []keywordsapp.ItemReply) []itemReplyDTO {
	// dtos 是指定商品回复视图列表。
	dtos := make([]itemReplyDTO, 0, len(rows))
	// row 是当前待映射的回复配置。
	for _, row := range rows {
		dtos = append(dtos, itemReplyDTO{ItemID: row.ItemID, AccountID: row.CookieID, ReplyContent: row.ReplyContent})
	}
	return dtos
}
