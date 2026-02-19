package hosts

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var httpCache = map[string]*httpCacheItem{}
var httpCacheMu sync.Mutex

type httpCacheItem struct {
	mu       sync.Mutex
	deadline time.Time
	err      error
}

func httpGetCache(host string, handler handlerFunc) error {
	item := httpCachedItem(host)
	item.mu.Lock()
	defer item.mu.Unlock()

	if now := time.Now(); now.After(item.deadline) {
		item.deadline = now.Add(time.Hour)
		item.err = handler(host)
	}

	return item.err
}

func httpCachedItem(host string) *httpCacheItem {
	httpCacheMu.Lock()
	defer httpCacheMu.Unlock()

	item, ok := httpCache[host]
	if !ok {
		item = &httpCacheItem{}
		httpCache[host] = item
	}

	return item
}

func httpRequest(rawURL string, timeout, readBody int, proxy string) (int, error) {
	client := http.Client{
		Transport: &http.Transport{
			Proxy: func(req *http.Request) (*url.URL, error) {
				if proxy == "" {
					return nil, nil
				}
				return url.Parse(proxy)
			},
			DisableCompression:  true, // important
			TLSHandshakeTimeout: 1 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: time.Duration(timeout) * time.Second,
	}

	res, err := client.Get(rawURL)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return res.StatusCode, nil
	}

	if readBody > 0 {
		_, err = io.ReadFull(res.Body, make([]byte, readBody))
		if errors.Is(err, io.ErrUnexpectedEOF) {
			err = nil
		}
	}

	return res.StatusCode, err
}
