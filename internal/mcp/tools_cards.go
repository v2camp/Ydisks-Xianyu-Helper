// tools_cards.go 注册卡密库存域工具：卡券组查询、创建、更新、删除、追加库存与
// API 配置连通性测试。
//
// 安全红线：data 类型卡券组的库存正文（逐行卡密）永不进入 MCP 输出，列表与详情
// 只返回按非空行计算的 stock_lines 计数；api 类型只返回脱敏配置摘要，请求头、
// 参数、密钥与响应正文永不回传。库存与 API 密钥仅经写入参数进入应用层。

package mcp

import (
	"context"
	"strings"

	cardsapp "xianyu-go/internal/application/cards"
)

// cardDTO 是卡券组的非敏感视图；不含逐行卡密、API 请求头、参数或密钥。
type cardDTO struct {
	// ID 是卡券组标识。
	ID int64 `json:"card_id"`
	// Name 是卡券组名称。
	Name string `json:"name"`
	// Type 是卡券类型：text/data/image/api。
	Type string `json:"type"`
	// Description 是运营说明。
	Description string `json:"description"`
	// Enabled 表示自动化是否可使用该组。
	Enabled bool `json:"enabled"`
	// DelaySeconds 是发货延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// IsMultiSpec 表示是否按规格匹配。
	IsMultiSpec bool `json:"multi_spec"`
	// SpecName 是匹配规格名。
	SpecName string `json:"spec_name,omitempty"`
	// SpecValue 是匹配规格值。
	SpecValue string `json:"spec_value,omitempty"`
	// TextContent 是 text 类型的发货话术模板（运营配置文本，非逐行卡密）。
	TextContent string `json:"text_content,omitempty"`
	// ImageURL 是 image 类型的发货图片地址。
	ImageURL string `json:"image_url,omitempty"`
	// StockLines 是 data 类型的非空库存行数；其它类型为 0。
	StockLines int `json:"stock_lines"`
	// APIConfig 是 api 类型的脱敏摘要；其它类型为 nil。
	APIConfig *cardAPIConfigDTO `json:"api_config,omitempty"`
}

// cardAPIConfigDTO 是 API 卡券的脱敏配置摘要。
type cardAPIConfigDTO struct {
	// URL 是请求地址。
	URL string `json:"url"`
	// Method 是 HTTP 方法。
	Method string `json:"method"`
	// TimeoutSeconds 是超时秒数。
	TimeoutSeconds int `json:"timeout_seconds"`
	// ContentType 是请求媒体类型。
	ContentType string `json:"content_type,omitempty"`
	// ResponsePath 是响应取值路径。
	ResponsePath string `json:"response_path,omitempty"`
	// RetryEnabled 表示是否启用失败重试。
	RetryEnabled bool `json:"retry_enabled"`
	// HeadersConfigured 表示是否已配置请求头（不返回内容）。
	HeadersConfigured bool `json:"headers_configured"`
	// ParamsConfigured 表示是否已配置请求参数（不返回内容）。
	ParamsConfigured bool `json:"params_configured"`
	// Ready 表示配置是否通过校验可用于自动化。
	Ready bool `json:"ready"`
	// ValidationError 是脱敏的配置校验错误。
	ValidationError string `json:"validation_error,omitempty"`
}

// cardListResult 是卡券组列表返回。
type cardListResult struct {
	// Total 是卡券组数量。
	Total int `json:"total"`
	// Cards 是卡券组视图列表。
	Cards []cardDTO `json:"cards"`
}

// cardMutationResult 是创建/更新/追加操作的确认返回。
type cardMutationResult struct {
	// CardID 是卡券组标识。
	CardID int64 `json:"card_id"`
	// AppendedLines 是追加操作新增的库存行数。
	AppendedLines int `json:"appended_lines,omitempty"`
}

