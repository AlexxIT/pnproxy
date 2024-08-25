package dns

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type dohConn struct {
	server   string
	deadline time.Time
	pool     sync.Pool
}

func newDoHConn(server string) *dohConn {
	if net.ParseIP(server) != nil {
		server = "https://" + server + "/dns-query"
	}
	return &dohConn{server: server}
}

func (d *dohConn) Read(b []byte) (n int, err error) {
	req, ok := d.pool.Get().([]byte)
	if !ok {
		return 0, io.EOF
	}

	res, err := d.query(req)
	if err != nil {
		return 0, err
	}

	return copy(b, res), nil
}

func (d *dohConn) Write(b []byte) (n int, err error) {
	d.pool.Put(b)
	return len(b), nil
}

func (d *dohConn) Close() error {
	return nil
}

func (d *dohConn) LocalAddr() net.Addr {
	return nil
}

func (d *dohConn) RemoteAddr() net.Addr {
	return nil
}

func (d *dohConn) SetDeadline(t time.Time) error {
	d.deadline = t
	return nil
}

func (d *dohConn) SetReadDeadline(t time.Time) error {
	panic("not implemented")
}

func (d *dohConn) SetWriteDeadline(t time.Time) error {
	panic("not implemented")
}

func (d *dohConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	panic("not implemented")
}

func (d *dohConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	panic("not implemented")
}

func (d *dohConn) query(b []byte) ([]byte, error) {
	deadline := d.deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(5 * time.Second)
	}

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	// https://datatracker.ietf.org/doc/html/rfc8484
	req, err := http.NewRequestWithContext(ctx, "POST", d.server, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("Content-Type", "application/dns-message")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(res.Body)
}
