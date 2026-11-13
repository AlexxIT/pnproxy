// Package byedpi provides a Linux TCP desynchronization wrapper based on
// ByeDPI's fake-packet and TCP MD5 signature technique.
package byedpi

import (
	"errors"
	"io"
	"net"
	"sync"
)

// ErrUnsupported reports that the kernel or socket does not support a required
// operation, or that the TCP_INFO fields needed by the algorithm are unavailable.
var ErrUnsupported = errors.New("byedpi: unsupported kernel or platform")

// DesyncConn wraps a TCP connection and serializes writes. The first non-empty
// Write uses the fake/MD5 technique; later writes use the underlying TCPConn.
// If that first write fails, the error is retained and returned by later writes.
// Use one wrapper per connection and do not write through TCPConn directly while
// the wrapper is in use.
type DesyncConn struct {
	*net.TCPConn
	mu      sync.Mutex
	started bool
	failure error
}

// NewDesyncConn creates a wrapper that applies desynchronization to the first
// non-empty write on c.
func NewDesyncConn(c *net.TCPConn) *DesyncConn { return &DesyncConn{TCPConn: c} }

func (c *DesyncConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != nil {
		return 0, c.failure
	}
	if len(p) == 0 {
		return 0, nil
	}
	if c.started {
		return c.TCPConn.Write(p)
	}
	c.started = true
	n, err := WriteFakeMD5(c.TCPConn, p)
	c.failure = err
	return n, err
}

// ReadFrom prevents the embedded TCPConn fast path from bypassing Write.
func (c *DesyncConn) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{c}, r)
}
