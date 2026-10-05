// network.go 提供 /mcp 端点的来源网络判定：默认只允许本机 loopback 访问，
// 且永远不相信 X-Forwarded-For 等可伪造代理头。

package mcp

import (
	"net"
	"net/http"
)

// remoteIP 从请求的 TCP 对端地址解析来源 IP；解析失败时返回 nil。
// 该函数刻意不读取 X-Forwarded-For 与 X-Real-IP，避免客户端通过请求头伪造来源。
func remoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	// host 是去掉端口后的对端主机；RemoteAddr 形如 127.0.0.1:52344。
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// 无端口时整体作为主机地址再解析一次。
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// isLoopbackRequest 判断请求是否来自本机 loopback 地址；
// 地址非法（含空 RemoteAddr）一律视为非 loopback，保持默认拒绝。
func isLoopbackRequest(r *http.Request) bool {
	// ip 是请求的 TCP 来源 IP。
	ip := remoteIP(r)
	return ip != nil && ip.IsLoopback()
}
