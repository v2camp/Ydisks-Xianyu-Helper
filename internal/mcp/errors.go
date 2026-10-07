// errors.go 定义 MCP 工具层的错误分类与面向管理员的中文错误映射。
//
// 所有错误最终都转换为 isError=true 的 MCP 工具结果：对外只暴露稳定类别与
// 可操作的中文摘要，禁止透传 SQL、堆栈、内部类型名或任何凭证片段。

package mcp

import (
	"errors"
	"strings"

	adminapp "xianyu-go/internal/application/admin"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	chatapp "xianyu-go/internal/application/chat"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	keywordsapp "xianyu-go/internal/application/keywords"
	notificationsapp "xianyu-go/internal/application/notifications"
	settingsapp "xianyu-go/internal/application/settings"
)

// ErrorClass 是对外暴露的稳定错误类别标识，Harness 可据此决定自纠或转人工。
type ErrorClass string

const (
	// ClassInvalidArgument 表示入参缺失、类型错误或未通过显式确认。
	ClassInvalidArgument ErrorClass = "invalid_argument"
	// ClassUnauthorized 表示调用身份无效或令牌未通过校验。
	ClassUnauthorized ErrorClass = "unauthorized"
	// ClassForbidden 表示目标资源不属于调用身份或操作被策略拒绝。
	ClassForbidden ErrorClass = "forbidden"
	// ClassNotFound 表示目标资源不存在。
	ClassNotFound ErrorClass = "not_found"
	// ClassSessionExpired 表示平台会话失效，需要账号续期后再试。
	ClassSessionExpired ErrorClass = "session_expired"
	// ClassUncertain 表示外部动作结果不确定，必须转人工核对，禁止自动重试。
	ClassUncertain ErrorClass = "needs_review"
	// ClassInternal 表示未归类的服务端内部错误；对外不回传内部细节。
	ClassInternal ErrorClass = "internal_error"
)

// ClassifiedError 由领域工具在明确知道语义时返回，携带稳定类别、中文摘要与内部原因。
// 内部原因只用于本进程日志，不直接写入 MCP 响应文本。
type ClassifiedError struct {
	// Class 是稳定错误类别。
	Class ErrorClass
	// PublicMessage 是可展示给管理员与 Harness 的中文摘要。
	PublicMessage string
	// cause 是内部原始错误，供日志关联，禁止序列化进响应。
	cause error
}

// Error 实现 error 接口，返回包含类别的中文描述；不含内部原因细节，避免日志外泄漏。
func (e *ClassifiedError) Error() string {
	return string(e.Class) + ": " + e.PublicMessage
}

// Unwrap 返回内部原因，支持 errors.Is/As 继续匹配底层哨兵错误。
func (e *ClassifiedError) Unwrap() error {
	return e.cause
}

// Fail 构造一个已分类错误；publicMessage 必须是不含敏感信息的中文可操作提示。
func Fail(class ErrorClass, publicMessage string, cause error) *ClassifiedError {
	return &ClassifiedError{Class: class, PublicMessage: publicMessage, cause: cause}
}

// InvalidArgument 是入参错误的快捷构造。
func InvalidArgument(publicMessage string) *ClassifiedError {
	return Fail(ClassInvalidArgument, publicMessage, nil)
}

// Classify 把应用层错误归一为稳定类别与中文摘要。
// 已分类错误原样返回；已知应用哨兵按语义映射；其余错误统一为内部错误且不回传原文。
func Classify(err error) (ErrorClass, string) {
	if err == nil {
		return "", ""
	}
	// classified 是工具层显式分类的错误，直接采用其类别与公开摘要。
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified.Class, classified.PublicMessage
	}
	// 各应用包携带稳定中文提示的输入校验错误优先按入参类错误返回。
	if class, message, ok := classifyValidationError(err); ok {
		return class, message
	}
	// 各应用包的哨兵错误按业务域顺序映射为稳定类别与中文提示。
	if class, message, ok := classifySentinelError(err); ok {
		return class, message
	}
	// message 是对已知风控/会话失效关键字的保守中文归一；未命中时不回传原文。
	message := err.Error()
	switch {
	case strings.Contains(message, "needs_review") || strings.Contains(message, "不确定") || strings.Contains(message, "人工"):
		return ClassUncertain, "外部动作结果不确定，已转人工核对，请勿自动重试"
	case strings.Contains(message, "会话失效") || strings.Contains(message, "auth_expired") || strings.Contains(message, "登录已失效"):
		return ClassSessionExpired, "平台登录态已失效，请在 Web 端完成账号续期后重试"
	}
	return ClassInternal, "服务端处理失败，请查看服务日志或稍后重试"
}

