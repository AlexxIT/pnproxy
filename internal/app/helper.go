package app

import (
	"net/url"
	"strings"
)

func ParseProxyParam(params url.Values) {
	proxyURL, err := url.Parse(params.Get("proxy"))
	if err != nil || proxyURL.Host == "" {
		return
	}

	scheme := strings.ToLower(proxyURL.Scheme)
	if scheme != "http" && scheme != "socks5" {
		return
	}

	params.Set("host", proxyURL.Hostname())
	port := proxyURL.Port()
	if port == "" {
		if scheme == "socks5" {
			port = "1080"
		} else {
			port = "80"
		}
	}
	params.Set("port", port)
	params.Set("type", scheme)

	if proxyURL.User != nil {
		params.Set("username", proxyURL.User.Username())
		if password, ok := proxyURL.User.Password(); ok {
			params.Set("password", password)
		}
	}
}