// cardAPITestResult 是已保存 API 卡券组连通性测试的非敏感诊断结果。
type cardAPITestResult struct {
	// CardID 是被测试的卡券组标识。
	CardID int64 `json:"card_id"`
	// Status 是测试结论：success 表示远端返回 2xx，failure 表示完成但非 2xx，error 表示请求未完成。
	Status string `json:"status"`
	// StatusCode 是远端 HTTP 状态码；网络错误时为 0。
	StatusCode int `json:"status_code"`
	// ResponseType 是远端响应声明的媒体类型。
	ResponseType string `json:"response_type,omitempty"`
	// ResponseFields 是响应 JSON 顶层字段名列表，只含键名不含值。
	ResponseFields []string `json:"response_fields,omitempty"`
	// ExtractedValue 是按 response_path 提取的限长文本，不含请求头或密钥。
	ExtractedValue string `json:"extracted_value,omitempty"`
	// ResponsePreview 是限长响应预览，用于诊断返回格式。
	ResponsePreview string `json:"response_preview,omitempty"`
}

// RegisterCardTools 注册卡密库存域全部工具。
// p 是卡密应用用例端口，nil 时跳过注册以支持部分能力装配。
func (e *Endpoint) RegisterCardTools(p CardPorts) {
	if p == nil {
		return
	}
	e.registerCardReadTools(p)
	e.registerCardMutationTools(p)
	e.registerCardStockTools(p)
}

