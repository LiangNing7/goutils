package controllermanager

import "testing"

// TestAppendPortIfNeeded 锁定 AppendPortIfNeeded 的行为（纯函数，无外部状态）.
func TestAppendPortIfNeeded(t *testing.T) {
	cases := []struct {
		name string
		addr string
		port int32
		want string
	}{
		{"ipv4-bare", "1.2.3.4", 80, "1.2.3.4:80"},
		{"ipv4-with-port", "1.2.3.4:8080", 80, "1.2.3.4:8080"},
		{"ipv6-bare", "::1", 80, "[::1]:80"},
		{"ipv6-with-port", "[::1]:8080", 80, "[::1]:8080"},
		{"ipv6-global-bare", "2001:db8::1", 443, "[2001:db8::1]:443"},
		{"invalid", "not-an-ip", 80, "not-an-ip"},
		{"empty", "", 80, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AppendPortIfNeeded(c.addr, c.port); got != c.want {
				t.Fatalf("AppendPortIfNeeded(%q, %d) = %q, want %q", c.addr, c.port, got, c.want)
			}
		})
	}
}
