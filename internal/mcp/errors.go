// errors.go 定义 MCP 工具层的错误分类与面向管理员的中文错误映射。
//
// 所有错误最终都转换为 isError=true 的 MCP 工具结果：对外只暴露稳定类别与
// 可操作的中文摘要，禁止透传 SQL、堆栈、内部类型名或任何凭证片段。

package mcp

import (
	"errors"
	"strings"

	adminapp "xianyu-go/internal/application/admin"
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
	// 归属与身份类哨兵统一映射为越权/未认证/未找到中文提示。
	switch {
	case errors.Is(err, settingsapp.ErrForbidden):
		return ClassForbidden, "无权操作该资源：目标账号或配置不属于当前管理员"
	case errors.Is(err, settingsapp.ErrAccountNotFound):
		return ClassNotFound, "目标账号不存在"
	case errors.Is(err, settingsapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效，请检查 MCP 令牌与本地管理员账号"
	case errors.Is(err, settingsapp.ErrConfigNotFound):
		return ClassNotFound, "指定配置不存在"
	case errors.Is(err, adminapp.ErrSelfDelete):
		return ClassInvalidArgument, "不能删除当前登录管理员账号"
	case errors.Is(err, adminapp.ErrInvalidUser):
		return ClassUnauthorized, "管理员身份无效"
	case errors.Is(err, adminapp.ErrRuntimeStop):
		return ClassInternal, "停止账号运行实例失败，请稍后重试或在服务端查看日志"
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