// classifyValidationError 匹配各应用包携带稳定中文提示的输入校验错误。
// 命中时返回入参类错误与可直接展示的中文摘要；未命中时返回 ok=false。
func classifyValidationError(err error) (ErrorClass, string, bool) {
	// validation 是卡券业务校验错误，携带稳定中文提示，可直接作为入参类错误展示。
	var validation *cardsapp.ValidationError
	if errors.As(err, &validation) {
		return ClassInvalidArgument, validation.Message, true
	}
	// ruleValidation 是自动化规则与关键词回复的业务校验错误。
	var ruleValidation *automationapp.ValidationError
	if errors.As(err, &ruleValidation) {
		return ClassInvalidArgument, ruleValidation.Message, true
	}
	// keywordValidation 是关键词回复的稳定输入错误。
	var keywordValidation *keywordsapp.ValidationError
	if errors.As(err, &keywordValidation) {
		return ClassInvalidArgument, keywordValidation.Message, true
	}
	// settingsValidation 是系统设置与 AI 设置的业务校验错误。
	var settingsValidation *settingsapp.ValidationError
	if errors.As(err, &settingsValidation) {
		return ClassInvalidArgument, settingsValidation.Message, true
	}
	return "", "", false
}

// classifySentinelError 按业务域顺序匹配各应用包导出的哨兵错误。
// 命中时返回该域映射的稳定类别与中文提示；全部未命中时返回 ok=false。
func classifySentinelError(err error) (ErrorClass, string, bool) {
	// classifiers 是各业务域哨兵分类器，按原匹配顺序排列以保证语义不变。
	classifiers := []func(error) (ErrorClass, string, bool){
		classifyCardsError,
		classifyAutomationError,
		classifyDeliveryError,
		classifyDefaultReplyError,
		classifyKeywordsError,
		classifyChatError,
		classifyNotificationsError,
		classifySettingsError,
		classifyAdminError,
	}
	// classifier 是当前业务域哨兵分类器。
	for _, classifier := range classifiers {
		// class、message、ok 是当前分类器的命中结果。
		if class, message, ok := classifier(err); ok {
			return class, message, true
		}
	}
	return "", "", false
}

// classifyCardsError 匹配卡券域的归属、标识与测试组件哨兵错误。
func classifyCardsError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, cardsapp.ErrNotFound):
		return ClassNotFound, "卡券组不存在", true
	case errors.Is(err, cardsapp.ErrForbidden):
		return ClassForbidden, "无权操作该卡券组：目标卡券组不属于当前管理员", true
	case errors.Is(err, cardsapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效，请检查 MCP 令牌与本地管理员账号", true
	case errors.Is(err, cardsapp.ErrInvalidCardID):
		return ClassInvalidArgument, "卡券组标识无效", true
	case errors.Is(err, cardsapp.ErrNotDataType):
		return ClassInvalidArgument, "只有 data（逐行卡密）类型卡券组支持追加卡密", true
	case errors.Is(err, cardsapp.ErrNotAPIType):
		return ClassInvalidArgument, "只有 api（接口取卡）类型卡券组支持连通性测试", true
	case errors.Is(err, cardsapp.ErrAPITesterUnavailable):
		return ClassInternal, "API 连通性测试组件当前不可用，请稍后重试或在服务端检查装配", true
	}
	return "", "", false
}