// registerCardReadTools 注册卡密只读查询工具：卡券组列表与单组详情。
// p 是卡密应用用例端口。
func (e *Endpoint) registerCardReadTools(p CardPorts) {
	e.RegisterTools(
		ToolDef{
			Name: "card_list",
			Description: "列出全部卡券组的非敏感信息：类型、开关、延迟、规格匹配、库存行数。" +
				"不返回明文卡密、API 请求头或密钥。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是卡券组列表。
				rows, listErr := p.ListCards(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				// dtos 是卡券视图列表。
				dtos := make([]cardDTO, 0, len(rows))
				// row 是当前待映射的卡券组应用模型。
				for _, row := range rows {
					dtos = append(dtos, cardDTOFromApp(row))
				}
				return cardListResult{Total: len(dtos), Cards: dtos}, nil
			},
		},
		ToolDef{
			Name: "card_get",
			Description: "读取单个卡券组详情：text 返回发货话术模板、image 返回图片地址、data 只返回库存行数、" +
				"api 只返回脱敏配置摘要。绝不返回明文卡密或 API 密钥。只读。",
			Args: []ArgSpec{{Name: "card_id", Type: ArgInteger, Required: true, Description: "卡券组标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是卡券组标识。
				id, err := args.Int("card_id")
				if err != nil {
					return nil, err
				}
				// card、getErr 是卡券组应用模型。
				card, getErr := p.GetCard(ctx, identity.UserID, int64(id))
				if getErr != nil {
					return nil, getErr
				}
				return cardDTOFromApp(card), nil
			},
		},
	)
}

// registerCardMutationTools 注册卡券组创建与更新工具：两者共用元数据与内容入参。
// p 是卡密应用用例端口。
func (e *Endpoint) registerCardMutationTools(p CardPorts) {
	// commonArgs 是创建/更新共用的卡券元数据参数。
	commonArgs := []ArgSpec{
		{Name: "name", Type: ArgString, Required: true, Description: "卡券组名称。"},
		{Name: "type", Type: ArgString, Required: true, Enum: []string{"text", "data", "image", "api"},
			Description: "卡券类型：text=发货文本，data=逐行卡密，image=发货图片，api=接口取卡。"},
		{Name: "description", Type: ArgString, Description: "卡券组运营说明。"},
		{Name: "enabled", Type: ArgBoolean, Description: "是否允许自动化规则使用该组，默认 true。"},
		{Name: "delay_seconds", Type: ArgInteger, Description: "自动发货前延迟秒数，0-3600。"},
		{Name: "multi_spec", Type: ArgBoolean, Description: "是否仅匹配指定商品规格。"},
		{Name: "spec_name", Type: ArgString, Description: "多规格匹配的规格名。"},
		{Name: "spec_value", Type: ArgString, Description: "多规格匹配的规格值。"},
	}
	// contentArgs 是按类型提交内容的参数；全部只写，除 text 话术与 image 地址外不回显。
	contentArgs := []ArgSpec{
		{Name: "text_content", Type: ArgString, Description: "text 类型的发货话术。"},
		{Name: "image_url", Type: ArgString, Description: "image 类型的发货图片公网地址。"},
		{Name: "data_content", Type: ArgString, Description: "data 类型的逐行卡密，每行一条；更新时传入会整体替换库存，必须带 confirm=true。"},
		{Name: "api_config", Type: ArgString, Description: "api 类型的完整 JSON 配置（密钥仅写入）。"},
	}
	// createArgs 是创建卡券组的全部入参：名称与类型必填，类型不可在更新时修改。
	createArgs := append([]ArgSpec{
		{Name: "name", Type: ArgString, Required: true, Description: "卡券组名称。"},
		{Name: "type", Type: ArgString, Required: true, Enum: []string{"text", "data", "image", "api"},
			Description: "卡券类型：text=发货文本，data=逐行卡密，image=发货图片，api=接口取卡。"},
	}, commonArgsWithout(commonArgs, "name", "type")...)
	createArgs = append(createArgs, contentArgs...)
	// updateArgs 是更新卡券组的全部入参：类型沿用原值不可修改，未传字段保留现值。
	updateArgs := append([]ArgSpec{
		{Name: "card_id", Type: ArgInteger, Required: true, Description: "卡券组标识。"},
		{Name: "name", Type: ArgString, Description: "卡券组名称；不传则保留现名称。"},
	}, commonArgsWithout(commonArgs, "name", "type")...)
	updateArgs = append(updateArgs, contentArgs...)
	e.RegisterTools(
		ToolDef{
			Name: "card_create",
			Description: "创建卡券组。按 type 提供对应内容：text 传 text_content（发货话术）；" +
				"image 传 image_url；data 传 data_content（每行一条卡密，写入后不可经 MCP 读回）；" +
				"api 传 api_config（完整 JSON 配置，含请求头密钥，仅保存不回显）。",
			Args: createArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// draft、buildErr 是组装好的创建草稿。
				draft, buildErr := cardDraft(args, nil)
				if buildErr != nil {
					return nil, buildErr
				}
				// id、createErr 是新卡券组标识。
				id, createErr := p.CreateCard(ctx, identity.UserID, draft)
				if createErr != nil {
					return nil, createErr
				}
				return cardMutationResult{CardID: id}, nil
			},
		},
		ToolDef{
			Name: "card_update",
			Description: "更新卡券组元数据与内容。覆盖 data 库存（传 data_content）会替换全部现有卡密，属于高风险操作，" +
				"必须显式 confirm=true；仅改名/开关/延迟等元数据时不需要 confirm。api 配置更新必须重传完整 api_config。",
			Args: updateArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是卡券组标识。
				id, err := args.Int("card_id")
				if err != nil {
					return nil, err
				}
				// 覆盖库存是高风险操作，强制确认。
				if _, replacesData := args["data_content"]; replacesData && !RequireConfirm(args) {
					return nil, InvalidArgument("覆盖卡密库存会替换全部现有卡密，必须显式传 confirm=true")
				}
				// existing、getErr 是现有卡券组，更新必须沿用原类型。
				existing, getErr := p.GetCard(ctx, identity.UserID, int64(id))
				if getErr != nil {
					return nil, getErr
				}
				// draft、buildErr 是按原类型组装的更新草稿。
				draft, buildErr := cardDraft(args, &existing)
				if buildErr != nil {
					return nil, buildErr
				}
				// updateErr 是更新用例返回的错误。
				if updateErr := p.UpdateCard(ctx, identity.UserID, int64(id), draft); updateErr != nil {
					return nil, updateErr
				}
				return cardMutationResult{CardID: int64(id)}, nil
			},
		},
	)
}

