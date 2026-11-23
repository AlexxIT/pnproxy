package hosts

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type Checker struct {
	ReadLimit int
	Parallel  int
	Proxy     string
	SNI       string
	Timeout   time.Duration
}

//type CheckResponse struct {
//	StatusCode    int
//	ContentLength int64
//	ReadBytes     int64
//	Close         bool
//	ElapsedTime   time.Duration
//}

func (c *Checker) Check(rawURL string) (int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	if c.Parallel <= 1 {
		return c.CheckContext(ctx, rawURL)
	}

	type result struct {
		status int
		size   int
		err    error
	}
	results := make(chan result, c.Parallel)

	for range c.Parallel {
		go func() {
			status, size, err := c.CheckContext(ctx, rawURL)
			results <- result{status, size, err}
		}()
	}

	var maxResult result

	for range c.Parallel {
		curResult := <-results
		if curResult.status > 0 {
			return curResult.status, curResult.size, curResult.err
		}
		if maxResult.size == 0 || curResult.size > maxResult.size {
			maxResult = curResult
		}
	}

	return maxResult.status, maxResult.size, maxResult.err
}

func (c *Checker) CheckContext(ctx context.Context, rawURL string) (int, int, error) {
	var readBytes atomic.Int64
	var conn *countingConn

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			// Connections can be more than one if redirect will be to subdomain.
			rawConn, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			conn = &countingConn{Conn: rawConn, readBytes: &readBytes}
			return conn, nil
		},
		DisableCompression:  true, // increase body size
		TLSHandshakeTimeout: time.Second,
		MaxConnsPerHost:     1,
	}
	defer transport.CloseIdleConnections()

	// Use proxy to check IP block.
	if c.Proxy != "" {
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			return url.Parse(c.Proxy)
		}
	}

	// Use false SNI to check SNI block.
	if c.SNI != "" {
		transport.TLSClientConfig = &tls.Config{
			ServerName:         c.SNI,
			InsecureSkipVerify: true,
			CurvePreferences:   []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
			NextProtos:         []string{"http/1.1"},
		}
	}

	client := http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow redirects to same or subdomain
			if strings.HasSuffix(req.URL.Host, via[0].URL.Host) {
				return nil
			}
			return http.ErrUseLastResponse
		},
	}

	var status int
	var err error

	// Call multiple GET in same TCP connection to check throttled.
	for {
		var req *http.Request
		if req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil); err != nil {
			break
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")

		var res *http.Response
		if res, err = client.Do(req); err != nil {
			break
		}

		// Save status from first response.
		if status == 0 {
			status = res.StatusCode
		}

		if status != http.StatusOK || c.ReadLimit == 0 {
			_ = res.Body.Close()
			break
		}

		// Read body to check throttled.
		discard := make([]byte, 8192)
		for int(readBytes.Load()) < c.ReadLimit {
			if _, err = res.Body.Read(discard); err != nil {
				if err == io.EOF {
					err = nil
				}
				break
			}
		}

		// We must close body.
		_ = res.Body.Close()
		if err != nil {
			break
		}

		// If res.Close flag - we can't call next request in same connection.
		if res.Close {
			break
		}

		// Finish if already read enought. Or if no chance to read enought.
		if n := int(readBytes.Load()); n > c.ReadLimit || n < c.ReadLimit/3 {
			break
		}
	}

	if conn == nil {
		return 0, -1, err
	}

	return status, int(readBytes.Load()), err
}

type countingConn struct {
	net.Conn
	readBytes *atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.readBytes.Add(int64(n))
	return n, err
}
