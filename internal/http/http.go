package http

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/dns"
	"github.com/AlexxIT/pnproxy/internal/hosts"
	"github.com/rs/zerolog/log"
)

func Init() {
	var cfg struct {
		HTTP struct {
			Listen string `yaml:"listen"`
			Rules  []struct {
				Name   string `yaml:"name"`
				Action string `yaml:"action"`
			}
			Default struct {
				Action string `yaml:"action"`
			} `yaml:"default"`
		} `yaml:"http"`
	}

	app.LoadConfig(&cfg)

	for _, rule := range cfg.HTTP.Rules {
		handler := parseAction(rule.Action)
		if handler == nil {
			log.Warn().Msgf("[http] wrong action: %s", rule.Action)
			continue
		}

		for _, name := range hosts.Get(rule.Name) {
			handlers["."+name] = handler
		}
	}

	defaultHandler = parseAction(cfg.HTTP.Default.Action)

	if cfg.HTTP.Listen != "" {
		go serve(cfg.HTTP.Listen)
	}
}

func Handle(w http.ResponseWriter, r *http.Request) {
	domain := r.Host
	if i := strings.IndexByte(r.Host, ':'); i > 0 {
		domain = domain[:i]
	}

	handler := findHandler(domain)
	if handler == nil {
		log.Trace().Msgf("[http] skip remote_addr=%s domain=%s", r.RemoteAddr, domain)
		return
	}

	log.Trace().Msgf("[http] open remote_addr=%s domain=%s", r.RemoteAddr, domain)

	handler(w, r)
}

var handlers = map[string]http.HandlerFunc{}
var defaultHandler http.HandlerFunc

func findHandler(domain string) http.HandlerFunc {
	domain = "." + domain
	for k, handler := range handlers {
		if strings.HasSuffix(domain, k) {
			return handler
		}
	}
	return defaultHandler
}

func serve(address string) {
	log.Info().Msgf("[http] listen=%s", address)
	srv := &http.Server{
		Addr:    address,
		Handler: http.HandlerFunc(Handle),
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

func parseAction(raw string) http.HandlerFunc {
	if raw != "" {
		action, params := app.ParseAction(raw)
		switch action {
		case "redirect":
			return handleRedirect(params)
		case "raw_pass":
			return handleRaw(params)
		case "proxy_pass":
			return handleProxy(params)
		default:
		}
	}
	return nil
}

func handleRedirect(params url.Values) http.HandlerFunc {
	code := http.StatusTemporaryRedirect
	if params.Has("code") {
		code, _ = strconv.Atoi(params.Get("code"))
	}
	scheme := params.Get("scheme")

	return func(w http.ResponseWriter, r *http.Request) {
		if scheme != "" {
			r.URL.Scheme = scheme
		}
		w.Header().Add("Location", r.URL.String())
		w.WriteHeader(code)
	}
}

func handleTransport(transport http.RoundTripper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, suffix := r.URL.Host, ""
		if i := strings.IndexByte(domain, ':'); i > 0 {
			domain, suffix = domain[:i], domain[i:]
		}

		host, err := dns.Resolve(domain)
		if err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}

		r.Header.Set("Host", r.Host)
		r.URL.Host = host + suffix

		res, err := transport.RoundTrip(r)
		if err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}
		defer res.Body.Close()

		header := w.Header()
		for k, vv := range res.Header {
			for _, v := range vv {
				header.Add(k, v)
			}
		}

		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}
}

func handleRaw(params url.Values) http.HandlerFunc {
	return handleTransport(http.DefaultTransport)
}

func handleProxy(params url.Values) http.HandlerFunc {
	if !params.Has("host") {
		return nil
	}

	proxyURL := &url.URL{Host: params.Get("host")}
	if params.Has("type") {
		proxyURL.Scheme = params.Get("type")
	} else {
		proxyURL.Scheme = "http"
	}
	if params.Has("port") {
		proxyURL.Host += ":" + params.Get("port")
	}
	if params.Has("username") {
		if params.Has("password") {
			proxyURL.User = url.UserPassword(params.Get("username"), params.Get("password"))
		} else {
			proxyURL.User = url.User(params.Get("username"))
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)

	return handleTransport(transport)
}