// registerCardStockTools 注册卡券组库存维护与连通性测试工具：删除、追加卡密与 API 测试。
// p 是卡密应用用例端口。
func (e *Endpoint) registerCardStockTools(p CardPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "card_delete",
			Description: "删除卡券组及其全部库存与关联自动化配置（不可逆）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "card_id", Type: ArgInteger, Required: true, Description: "要删除的卡券组标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是卡券组标识。
				id, err := args.Int("card_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是删除用例返回的错误。
				if deleteErr := p.DeleteCard(ctx, identity.UserID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return cardMutationResult{CardID: int64(id)}, nil
			},
		},
		ToolDef{
			Name: "card_append_data",
			Description: "向 data 卡券组追加逐行卡密（每行一条，空行忽略）；只写不读，返回新增行数。" +
				"追加的卡密无法通过任何 MCP 工具读回。",
			Args: []ArgSpec{
				{Name: "card_id", Type: ArgInteger, Required: true, Description: "data 类型卡券组标识。"},
				{Name: "data_content", Type: ArgString, Required: true, Description: "要追加的卡密文本，每行一条。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是卡券组标识。
				id, err := args.Int("card_id")
				if err != nil {
					return nil, err
				}
				// content 是待追加卡密文本。
				content, err := args.String("data_content")
				if err != nil {
					return nil, err
				}
				// appended、appendErr 是新增行数。
				appended, appendErr := p.AppendCardData(ctx, identity.UserID, int64(id), content)
				if appendErr != nil {
					return nil, appendErr
				}
				return cardMutationResult{CardID: int64(id), AppendedLines: appended}, nil
			},
		},
		ToolDef{
			Name: "card_test_api",
			Description: "用已保存的 api 卡券配置发起一次连通性测试（外部 HTTP 请求，受公网出站策略约束），" +
				"返回状态码、媒体类型、响应顶层字段名与限长预览，不返回密钥。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "card_id", Type: ArgInteger, Required: true, Description: "api 类型卡券组标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是卡券组标识。
				id, err := args.Int("card_id")
				if err != nil {
					return nil, err
				}
				// result、testErr 是脱敏测试结果；完整配置由应用层在内部读取，不进入 MCP 层。
				result, testErr := p.TestCardAPI(ctx, identity.UserID, int64(id))
				if testErr != nil {
					return nil, testErr
				}
				return cardAPITestResult{
					CardID: int64(id), Status: result.Status, StatusCode: result.StatusCode,
					ResponseType: result.ResponseContentType, ResponseFields: result.ResponseFields,
					ExtractedValue: result.ExtractedValue, ResponsePreview: result.ResponsePreview,
				}, nil
			},
		},
	)
}

// commonArgsWithout 返回去掉指定名称后的通用参数切片（创建自行声明 name/type，更新不允许改类型）。
func commonArgsWithout(args []ArgSpec, removed ...string) []ArgSpec {
	// skipped 是需要剔除的参数名集合。
	skipped := make(map[string]struct{}, len(removed))
	// name 是当前待加入剔除集合的参数名。
	for _, name := range removed {
		skipped[name] = struct{}{}
	}
	// kept 是保留的参数。
	kept := make([]ArgSpec, 0, len(args))
	// arg 是当前待判定去留的参数定义。
	for _, arg := range args {
		// excluded 表示该参数是否命中剔除集合。
		_, excluded := skipped[arg.Name]
		if excluded {
			continue
		}
		kept = append(kept, arg)
	}
	return kept
}

