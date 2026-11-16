package hosts

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/pkg"
	"github.com/rs/zerolog/log"
)

func Init() {
	var cfg struct {
		Hosts map[string]string `yaml:"hosts"`
	}

	app.LoadConfig(&cfg)

	for name, items := range cfg.Hosts {
		fields, params := app.ParseAction(items)
		switch fields[0] {
		case "domain":
			panic("TODO")
		case "subdomain":
			panic("TODO")
		case "address":
			panic("TODO")
		case "http_get":
			httpGet = &HostChecker{Handler: handleHttpGet(params)}
		default:
			for _, host := range fields {
				static[host] = name
			}
		}
	}
}

func Resolve(host string) string {
	host, _ = strings.CutSuffix(host, ".")

	// 1. Check static domains
	if name, ok := static[host]; ok {
		return name
	}

	// 2. Check static subdomains
	for i := len(host) - 1; i > 0; i-- {
		if host[i] == '.' {
			if name, ok := static[host[i+1:]]; ok {
				return name
			}
		}
	}

	// 2. http get
	if httpGet != nil {
		name := statusName(httpGet.CheckHost(host))
		log.Trace().Msgf("[hosts] http_get host=%s status=%s", host, name)
		return name
	}

	return ""
}

var static = map[string]string{}
var httpGet *HostChecker

func handleHttpGet(params url.Values) func(host string) int {
	timeout := pkg.ParseTimeout(params.Get("timeout"), 5*time.Second)
	readLimit, _ := strconv.Atoi(params.Get("read_size"))

	proxy := params.Get("proxy")
	proxyTimeout := pkg.ParseTimeout(params.Get("proxy_timeout"), timeout)

	sni := params.Get("sni")
	sniTimeout := pkg.ParseTimeout(params.Get("sni_timeout"), timeout)

	statusProxy := params.Get("status_proxy")
	statusTimeout := pkg.ParseTimeout(params.Get("status_timeout"), timeout)
	statusCodes := map[int]bool{}
	for _, s := range params["status_code"] {
		if i, _ := strconv.Atoi(s); i > 0 {
			statusCodes[i] = true
		}
	}
	if statusProxy == "" && proxy != "" {
		statusProxy = proxy
	}

	return func(host string) int {
		rawURL := "https://" + host

		statusCode, size, err := CheckURL(rawURL, timeout, readLimit, "", "")
		if statusCode > 0 {
			// Check if proxy status code better than direct status.
			if statusProxy != "" && statusCodes[statusCode] {
				proxyCode, _, _ := CheckURL(rawURL, statusTimeout, 0, statusProxy, "")
				if proxyCode > 0 && !statusCodes[proxyCode] {
					return ProxyStatusOK
				}
			}

			if size >= readLimit {
				return DirectOK
			} else if errors.Is(err, context.DeadlineExceeded) {
				return DirectThrottled
			} else {
				return DirectNoBody
			}
		} else if size > 0 {
			return DirectHTTPFailed
		}

		if size == 0 && sni != "" {
			// One second will be enought.
			_, size, err = CheckURL(rawURL, sniTimeout, 0, "", sni)
			// Some sites can close connection before TLS handshake (it is good).
			if size > 0 || errors.Is(err, io.EOF) {
				return DirectSNIBlocked
			}
		}

		if size <= 0 && proxy != "" {
			statusCode, _, _ = CheckURL(rawURL, proxyTimeout, 0, proxy, "")
			if statusCode > 0 {
				return ProxyOK
			} else {
				return ProxyError
			}
		}

		return DirectError
	}
}

func Hosts() any {
	return httpGet
}
