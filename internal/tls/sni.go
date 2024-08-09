package tls

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
)

func parseSNI(hello []byte) (sni string) {
	_ = tls.Server(connSNI{r: bytes.NewReader(hello)}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName
			return nil, nil
		},
	}).Handshake()
	return
}

type connSNI struct {
	r io.Reader
	net.Conn
}

func (c connSNI) Read(p []byte) (int, error) { return c.r.Read(p) }
func (connSNI) Write(p []byte) (int, error)  { return 0, io.EOF }
