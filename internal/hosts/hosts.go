package hosts

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/AlexxIT/pnproxy/internal/app"
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
			httpRule = name
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
		status, err := httpGet.Check(host)
		if err != nil {
			if status > 0 {
				log.Info().Msgf("[hosts] http_get host=%s error=%v", host, err)
			}
			return httpRule
		}
	}

	return ""
}

var static = map[string]string{}
var httpRule string
var httpGet *HostChecker

const (
	// StatusOK Means that direct access to the resource definitely exists.
	StatusOK = iota + 1
	// StatusMaybeOK Means that direct access is available, but we are not sure about it.
	StatusMaybeOK
	// StatusProxyOK Means that access via a proxy works better than direct access.
	StatusProxyOK
	// StatusMaybeError Means that there are problems with direct access, and we are not sure if a proxy will help.
	StatusMaybeError
	// StatusError Means that there is definitely an issue with direct access and access via proxy.
	StatusError
)

func handleHttpGet(params url.Values) func(host string) (int, []byte, error) {
	timeout, _ := strconv.Atoi(params.Get("timeout"))
	readBody, _ := strconv.Atoi(params.Get("read_body"))
	proxy := params.Get("proxy")
	status := map[int]bool{}
	for _, s := range params["status"] {
		if i, _ := strconv.Atoi(s); i > 0 {
			status[i] = true
		}
	}

	if proxy == "" {
		return func(rawURL string) (int, []byte, error) {
			status1, body1, err1 := httpRequest(rawURL, timeout, readBody, "")
			if err1 != nil {
				return StatusMaybeError, body1, unwrapError(err1)
			}

			if status[status1] {
				return StatusMaybeError, body1, fmt.Errorf("http_get: wrong status %d", status1)
			}

			if readBody > 0 && len(body1) < readBody {
				return StatusMaybeOK, body1, nil
			}

			return StatusOK, body1, nil
		}
	}

	return func(rawURL string) (int, []byte, error) {
		status1, body1, err1 := httpRequest(rawURL, timeout, readBody, "")

		if err1 != nil {
			_, body2, err2 := httpRequest(rawURL, timeout, readBody, proxy)
			if err2 != nil {
				// if both direct and proxy has errors - don't use proxy
				return StatusError, body2, err2
			}

			// don't need to check proxy body size
			return StatusProxyOK, body2, unwrapError(err1)
		}

		if status[status1] {
			status2, body2, err2 := httpRequest(rawURL, timeout, readBody, proxy)
			if err2 != nil {
				return StatusMaybeOK, body2, nil
			}

			if status1 == status2 {
				// if both direct and proxy has problem status - don't use proxy
				return StatusError, body2, nil
			}

			return StatusProxyOK, body2, fmt.Errorf("http_get: wrong status %d", status1)
		}

		if readBody > 0 && len(body1) < readBody {
			return StatusMaybeOK, body1, nil
		}

		return StatusOK, body1, nil
	}
}

func unwrapError(err error) error {
	if err2 := errors.Unwrap(err); err2 != nil {
		return err2
	}
	return err
}

func Hosts() any {
	return httpGet
}
