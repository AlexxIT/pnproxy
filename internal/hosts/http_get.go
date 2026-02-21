package hosts

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type HostChecker struct {
	// cache key is domain
	cache   map[string]*hostItem
	cacheMu sync.Mutex
	Handler func(rawURL string) (status int, body []byte, err error)
}

func (h *HostChecker) MarshalJSON() ([]byte, error) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	return json.Marshal(h.cache)
}

func (h *HostChecker) Check(host string) (int, error) {
	item := h.getCache(host)
	item.mu.Lock()
	defer item.mu.Unlock()

	now := time.Now()

	if now.Before(item.deadline) {
		return 0, item.err
	}

	item.deadline = now.Add(time.Hour)

	index := "https://" + host

	if !slices.Contains(item.links, index) {
		item.links = append(item.links, index)
	} else {
		index = ""
	}

	// Check links to pages of this domain if we know them
	for i := 0; i < len(item.links); i++ {
		link := item.links[i]
		status, body, err := h.Handler(link)

		if link == index {
			// Save all CSS and JS links from main page head
			for host2, links2 := range parseLinks(host, body) {
				if host == host2 {
					item.updateLinks(links2)
				} else {
					go h.updateLinks(host2, links2)
				}
			}
		}

		switch status {
		case StatusOK, StatusError, StatusMaybeOK:
			item.err = nil
		case StatusProxyOK, StatusMaybeError:
			item.err = err
		}

		if status == StatusMaybeOK {
			continue
		}

		if i > 0 {
			// move known problem link to the first place
			item.links[i] = item.links[0]
			item.links[0] = link
		}

		return status, item.err
	}

	return StatusMaybeOK, item.err
}

func (h *HostChecker) getCache(host string) *hostItem {
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

func (h *HostChecker) updateLinks(host string, links []string) {
	item := h.getCache(host)
	item.mu.Lock()
	item.updateLinks(links)
	item.mu.Unlock()
}

type hostItem struct {
	mu       sync.Mutex
	deadline time.Time
	links    []string
	err      error
}

func (h *hostItem) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"links": h.links,
	}
	if h.err != nil {
		m["error"] = h.err.Error()
	}
	return json.Marshal(m)
}

func (h *hostItem) updateLinks(links []string) {
	for _, link := range links {
		// Just for saving memory
		if len(h.links) >= 5 {
			break
		}
		if !slices.Contains(h.links, link) {
			h.links = append(h.links, link)
		}
	}
}

// httpRequest return http_status, http_body, error
func httpRequest(rawURL string, timeout, readBody int, proxy string) (int, []byte, error) {
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
		return 0, nil, err
	}
	defer res.Body.Close()

	if readBody == 0 {
		return res.StatusCode, nil, nil
	}

	buf := make([]byte, readBody)
	var n, nn int
	for n < readBody {
		nn, err = res.Body.Read(buf[n:])
		n += nn
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
	}
	return res.StatusCode, buf[:n], err
}

var reTag = regexp.MustCompile(`<(link|script).+?>`)
var reRef = regexp.MustCompile(`href="([^"]+)`)
var reSrc = regexp.MustCompile(`src="([^"]+)`)

func parseLinks(host string, body []byte) url.Values {
	body, _ = bytes.CutSuffix(body, []byte("<body"))

	links := url.Values{}

	for _, m := range reTag.FindAllSubmatch(body, -1) {
		switch string(m[1]) {
		case "link":
			if !bytes.Contains(m[0], []byte(`rel="stylesheet"`)) {
				continue
			}
			m = reRef.FindSubmatch(m[0])
		case "script":
			m = reSrc.FindSubmatch(m[0])
		}

		if m == nil {
			continue
		}

		link := string(m[1])

		u2, err := url.Parse(link)
		if err != nil {
			continue
		}

		if u2.Host == "" {
			u := url.URL{Scheme: "https", Host: host}
			links.Add(host, u.ResolveReference(u2).String())
		} else {
			links.Add(u2.Host, link)
		}
	}

	return links
}
