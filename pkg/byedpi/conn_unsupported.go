//go:build !linux && !windows

package byedpi

import (
	"net"
)

func NewDesyncConn(c *net.TCPConn) net.Conn { return nil }
