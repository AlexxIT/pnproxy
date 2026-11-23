package hosts

import (
	"encoding/json"
	"strings"
	"sync"
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
	DirectNoBody
	DirectNoHTTP
	DirectThrottled
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
	case DirectNoBody:
		return "direct_no_body"
	case DirectNoHTTP:
		return "direct_no_http"
	case DirectThrottled:
		return "direct_throttled"
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
