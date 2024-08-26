package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	ihttp "github.com/AlexxIT/pnproxy/internal/http"
	"github.com/AlexxIT/pnproxy/internal/tls"
)

func apiRequest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	req, err := http.NewRequest(query.Get("method"), query.Get("url"), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var result struct {
		Addrs      []string          `json:"dns_address"`
		URL        string            `json:"url"`
		Proto      string            `json:"proto"`
		StatusCode int               `json:"status_code"`
		Headers    map[string]string `json:"headers"`
	}

	var res *http.Response

	switch req.URL.Scheme {
	case "http":
		rec := httptest.NewRecorder()
		ihttp.Handle(rec, req)
		res = rec.Result()
		res.Request = req
	case "https":
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, _ := net.SplitHostPort(addr)
				result.Addrs, _ = net.LookupHost(host)

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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = res.Body.Close()
	default:
		http.Error(w, "wrong scheme", http.StatusBadRequest)
		return
	}

	result.URL = res.Request.URL.String()
	result.Proto = res.Proto
	result.StatusCode = res.StatusCode

	result.Headers = make(map[string]string)
	for k, v := range res.Header {
		result.Headers[k] = v[0]
	}

	w.Header().Add("Content-Type", "application/json")
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	_ = e.Encode(result)
}
