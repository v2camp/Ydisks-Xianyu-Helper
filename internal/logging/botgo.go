package logging

import (
	"fmt"
	"log/slog"

	botgolog "github.com/tencent-connect/botgo/log"

	"xianyu-go/internal/logsafe"
)

// 编译期断言：适配器必须完整实现 SDK 的日志接口，接口变更时在此处直接编译失败。
var _ botgolog.Logger = (*botgoLogger)(nil)

// InstallBotgoLogger 用项目日志器接管 QQ 官方 SDK 的全局日志输出。
// SDK 会在 Debug 级原样打印换取 Access Token 的请求体与响应体，其中含 AppSecret 与
// Access Token 明文，业务侧无法关闭该调用点，只能在进程启动阶段替换其全局 logger。
// 入站网关与出站通知共用同一 SDK，因此本函数必须覆盖两条链路，只需在进程内调用一次。
// logger 为 nil 时保持 SDK 默认行为，避免调用方误以为已完成接管。
func InstallBotgoLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	botgolog.DefaultLogger = &botgoLogger{logger: logger}
}

// botgoLogger 把 SDK 的日志接口转接到项目日志器，并在写出前统一清除凭证明文。
// 它不改变日志等级语义之外的内容，只负责拼接、脱敏与转发。
type botgoLogger struct {
	// logger 是项目进程级结构化日志器，其 handler 还会执行一次集中脱敏。
	logger *slog.Logger
}

// Debug 输出 SDK 调试日志，仅在进程开启调试等级时可见。
func (l *botgoLogger) Debug(v ...interface{}) {
	l.emit(slog.LevelDebug, "", v)
}

// Info 输出 SDK 普通日志；SDK 的 Info 级会打印完整收发报文，因此统一降级为调试等级。
func (l *botgoLogger) Info(v ...interface{}) {
	l.emit(slog.LevelDebug, "", v)
}

// Warn 输出 SDK 告警日志。
func (l *botgoLogger) Warn(v ...interface{}) {
	l.emit(slog.LevelWarn, "", v)
}

// Error 输出 SDK 错误日志。
func (l *botgoLogger) Error(v ...interface{}) {
	l.emit(slog.LevelError, "", v)
}

// Debugf 输出 SDK 格式化调试日志；format 与 v 由 SDK 原样传入，必须整体脱敏后再写出。
func (l *botgoLogger) Debugf(format string, v ...interface{}) {
	l.emit(slog.LevelDebug, format, v)
}

// Infof 输出 SDK 格式化普通日志；SDK 的 Info 级会打印完整收发报文，因此统一降级为调试等级。
func (l *botgoLogger) Infof(format string, v ...interface{}) {
	l.emit(slog.LevelDebug, format, v)
}

// Warnf 输出 SDK 格式化告警日志。
func (l *botgoLogger) Warnf(format string, v ...interface{}) {
	l.emit(slog.LevelWarn, format, v)
}

// Errorf 输出 SDK 格式化错误日志。
func (l *botgoLogger) Errorf(format string, v ...interface{}) {
	l.emit(slog.LevelError, format, v)
}

// Sync 满足 SDK 日志接口；项目日志直接写输出目标，无需额外刷盘，因此恒返回 nil。
func (l *botgoLogger) Sync() error {
	return nil
}

// emit 把一次 SDK 日志调用脱敏后转发给项目日志器。
// level 是调用方的原始等级；format 为空表示 SDK 使用非格式化接口；args 是 SDK 传入的参数。
func (l *botgoLogger) emit(level slog.Level, format string, args []interface{}) {
	if l == nil || l.logger == nil {
		return
	}
	// message 是拼接后的原始日志文本，可能含请求体、报文体或令牌字段等敏感载荷。
	message := joinSDKLog(format, args)
	// sanitized 是移除 URL 查询参数与凭证键值后的日志文本。
	sanitized := logsafe.Text(message)
	switch level {
	case slog.LevelWarn:
		l.logger.Warn(sanitized)
	case slog.LevelError:
		l.logger.Error(sanitized)
	default:
		l.logger.Debug(sanitized)
	}
}

// joinSDKLog 按 SDK 的调用方式拼接日志文本：format 非空时按格式化展开，否则直接连接参数。
// format 是 SDK 传入的格式串，args 是其参数；两者都来自第三方，禁止当作可信输入处理。
func joinSDKLog(format string, args []interface{}) string {
	if format == "" {
		return fmt.Sprint(args...)
	}
	return fmt.Sprintf(format, args...)
}
