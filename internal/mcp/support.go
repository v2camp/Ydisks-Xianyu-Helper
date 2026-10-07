// support.go 提供 MCP 传输包内部共用的小型无依赖辅助函数。

package mcp

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// errGuardConfigMissing 生成守卫构造期的缺依赖错误；name 标识缺失的依赖名称，不含任何敏感配置。
func errGuardConfigMissing(name string) error {
	return fmt.Errorf("MCP 守卫缺少必需依赖: %s", name)
}

// sourceKey 返回用于限流的来源键：TCP 对端 IP 字符串；无法解析时使用固定未知键，避免空键绕过计数。
func sourceKey(r *http.Request) string {
	// ip 是请求的 TCP 来源 IP。
	ip := remoteIP(r)
	if ip == nil {
		return "unknown"
	}
	return ip.String()
}

// retryAfterSeconds 把封禁剩余时长向上取整为秒，供 Retry-After 头使用。
func retryAfterSeconds(d time.Duration) string {
	// seconds 是向上取整后的等待秒数，保证至少 1 秒。
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
