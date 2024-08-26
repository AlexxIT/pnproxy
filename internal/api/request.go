package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	ihttp "github.com/AlexxIT/pnproxy/internal/http"
	"github.com/AlexxIT/pnproxy/internal/tls"
)

func apiRequest(w http.ResponseWriter, r *http.Request) {
	urls := r.URL.Query()["url"]

	var wg sync.WaitGroup
	wg.Add(len(urls))

	results := make([]any, len(urls))

	for i, url := range urls {
		go func(i int, url string) {
			results[i] = request(url)
			wg.Done()
		}(i, url)
	}

	wg.Wait()

	w.Header().Add("Content-Type", "application/json")
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	_ = e.Encode(results)
}

func request(url string) any {
	if strings.Index(url, "://") < 0 {
		url = "https://" + url
	}

	result := &struct {
		URL        string   `json:"url"`
		Addrs      []string `json:"dns_address,omitempty"`
		Proto      string   `json:"proto,omitempty"`
		StatusCode int      `json:"status_code,omitempty"`
		Location   string   `json:"location,omitempty"`
		Error      string   `json:"error,omitempty"`
	}{
		URL: url,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Addrs, _ = net.LookupHost(req.URL.Host)

	var res *http.Response

	switch req.URL.Scheme {
	case "http":
		rec := httptest.NewRecorder()
		ihttp.Handle(rec, req)
		res = rec.Result()
	case "https":
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn1, conn2 := net.Pipe()
				go tls.Handle(conn2)
				return conn1, nil
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		res, err = transport.RoundTrip(req)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		_ = res.Body.Close()
	}

	if res != nil {
		result.URL = res.Request.URL.String()
		result.Proto = res.Proto
		result.StatusCode = res.StatusCode
		if location := res.Header.Get("Location"); location != "" {
			result.Location = location
		}
	}

	return result
}
