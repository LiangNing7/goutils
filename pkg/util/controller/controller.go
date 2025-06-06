package controllermanager

import (
	"fmt"
	"net"

	netutils "k8s.io/utils/net"
)

// AppendPortIfNeeded appends the given port to IP address unless it is already in
// "ipv4:port" or "[ipv6]:port" format.
func AppendPortIfNeeded(addr string, port int32) string {
	// Return if address is already in "ipv4:port" or "[ipv6]:port" format.
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}

	// Simply return for invalid case. This should be caught by validation instead.
	ip := netutils.ParseIPSloppy(addr)
	if ip == nil {
		return addr
	}

	// Append port to address.
	if ip.To4() != nil {
		return fmt.Sprintf("%s:%d", addr, port)
	}
	return fmt.Sprintf("[%s]:%d", addr, port)
}
