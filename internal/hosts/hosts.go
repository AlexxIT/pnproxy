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
	directChecker := Checker{
		ReadLimit: atoi(params.Get("read_size")),
		Parallel:  atoi(params.Get("parallel")),
		Timeout:   pkg.ParseTimeout(params.Get("timeout"), 5*time.Second),
	}

	proxyChecker := Checker{
		Proxy:   params.Get("proxy"),
		Timeout: pkg.ParseTimeout(params.Get("proxy_timeout"), directChecker.Timeout),
	}

	sniChecker := Checker{
		SNI:     params.Get("sni"),
		Timeout: pkg.ParseTimeout(params.Get("sni_timeout"), directChecker.Timeout),
	}

	statusChecker := Checker{
		Proxy:   params.Get("status_proxy"),
		Timeout: pkg.ParseTimeout(params.Get("status_timeout"), directChecker.Timeout),
	}

	statusCodes := map[int]bool{}
	for _, s := range params["status_code"] {
		if i, _ := strconv.Atoi(s); i > 0 {
			statusCodes[i] = true
		}
	}
	if statusChecker.Proxy == "" && proxyChecker.Proxy != "" {
		statusChecker.Proxy = proxyChecker.Proxy
	}

	return func(host string) int {
		rawURL := "https://" + host

		statusCode, size, err := directChecker.Check(rawURL)
		if statusCode > 0 {
			// Check if proxy status code better than direct status.
			if statusChecker.Proxy != "" && statusCodes[statusCode] {
				proxyCode, _, _ := statusChecker.Check(rawURL)
				if proxyCode > 0 && !statusCodes[proxyCode] {
					return ProxyStatusOK
				}
			}

			if size >= directChecker.ReadLimit {
				return DirectOK
			} else if errors.Is(err, context.DeadlineExceeded) {
				return DirectThrottled
			} else {
				return DirectNoBody
			}
		} else if size > 0 {
			return DirectNoHTTP
		}

		if size == 0 && sniChecker.SNI != "" {
			// One second will be enought.
			_, size, err = sniChecker.Check(rawURL)
			// Some sites can close connection before TLS handshake (it is good).
			if size > 0 || errors.Is(err, io.EOF) {
				return DirectSNIBlocked
			}
		}

		if size <= 0 && proxyChecker.Proxy != "" {
			statusCode, _, _ = proxyChecker.Check(rawURL)
			if statusCode > 0 {
				return ProxyOK
			} else {
				return ProxyError
			}
		}

		return DirectError
	}
}

func atoi(str string) int {
	i, _ := strconv.Atoi(str)
	return i
}

func Hosts() any {
	return httpGet
}
