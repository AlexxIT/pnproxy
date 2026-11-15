package hosts

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type HostChecker struct {
	// cache key is domain
	cache   map[string]*hostItem
	cacheMu sync.Mutex
	Handler func(host string) int
}

func (h *HostChecker) MarshalJSON() ([]byte, error) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	return json.Marshal(h.cache)
}

func (h *HostChecker) CheckHost(host string) int {
	item := h.getItem(host)
	item.mu.Lock()
	defer item.mu.Unlock()

	now := time.Now()
	if now.Before(item.deadline) {
		return item.status
	}

	item.deadline = now.Add(time.Hour)
	item.status = h.Handler(host)

	return item.status
}

func (h *HostChecker) getItem(host string) *hostItem {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()

	host, _ = strings.CutPrefix(host, "www.")

	// 1. Check domain
	if item, ok := h.cache[host]; ok {
		return item
	}

	// 2. Check all subdomains
	for i := len(host) - 1; i > 0; i-- {
		if host[i] == '.' {
			host2 := host[i+1:]
			if item, ok := h.cache[host2]; ok {
				return item
			}
		}
	}

	// 3. Add domain to cache
	item := &hostItem{}
	if h.cache == nil {
		h.cache = map[string]*hostItem{host: item}
	} else {
		h.cache[host] = item
	}
	return item
}

const (
	DirectOK = iota + 1
	DirectThrottled
	DirectUnknown
	DirectSNIBlocked
	ProxyOK
	ProxyStatusOK
	ProxyError
	DirectError
)

func statusName(status int) string {
	switch status {
	case DirectOK:
		return "direct_ok"
	case DirectThrottled:
		return "direct_throttled"
	case DirectUnknown:
		return "direct_unknown"
	case DirectSNIBlocked:
		return "direct_sni_blocked"
	case ProxyOK:
		return "proxy_ok"
	case ProxyStatusOK:
		return "proxy_status_ok"
	case ProxyError:
		return "proxy_error"
	case DirectError:
		return "direct_error"
	}
	return ""
}

type hostItem struct {
	mu       sync.Mutex
	deadline time.Time
	status   int
}

func (h *hostItem) MarshalJSON() ([]byte, error) {
	s := statusName(h.status)
	return json.Marshal(s)
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

func CheckURL(rawURL string, timeout time.Duration, readLimit int, proxy, sni string) (int, int, error) {
	var readBytes atomic.Int64
	var conn *countingConn
	var connMu sync.Mutex

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			connMu.Lock()
			defer connMu.Unlock()
			// This shouldn't happen, but anyway.
			if conn != nil {
				return nil, errors.New("refusing to open a second TCP connection")
			}

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
	if proxy != "" {
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			return url.Parse(proxy)
		}
	}

	// Use false SNI to check SNI block.
	if sni != "" {
		transport.TLSClientConfig = &tls.Config{
			ServerName:         sni,
			InsecureSkipVerify: true,
			NextProtos:         []string{"http/1.1"},
		}
	}

	client := http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return nil
			}
			return http.ErrUseLastResponse
		},
	}

	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var status int
	var err error

	// Call multiple GET in same TCP connection to check throttled.
	for range 3 {
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

		// Read body to check throttled.
		if readLimit > 0 {
			_, err = io.Copy(io.Discard, res.Body)
		}

		// We must close body.
		err = errors.Join(err, res.Body.Close())
		if err != nil {
			break
		}

		// Skip other than 200 answers because they have small body.
		// If res.Close flag - we can't call next request in same connection.
		if status != http.StatusOK || res.Close || readBytes.Load() > int64(readLimit) {
			break
		}
	}

	return status, int(readBytes.Load()), err
}
