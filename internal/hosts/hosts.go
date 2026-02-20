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
			httpHandler = handleHttpGet(params)
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
	if httpHandler != nil {
		if err := httpHandler(host); err != nil {
			return httpRule
		}
	}

	return ""
}

var static = map[string]string{}
var httpRule string
var httpHandler handlerFunc

type handlerFunc func(host string) error

func handleHttpGet(params url.Values) func(host string) error {
	timeout, _ := strconv.Atoi(params.Get("timeout"))
	readBody, _ := strconv.Atoi(params.Get("read_body"))
	proxy := params.Get("proxy")
	status := map[int]bool{}
	for _, s := range params["status"] {
		if i, _ := strconv.Atoi(s); i > 0 {
			status[i] = true
		}
	}

	handler1 := func(host string) error {
		rawURL := "https://" + host

		// 1. Check direct request
		status1, err1 := httpRequest(rawURL, timeout, readBody, "")

		if proxy != "" {
			if err1 != nil {
				_, err2 := httpRequest(rawURL, timeout, readBody, proxy)
				if err2 != nil {
					// if both direct and proxy has errors - don't use proxy
					return nil
				}
				return err1
			}

			if status[status1] {
				status2, err2 := httpRequest(rawURL, timeout, readBody, proxy)
				if err2 != nil || status1 == status2 {
					// if both direct and proxy has problem status - don't use proxy
					return nil
				}
				return fmt.Errorf("http_get: wrong status %d", status1)
			}
		}

		if err1 != nil {
			return err1
		}

		if status[status1] {
			return fmt.Errorf("http_get: wrong status %d", status1)
		}

		return nil
	}

	handler2 := func(host string) error {
		err := handler1(host)
		if err != nil {
			if err2 := errors.Unwrap(err); err2 != nil {
				err = err2
			}
			// net/http: TLS handshake timeout
			// context deadline exceeded (Client.Timeout or context cancellation while reading body)
			log.Info().Msgf("[hosts] http_get host=%s error=%s", host, err)
		}
		return err
	}

	return func(host string) error {
		return httpGetCache(host, handler2)
	}
}
