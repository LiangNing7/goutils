package ip

import (
	"net"
	"net/http"
	"strings"
)

// Define http headers.
const (
	XForwardedFor = "X-Forwarded-For"
	XRealIP       = "X-Real-IP"
	XClientIP     = "x-client-ip"
)

// GetLocalIP returns the non loopback local IP of the host.
// 返回主机的非回环(localhost)本地IPv4地址.
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it.
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

// RemoteIP returns the remote ip of the request.
// 依次从"X-Client-Ip、X-Real-IP、X-Forwarded-For" HTTP 头中获取远程客户端 IP，
// 都没有时回退到 req.RemoteAddr。
//
// 顺序策略与理由：
//   - X-Client-Ip / X-Real-IP 通常由可信边缘代理设置，直接取用；
//   - X-Forwarded-For 常见形式为 "client, proxy1, proxy2"（代理逐跳追加左端为最原始客户端）。
//     本函数取「最左侧」的元素，即最接近真实客户端的地址。
//     注意：最左侧的值可被客户端伪造，若部署在多级代理后且需要权威来源，
//     应由可信代理改写 X-Real-IP / X-Client-Ip，或在此处改用「最右侧」的可信跳。
//
// 容错：
//   - RemoteAddr 无端口时不再返回空串，而是按裸 IP 处理；
//   - 支持 "[::1]:1234"、"2001:db8::1"、"::1" 等 IPv6 写法；
//   - 回环地址 "::1" 归一化为 "127.0.0.1".
func RemoteIP(req *http.Request) string {
	switch {
	case req.Header.Get(XClientIP) != "":
		return normalizeIP(req.Header.Get(XClientIP))
	case req.Header.Get(XRealIP) != "":
		return normalizeIP(req.Header.Get(XRealIP))
	case req.Header.Get(XForwardedFor) != "":
		return normalizeIP(firstForwardedFor(req.Header.Get(XForwardedFor)))
	default:
		return normalizeIP(hostFromRemoteAddr(req.RemoteAddr))
	}
}

// firstForwardedFor 返回 X-Forwarded-For 中最左侧（最接近原始客户端）的地址.
func firstForwardedFor(xff string) string {
	if i := strings.IndexByte(xff, ','); i >= 0 {
		return strings.TrimSpace(xff[:i])
	}
	return strings.TrimSpace(xff)
}

// hostFromRemoteAddr 从容错的 remoteAddr 中解析出主机部分.
// 对无端口的裸 IP（尤其是裸 IPv6）不返回空串.
func hostFromRemoteAddr(addr string) string {
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	// 没有端口：可能是裸 IPv4 或裸 IPv6（"::1"），也可能带了方括号 "[::1]".
	return strings.Trim(addr, "[]")
}

// normalizeIP 去除可能的端口与方括号，并把 IPv6 回环归一化为 127.0.0.1.
func normalizeIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	} else {
		s = strings.Trim(s, "[]")
	}
	if s == "::1" {
		s = "127.0.0.1"
	}
	return s
}