// classifyAutomationError 匹配自动化规则的运行状态、改价冲突与模板可用性哨兵错误。
func classifyAutomationError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, automationapp.ErrRuleNotFound):
		return ClassNotFound, "自动化规则不存在或不属于当前管理员", true
	case errors.Is(err, automationapp.ErrRuleActive):
		return ClassInvalidArgument, "自动化规则仍有待处理的运行，请先在 Web 端处理运行记录后再删除", true
	case errors.Is(err, automationapp.ErrPricingModeConflict):
		return ClassInvalidArgument, "该账号已启用 AI 议价，不能同时启用自动化规则改价", true
	case errors.Is(err, automationapp.ErrDeliveryTemplateUnavailable):
		return ClassInvalidArgument, "发货模板不存在或已停用，请重新选择后保存", true
	case errors.Is(err, automationapp.ErrInvalidInput):
		return ClassInternal, "自动化规则服务未就绪，请检查服务端装配", true
	}
	return "", "", false
}

// classifyDeliveryError 匹配发货模板的引用、变量契约与校验哨兵错误。
func classifyDeliveryError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, deliveryapp.ErrNotFound):
		return ClassNotFound, "发货模板不存在或不属于当前管理员", true
	case errors.Is(err, deliveryapp.ErrReferenced):
		return ClassInvalidArgument, "发货模板仍被自动化规则引用，请先删除或调整引用它的规则", true
	case errors.Is(err, deliveryapp.ErrVariableConflict):
		return ClassInvalidArgument, "发货模板变量契约冲突：变量键已被自动化规则引用，请保持原名不变", true
	case errors.Is(err, deliveryapp.ErrInvalidInput):
		// 发货模板校验错误只包含稳定的中文业务提示，可直接展示；不携带数据库或凭证细节。
		return ClassInvalidArgument, err.Error(), true
	}
	return "", "", false
}

// classifyDefaultReplyError 匹配账号默认回复的归属、账号与配置哨兵错误。
func classifyDefaultReplyError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, defaultreplyapp.ErrForbidden):
		return ClassForbidden, "无权操作该账号的默认回复", true
	case errors.Is(err, defaultreplyapp.ErrAccountNotFound):
		return ClassNotFound, "目标账号不存在", true
	case errors.Is(err, defaultreplyapp.ErrInvalidCookieID):
		return ClassInvalidArgument, "账号标识不能为空", true
	case errors.Is(err, defaultreplyapp.ErrConfigNotFound):
		return ClassNotFound, "该账号尚未配置默认回复", true
	case errors.Is(err, defaultreplyapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效，请检查 MCP 令牌与本地管理员账号", true
	}
	return "", "", false
}

// classifyKeywordsError 匹配关键词与指定商品回复的归属与入参哨兵错误。
func classifyKeywordsError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, keywordsapp.ErrNotFound):
		return ClassNotFound, "关键词或指定商品回复不存在", true
	case errors.Is(err, keywordsapp.ErrForbidden):
		return ClassForbidden, "无权操作该关键词回复", true
	case errors.Is(err, keywordsapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效，请检查 MCP 令牌与本地管理员账号", true
	case errors.Is(err, keywordsapp.ErrInvalidInput):
		return ClassInvalidArgument, "关键词回复参数无效，请检查账号标识与回复内容", true
	}
	return "", "", false
}

