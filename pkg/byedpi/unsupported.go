//go:build !linux

package byedpi

import "net"

func WriteFakeMD5(c *net.TCPConn, p []byte) (int, error) { return 0, ErrUnsupported }
