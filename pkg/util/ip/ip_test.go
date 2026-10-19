package ip

import (
	"net/http"
	"testing"
)

// TestRemoteIP 覆盖 RemoteAddr 含/不含端口、IPv6 写法，以及 XFF 多跳透传等真实场景.
func TestRemoteIP(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{"remoteaddr-with-port", "1.2.3.4:5678", nil, "1.2.3.4"},
		{"remoteaddr-without-port", "1.2.3.4", nil, "1.2.3.4"},
		{"remoteaddr-ipv6-with-port", "[::1]:1234", nil, "127.0.0.1"},
		{"remoteaddr-ipv6-bracket", "[2001:db8::1]:443", nil, "2001:db8::1"},
		{"remoteaddr-bare-ipv6", "::1", nil, "127.0.0.1"},
		{"remoteaddr-empty", "", nil, ""},
		{"xff-single", "9.8.7.6:1", map[string]string{XForwardedFor: "203.0.113.1"}, "203.0.113.1"},
		// XFF 多跳：取最左侧（原始客户端），而不是整串透传.
		{
			"xff-multi-hop",
			"9.8.7.6:1",
			map[string]string{XForwardedFor: "203.0.113.1, 70.41.3.18, 150.172.238.178"},
			"203.0.113.1",
		},
		{"real-ip", "9.8.7.6:1", map[string]string{XRealIP: "198.51.100.7"}, "198.51.100.7"},
		{"client-ip-priority", "9.8.7.6:1", map[string]string{XClientIP: "10.0.0.1", XRealIP: "198.51.100.7", XForwardedFor: "203.0.113.1"}, "10.0.0.1"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &http.Request{RemoteAddr: c.remoteAddr, Header: http.Header{}}
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			if got := RemoteIP(req); got != c.want {
				t.Fatalf("RemoteIP(addr=%q, headers=%v) = %q, want %q", c.remoteAddr, c.headers, got, c.want)
			}
		})
	}
}
