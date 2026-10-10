// tools_delivery_templates.go 注册发货模板域工具：模板列表、详情、新建、更新与删除。
//
// 安全与语义红线：模板消息只包含运营话术与变量占位符，不含卡密明文；
// 变量契约与归属校验完全由 deliverytemplate 应用服务负责，MCP 层只做透传。

package mcp

import (
	"context"

	deliveryapp "xianyu-go/internal/application/deliverytemplate"

	"xianyu-go/internal/capability"
)

// RegisterDeliveryTemplateTools 注册发货模板域全部工具。
func (e *Endpoint) RegisterDeliveryTemplateTools(p capability.DeliveryTemplatePorts) {
	if p == nil {
		return
	}
	// draftArgs 是模板草稿的公共入参定义。
	draftArgs := []ArgSpec{
		{Name: "name", Type: ArgString, Required: true, Description: "模板名称。"},
		{Name: "enabled", Type: ArgBoolean, Description: "是否允许新规则引用该模板，默认 false。"},
		{Name: "messages", Type: ArgArray, Required: true,
			Description: "按发送顺序排列的消息正文数组，每条消息独立发送；支持 {变量名} 卡密变量。"},
	}
	e.RegisterTools(
		ToolDef{
			Name:        "template_list",
			Description: "列出发货模板及其有序消息、变量键。只读，不返回卡密明文。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是模板列表。
				rows, listErr := p.ListTemplates(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				return templateListResult{Total: len(rows), Templates: templateDTOsFromApp(rows)}, nil
			},
		},
		ToolDef{
			Name:        "template_get",
			Description: "读取单个发货模板详情：有序消息、卡密变量键与自定义变量键。只读。",
			Args:        []ArgSpec{{Name: "template_id", Type: ArgInteger, Required: true, Description: "发货模板标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是模板标识。
				id, err := args.Int("template_id")
				if err != nil {
					return nil, err
				}
				// template、getErr 是模板应用模型。
				template, getErr := p.GetTemplate(ctx, identity.UserID, int64(id))
				if getErr != nil {
					return nil, getErr
				}
				return templateDTOFromApp(template), nil
			},
		},
		ToolDef{
			Name: "template_create",
			Description: "创建发货模板：提交名称与有序消息数组，返回 template_id。" +
				"消息中的 {变量名} 会被登记为卡密变量键，供自动化规则绑定卡密组。",
			Args: draftArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// request 是模板草稿入参。
				var request templateDraftRequest
				// err 是入参解码错误。
				if err := decodeToolArguments(args, &request); err != nil {
					return nil, err
				}
				// id、createErr 是新模板标识与错误。
				id, createErr := p.CreateTemplate(ctx, identity.UserID, deliveryapp.Draft{
					Name: request.Name, Enabled: request.Enabled, Messages: request.Messages,
				})
				if createErr != nil {
					return nil, createErr
				}
				return templateMutationResult{TemplateID: id}, nil
			},
		},
		ToolDef{
			Name: "template_update",
			Description: "整体更新发货模板：消息数组会替换全部现有消息。被自动化规则引用且变量键发生不兼容变化时会被拒绝。" +
				"提交前建议先用 template_get 读取现内容。",
			Args: append([]ArgSpec{
				{Name: "template_id", Type: ArgInteger, Required: true, Description: "要更新的模板标识。"},
			}, draftArgs...),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是模板标识。
				id, err := args.Int("template_id")
				if err != nil {
					return nil, err
				}
				// request 是模板草稿入参。
				var request templateDraftRequest
				// err 是入参解码错误。
				if err := decodeToolArguments(args, &request); err != nil {
					return nil, err
				}
				// updateErr 是模板更新用例返回的错误。
				if updateErr := p.UpdateTemplate(ctx, identity.UserID, int64(id), deliveryapp.Draft{
					Name: request.Name, Enabled: request.Enabled, Messages: request.Messages,
				}); updateErr != nil {
					return nil, updateErr
				}
				return templateMutationResult{TemplateID: int64(id)}, nil
			},
		},
		ToolDef{
			Name:        "template_delete",
			Description: "删除发货模板（不可逆）。仍被自动化规则引用时会被拒绝，需要先删除或调整相关规则。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "template_id", Type: ArgInteger, Required: true, Description: "要删除的模板标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是模板标识。
				id, err := args.Int("template_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是模板删除用例返回的错误。
				if deleteErr := p.DeleteTemplate(ctx, identity.UserID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return templateMutationResult{TemplateID: int64(id)}, nil
			},
		},
	)
}

// templateDTOsFromApp 批量映射发货模板为非敏感视图。
func templateDTOsFromApp(templates []deliveryapp.Template) []templateDTO {
	// dtos 是模板视图列表。
	dtos := make([]templateDTO, 0, len(templates))
	// template 是当前待映射的模板应用模型。
	for _, template := range templates {
		dtos = append(dtos, templateDTOFromApp(template))
	}
	return dtos
}

// templateDTOFromApp 把发货模板应用模型映射为非敏感视图。
func templateDTOFromApp(template deliveryapp.Template) templateDTO {
	// messages 是模板消息视图列表。
	messages := make([]templateMessageDTO, 0, len(template.Messages))
	// message 是当前待映射的模板消息。
	for _, message := range template.Messages {
		messages = append(messages, templateMessageDTO{
			MessageID: message.ID, SortOrder: message.SortOrder, Content: message.Content,
		})
	}
	return templateDTO{
		TemplateID: template.ID, Name: template.Name, Enabled: template.Enabled, Messages: messages,
		Keys: template.Keys, CustomKeys: template.CustomKeys,
		CreatedAt: template.CreatedAt, UpdatedAt: template.UpdatedAt,
	}
}