// cardDraft 把 MCP 入参组装为应用层草稿；existing 非空表示更新场景，类型沿用原卡券组。
func cardDraft(args Arguments, existing *cardsapp.Card) (cardsapp.Draft, error) {
	// cardType 是卡券类型；更新时沿用现有类型，创建时必填。
	cardType := args.OptionalString("type", "")
	if existing != nil {
		cardType = existing.Type
	}
	if cardType == "" {
		return cardsapp.Draft{}, InvalidArgument("缺少必填参数 type")
	}
	// draft 是组装中的草稿，默认启用，开关未传时创建场景按 true 处理。
	draft := cardsapp.Draft{
		Type:         cardType,
		Name:         args.OptionalString("name", ""),
		Description:  args.OptionalString("description", ""),
		Enabled:      args.OptionalBool("enabled", true),
		DelaySeconds: args.OptionalInt("delay_seconds", 0),
	}
	// 更新场景下未传名称则沿用旧名称，避免名称被意外清空。
	if existing != nil && draft.Name == "" {
		draft.Name = existing.Name
	}
	// 多规格字段在更新时未传则沿用现值。
	draft.IsMultiSpec = args.OptionalBool("multi_spec", false)
	draft.IsMultiSpecSet = true
	if existing != nil {
		// provided 表示是否显式提交了多规格开关；未提交时沿用现值。
		_, provided := args["multi_spec"]
		if !provided {
			draft.IsMultiSpec = existing.IsMultiSpec
		}
	}
	// specName、specOK 是规格名入参及其存在性。
	if specName, specOK := args["spec_name"]; specOK {
		// specText 是规格名字符串。
		if specText, ok := specName.(string); ok {
			draft.SpecName, draft.SpecNameSet = specText, true
		}
	}
	// value、ok 是规格值入参及其存在性。
	if value, ok := args["spec_value"]; ok {
		// specText 是规格值字符串。
		if specText, ok := value.(string); ok {
			draft.SpecValue, draft.SpecValueSet = specText, true
		}
	}
	// 按类型装配内容字段。
	switch cardType {
	case "text":
		draft.TextContent = args.OptionalString("text_content", "")
		if existing != nil && draft.TextContent == "" {
			draft.TextContent = existing.TextContent
		}
	case "image":
		draft.ImageURL = args.OptionalString("image_url", "")
		if existing != nil && draft.ImageURL == "" {
			draft.ImageURL = existing.ImageURL
		}
	case "data":
		// content、provided 表示是否显式提交了库存正文。
		if content, provided := args["data_content"]; provided {
			// text 是库存正文字符串。
			if text, ok := content.(string); ok {
				draft.DataContent, draft.DataContentSet = text, true
			}
		}
	case "api":
		draft.APIConfig = args.OptionalString("api_config", "")
		if existing != nil && draft.APIConfig == "" {
			// 更新 API 卡券但未重传配置时沿用旧配置，应用层会据此保留密钥模板。
			draft.APIConfig = existing.APIConfig
		}
	default:
		return cardsapp.Draft{}, InvalidArgument("type 必须是 text、data、image 或 api")
	}
	if draft.DelaySeconds < 0 || draft.DelaySeconds > 3600 {
		return cardsapp.Draft{}, InvalidArgument("delay_seconds 必须在 0 到 3600 之间")
	}
	return draft, nil
}

// cardDTOFromApp 把应用层卡券模型映射为非敏感 MCP 视图。
func cardDTOFromApp(card cardsapp.Card) cardDTO {
	// dto 是剔除敏感字段后的视图。
	dto := cardDTO{
		ID: card.ID, Name: card.Name, Type: card.Type, Description: card.Description,
		Enabled: card.Enabled, DelaySeconds: card.DelaySeconds, IsMultiSpec: card.IsMultiSpec,
		SpecName: card.SpecName, SpecValue: card.SpecValue,
	}
	// 按类型补充非敏感内容；data 只给库存行数。
	switch card.Type {
	case "text":
		dto.TextContent = card.TextContent
	case "image":
		dto.ImageURL = card.ImageURL
	case "data":
		dto.StockLines = countDataLines(card.DataContent)
	case "api":
		// summary 是 API 配置的脱敏摘要。
		summary := cardsapp.SummarizeAPIConfig(card.APIConfig)
		if card.APIConfigSummary != nil {
			summary = *card.APIConfigSummary
		}
		dto.APIConfig = &cardAPIConfigDTO{
			URL: summary.URL, Method: summary.Method, TimeoutSeconds: summary.TimeoutSeconds,
			ContentType: summary.ContentType, ResponsePath: summary.ResponsePath,
			RetryEnabled: summary.RetryEnabled, HeadersConfigured: summary.HeadersConfigured,
			ParamsConfigured: summary.ParamsConfigured, Ready: summary.Ready,
			ValidationError: summary.ValidationError,
		}
	}
	return dto
}

// countDataLines 按与库存消费侧一致的口径统计非空卡密行数；内容不写入任何输出。
func countDataLines(content string) int {
	// count 是非空行计数。
	count := 0
	// line 是当前卡密行，仅判断空值不留存。
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
