package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	botgolog "github.com/tencent-connect/botgo/log"
)

// captureBotgoLog 接管 SDK 全局日志并返回输出缓冲与还原函数。
// t 用于登记清理动作；返回的缓冲收集接管期间的全部日志文本，还原函数必须 defer 调用。
func captureBotgoLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	// previousLogger 是测试前的 SDK 全局 logger，用例结束后还原。
	previousLogger := botgolog.DefaultLogger
	// previousLevel 是测试前的进程日志等级，用例结束后还原。
	previousLevel := Level.Level()
	// buffer 收集本次接管的全部日志文本。
	buffer := &bytes.Buffer{}
	Level.Set(slog.LevelDebug)
	InstallBotgoLogger(NewLogger(buffer, "text"))
	return buffer, func() {
		Level.Set(previousLevel)
		botgolog.DefaultLogger = previousLogger
	}
}

// tokenLike 复刻 oauth2.Token 的字段形态，用于验证 %+v 结构化输出同样会被脱敏。
type tokenLike struct {
	// AccessToken 是访问令牌。
	AccessToken string
	// RefreshToken 是刷新令牌。
	RefreshToken string
}

// TestInstallBotgoLoggerRedactsTokenRequest 验证 SDK 换取令牌的请求体不会把 AppSecret 明文写进日志。
func TestInstallBotgoLoggerRedactsTokenRequest(t *testing.T) {
	// buffer 是本次接管的日志输出，cleanup 负责还原全局状态。
	buffer, cleanup := captureBotgoLog(t)
	defer cleanup()
	// secret 是伪造的 AppSecret，任何情况下都不得出现在日志里。
	const secret = "test-secret-value"
	botgolog.DefaultLogger.Debugf("retrieve access token URL:%v req:%v",
		"https://bots.qq.com/app/getAppAccessToken", `{"appId":"102012345","clientSecret":"`+secret+`"}`)
	// output 是 SDK 日志经脱敏后的最终文本。
	output := buffer.String()
	if strings.Contains(output, secret) {
		t.Fatalf("SDK 调试日志泄露了 AppSecret: %s", output)
	}
	if !strings.Contains(output, "<redacted>") {
		t.Fatalf("SDK 调试日志未标记脱敏占位符: %s", output)
	}
	if !strings.Contains(output, "clientSecret") {
		t.Fatalf("脱敏后应保留键名以便定位来源: %s", output)
	}
}

// TestInstallBotgoLoggerRedactsTokenResponse 验证 SDK 打印的令牌响应体不会把 Access Token 明文写进日志。
func TestInstallBotgoLoggerRedactsTokenResponse(t *testing.T) {
	// buffer 是本次接管的日志输出，cleanup 负责还原全局状态。
	buffer, cleanup := captureBotgoLog(t)
	defer cleanup()
	// accessToken 是伪造的 Access Token，任何情况下都不得出现在日志里。
	const accessToken = "token-value-should-never-appear"
	// refreshToken 是伪造的刷新令牌，同样不得出现在日志里。
	const refreshToken = "refresh-should-never-appear"
	botgolog.DefaultLogger.Debugf("access token:%v traceID:%v", `{"access_token":"`+accessToken+`","expires_in":"7200"}`, "trace-1")
	botgolog.DefaultLogger.Debugf("token:%+v ", &tokenLike{AccessToken: accessToken, RefreshToken: refreshToken})
	// output 是 SDK 日志经脱敏后的最终文本。
	output := buffer.String()
	if strings.Contains(output, accessToken) || strings.Contains(output, refreshToken) {
		t.Fatalf("SDK 调试日志泄露了 Access Token: %s", output)
	}
}

// TestInstallBotgoLoggerRoutesLevels 验证 SDK 普通日志降级为调试等级，避免默认等级下刷屏。
func TestInstallBotgoLoggerRoutesLevels(t *testing.T) {
	// buffer 收集接管的日志文本，cleanup 负责还原全局状态。
	buffer, cleanup := captureBotgoLog(t)
	defer cleanup()
	Level.Set(slog.LevelInfo)
	botgolog.DefaultLogger.Info("listening heartBeat")
	botgolog.DefaultLogger.Warn("recv ctx exit refresh token")
	// output 是接管的日志文本。
	output := buffer.String()
	if strings.Contains(output, "listening heartBeat") {
		t.Fatalf("SDK 普通日志不应在 info 等级输出: %s", output)
	}
	if !strings.Contains(output, "recv ctx exit refresh token") {
		t.Fatalf("SDK 告警日志必须输出: %s", output)
	}
}

// TestInstallBotgoLoggerIgnoresNilLogger 验证传入空日志器时不接管全局 logger。
func TestInstallBotgoLoggerIgnoresNilLogger(t *testing.T) {
	// previous 是测试前的全局 logger，用于确认空参调用没有替换它。
	previous := botgolog.DefaultLogger
	InstallBotgoLogger(nil)
	if botgolog.DefaultLogger != previous {
		t.Fatal("传入空日志器时不得替换 SDK 全局 logger")
	}
}