// classifyChatError 匹配聊天发送、会话、快捷回复与装配可用性哨兵错误。
func classifyChatError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, chatapp.ErrSendUncertain):
		return ClassUncertain, "消息发送结果不确定，请先在闲鱼核对是否已经发出，确认未发出后再重试", true
	case errors.Is(err, chatapp.ErrStatusSave):
		return ClassUncertain, "消息可能已经发出，但本地状态保存失败，请先人工核对后再决定是否重发", true
	case errors.Is(err, chatapp.ErrOffline):
		return ClassInvalidArgument, "发送账号当前离线，请先在 Web 端确认账号运行状态后重试", true
	case errors.Is(err, chatapp.ErrSend):
		return ClassInternal, "平台消息发送失败，请稍后重试或在服务端查看日志", true
	case errors.Is(err, chatapp.ErrSessionForbidden), errors.Is(err, chatapp.ErrMetadataForbidden), errors.Is(err, chatapp.ErrChatItemForbidden):
		return ClassForbidden, "无权访问该聊天账号或会话", true
	case errors.Is(err, chatapp.ErrChatSessionNotFound):
		return ClassNotFound, "聊天会话不存在或尚未同步，请先刷新联系人", true
	case errors.Is(err, chatapp.ErrQuickReplyNotFound):
		return ClassNotFound, "快捷回复不存在", true
	case errors.Is(err, chatapp.ErrQuickReplyLimitReached):
		return ClassInvalidArgument, "该账号的快捷回复数量已达上限，请先删除不再使用的快捷回复", true
	case errors.Is(err, chatapp.ErrSessionUnavailable), errors.Is(err, chatapp.ErrRefreshUnavailable), errors.Is(err, chatapp.ErrSubscriptionUnavailable), errors.Is(err, chatapp.ErrMetadataUnavailable), errors.Is(err, chatapp.ErrUnavailable), errors.Is(err, chatapp.ErrChatItemUnavailable), errors.Is(err, chatapp.ErrChatItemCreate):
		return ClassInternal, "聊天服务未启用或尚未装配，请检查服务端配置", true
	case errors.Is(err, chatapp.ErrRefreshPersist):
		return ClassInternal, "平台聊天数据已拉取但本地保存失败，请重试刷新", true
	case errors.Is(err, chatapp.ErrInvalidInput), errors.Is(err, chatapp.ErrSendInvalidInput), errors.Is(err, chatapp.ErrChatItemInvalid):
		return ClassInvalidArgument, "聊天参数无效，请检查账号、会话与消息内容", true
	}
	return "", "", false
}

// classifyNotificationsError 匹配通知渠道、账号绑定与通知器装配哨兵错误。
func classifyNotificationsError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, notificationsapp.ErrChannelNotFound):
		return ClassNotFound, "通知渠道或账号绑定不存在", true
	case errors.Is(err, notificationsapp.ErrChannelForbidden), errors.Is(err, notificationsapp.ErrAccountForbidden):
		return ClassForbidden, "无权操作该通知渠道或账号绑定", true
	case errors.Is(err, notificationsapp.ErrChannelInvalidInput):
		return ClassInvalidArgument, "通知渠道参数无效，请检查名称、类型与配置 JSON", true
	case errors.Is(err, notificationsapp.ErrNotifierUnavailable):
		return ClassInternal, "通知器未启用或尚未装配，请检查服务端配置", true
	case errors.Is(err, notificationsapp.ErrInvalidInput):
		return ClassInvalidArgument, "通知查询参数无效", true
	}
	return "", "", false
}

// classifySettingsError 匹配系统与 AI 设置的归属、配置与改价冲突哨兵错误。
func classifySettingsError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, settingsapp.ErrForbidden):
		return ClassForbidden, "无权操作该资源：目标账号或配置不属于当前管理员", true
	case errors.Is(err, settingsapp.ErrAccountNotFound):
		return ClassNotFound, "目标账号不存在", true
	case errors.Is(err, settingsapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效，请检查 MCP 令牌与本地管理员账号", true
	case errors.Is(err, settingsapp.ErrConfigNotFound):
		return ClassNotFound, "指定配置不存在", true
	case errors.Is(err, settingsapp.ErrPricingModeConflict):
		return ClassInvalidArgument, "AI 议价与自动化规则改价不能同时启用，请先关闭另一种改价方式", true
	}
	return "", "", false
}

// classifyAdminError 匹配管理员自删、身份与运行时停止哨兵错误。
func classifyAdminError(err error) (ErrorClass, string, bool) {
	switch {
	case errors.Is(err, adminapp.ErrSelfDelete):
		return ClassInvalidArgument, "不能删除当前登录管理员账号", true
	case errors.Is(err, adminapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效", true
	case errors.Is(err, adminapp.ErrRuntimeStop):
		return ClassInternal, "停止账号运行实例失败，请稍后重试或在服务端查看日志", true
	}
	return "", "", false
}
