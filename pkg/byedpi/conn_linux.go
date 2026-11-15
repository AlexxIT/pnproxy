//go:build linux

package byedpi

import (
	"io"
	"net"
	"sync"
)

// desyncConn wraps a TCP connection and desynchronizes its first non-empty
// write. Later writes pass through to the underlying connection.
// --fake -1 --md5sig
type desyncConn struct {
	*net.TCPConn
	mu      sync.Mutex
	started bool
	failure error
}

// NewDesyncConn wraps c with the Linux desynchronization strategy.
func NewDesyncConn(c *net.TCPConn) net.Conn { return &desyncConn{TCPConn: c} }

func (c *desyncConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.failure != nil {
		return 0, c.failure
	}
	if c.started {
		return c.TCPConn.Write(p)
	}

	c.started = true
	n, err := desyncFirst(c.TCPConn, p)
	c.failure = err
	return n, err
}

// ReadFrom prevents the embedded TCPConn fast path from bypassing Write.
func (c *desyncConn) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{c}, r)
}
