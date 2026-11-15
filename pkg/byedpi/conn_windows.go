//go:build windows

package byedpi

import (
	"io"
	"net"
	"sync"
)

// desyncConn buffers the initial TLS ClientHello so Windows can split it at
// the SNI offset. Later writes pass through to the underlying connection.
// --split 1 --disorder 3+s
type desyncConn struct {
	*net.TCPConn
	mu      sync.Mutex
	started bool
	failure error
	buffer  []byte
}

// NewDesyncConn wraps c with the Windows split/disorder strategy.
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

	c.buffer = append(c.buffer, p...)
	if c.buffer[0] != 0x16 {
		return c.fail(ErrNotClientHello)
	}
	if len(c.buffer) < 5 {
		return len(p), nil
	}
	recordLength := int(c.buffer[3])<<8 | int(c.buffer[4])
	if recordLength < 4 {
		return c.fail(errMalformedClientHello)
	}
	if len(c.buffer) < 6 {
		return len(p), nil
	}
	if c.buffer[5] != tlsClientHello {
		return c.fail(ErrNotClientHello)
	}
	if len(c.buffer) < tlsRecordHeaderSize+recordLength {
		return len(p), nil
	}

	c.started = true
	if _, err := desyncFirst(c.TCPConn, c.buffer); err != nil {
		return c.fail(err)
	}
	c.buffer = nil
	return len(p), nil
}

func (c *desyncConn) fail(err error) (int, error) {
	c.failure = err
	c.buffer = nil
	_ = c.TCPConn.Close()
	return 0, err
}

// ReadFrom prevents the embedded TCPConn fast path from bypassing Write.
func (c *desyncConn) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{c}, r)
}
