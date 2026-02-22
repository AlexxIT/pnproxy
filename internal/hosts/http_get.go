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

	if now := time.Now(); now.Before(item.deadline) {
		return 0, item.err
	} else {
		item.deadline = now.Add(time.Hour)
	}

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

// maxDomainLinks Just for saving app memory
const maxDomainLinks = 4
const maxDomainLinkLen = 128

func (h *hostItem) updateLinks(links []string) {
	for _, link := range links {
		if len(h.links) >= maxDomainLinks {
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
			// Allow only redirect to same host and subdomain:
			// https://wiki.qidi3d.com -> https://wiki.qidi3d.com/en/home
			// https://goodreads.com -> https://www.goodreads.com
			if strings.HasSuffix(req.URL.Host, via[0].URL.Host) {
				return nil
			}
			return http.ErrUseLastResponse
		},
		Timeout: time.Duration(timeout) * time.Second,
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return 0, nil, err
	}

	req.Header.Add("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")

	res, err := client.Do(req)
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

var reLink = regexp.MustCompile(`(href|src|content)="(https:/)?/[^"]+`)

func parseLinks(host string, body []byte) url.Values {
	links := url.Values{}

	// Find all links in body
	matches := reLink.FindAll(body, -1)

	// Sort links, so more important will be first
	slices.SortFunc(matches, func(a, b []byte) int {
		return linkWeight(b) - linkWeight(a)
	})

	for _, m := range matches {
		i := bytes.IndexByte(m, '"')
		link := string(m[i+1:])

		// Skip big links (save app memory)
		if len(link) > maxDomainLinkLen {
			continue
		}

		u2, err := url.Parse(link)
		if err != nil {
			continue
		}

		// Skip index pages
		if len(u2.Path) <= 1 {
			continue
		}

		if u2.Host == "" {
			if len(links[host]) < maxDomainLinks {
				u := url.URL{Scheme: "https", Host: host}
				links.Add(host, u.ResolveReference(u2).String())
			}
		} else if len(links[u2.Host]) < maxDomainLinks {
			links.Add(u2.Host, link)
		}
	}

	return links
}

func linkWeight(path []byte) int {
	for i := len(path) - 1; i > 0; i-- {
		switch path[i] {
		case '.':
			switch string(path[i+1:]) {
			case "css", "js":
				return 2
			case "jpg", "jpeg", "png":
				return 1
			}
			return 0
		case '?', '#':
			path = path[:i]
		case '/':
			return 0
		}
	}
	return 0
}
