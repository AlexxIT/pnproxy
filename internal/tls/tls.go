package tls

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/hosts"
	"github.com/AlexxIT/pnproxy/pkg"
	"github.com/AlexxIT/pnproxy/pkg/byedpi"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"
)

func Init() {
	var cfg struct {
		TLS struct {
			Listen string `yaml:"listen"`
			Rules  []struct {
				Name   string `yaml:"name"`
				Action any    `yaml:"action"`
			}
			Default struct {
				Action any `yaml:"action"`
			} `yaml:"default"`
		} `yaml:"tls"`
	}

	cfg.TLS.Default.Action = "raw_pass"

	app.LoadConfig(&cfg)

	for _, rule := range cfg.TLS.Rules {
		handler := parseAction(rule.Action)
		if handler == nil {
			log.Warn().Msgf("[tls] wrong action: %s", rule.Action)
			continue
		}

		for _, name := range strings.Fields(rule.Name) {
			handlers[name] = handler
		}
	}

	defaultHandler = parseAction(cfg.TLS.Default.Action)

	if cfg.TLS.Listen != "" {
		go serve(cfg.TLS.Listen)
	}
}

func Handle(src net.Conn) {
	//noinspection GoUnhandledErrorResult
	defer src.Close()

	remote := src.RemoteAddr().String()

	_ = src.SetReadDeadline(time.Now().Add(5 * time.Second))

	hello, err := readClientHello(src)
	if err != nil {
		log.Warn().Err(err).Caller().Send()
		return
	}

	_ = src.SetReadDeadline(time.Time{})

	domain := parseSNI(hello)
	if domain == "" {
		log.Warn().Msgf("[tls] skip empty domain remote_addr=%s", remote)
		return
	}

	handler, rule := findHandler(domain)
	if handler == nil {
		log.Warn().Msgf("[tls] skip remote_addr=%s domain=%s", remote, domain)
		return
	}

	log.Debug().Msgf("[tls] open remote_addr=%s domain=%s rule=%s", remote, domain, rule)

	err = handler(src, domain, hello)

	if err != nil {
		log.Warn().Err(err).Msgf("[tls] close remote_addr=%s", remote)
	} else {
		log.Trace().Msgf("[tls] close remote_addr=%s", remote)
	}
}

type handlerFunc func(src net.Conn, host string, hello []byte) error

var handlers = map[string]handlerFunc{}
var defaultHandler handlerFunc

func findHandler(host string) (handlerFunc, string) {
	if handler := handlers[host]; handler != nil {
		return handler, host
	}

	rule := hosts.Resolve(host)
	if handler := handlers[rule]; handler != nil {
		return handler, rule
	}

	return defaultHandler, "default"
}

func serve(address string) {
	log.Info().Msgf("[tls] listen=%s", address)

	ln, err := net.Listen("tcp", address)
	if err != nil {
		log.Error().Err(err).Caller().Send()
		return
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Error().Err(err).Caller().Send()
			return
		}
		go Handle(conn)
	}
}

func parseAction(raw any) handlerFunc {
	switch raw := raw.(type) {
	case string:
		if raw != "" {
			fields, params := app.ParseAction(raw)
			switch fields[0] {
			case "raw_pass":
				return handleRaw(params)
			case "bye_pass":
				return handleByePass(params)
			case "proxy_pass":
				return handleProxy(params)
			}
		}
	case []any:
		return handleMulti(raw)
	}
	return nil
}

func handleMulti(actions []any) handlerFunc {
	var handlers []handlerFunc

	for _, action := range actions {
		handler := parseAction(action)
		if handler == nil {
			log.Warn().Msgf("[tls] wrong action: %s", action)
			continue
		}
		handlers = append(handlers, handler)
	}

	if len(handlers) == 0 {
		return nil
	}

	return func(conn net.Conn, host string, hello []byte) (err error) {
		for _, handler := range handlers {
			err = handler(conn, host, hello)
			if err == nil {
				return
			}
			log.Debug().Err(err).Msg("[tls] can't handle")
		}
		return
	}
}

func handleRaw(params url.Values) handlerFunc {
	return handleRawPass(params, false)
}

func handleByePass(params url.Values) handlerFunc {
	return handleRawPass(params, true)
}

func handleRawPass(params url.Values, useByeDPI bool) handlerFunc {
	forceHost := params.Get("host")
	port := params.Get("port")
	if port == "" {
		port = "443"
	}

	connectTimeout := pkg.ParseTimeout(params.Get("connect_timeout"), 5*time.Second)
	responseTimeout := pkg.ParseTimeout(params.Get("response_timeout"), 0)

	dialer := net.Dialer{Timeout: connectTimeout}

	return func(src net.Conn, host string, hello []byte) error {
		if forceHost != "" {
			host = forceHost
		}

		dst, err := dialer.Dial("tcp", host+":"+port)
		if err != nil {
			return err
		}
		if useByeDPI {
			dst = byedpi.NewDesyncConn(dst.(*net.TCPConn))
		}
		//noinspection GoUnhandledErrorResult
		defer dst.Close()

		if err = exchangeHello(dst, src, hello, responseTimeout); err != nil {
			return err
		}

		hello = nil
		ioCopy(dst, src)
		return nil
	}
}

func handleProxy(params url.Values) handlerFunc {
	if params.Has("proxy") {
		app.ParseProxyParam(params)
	}

	if !params.Has("host") || !params.Has("port") {
		return nil
	}

	if params.Get("type") == "socks5" {
		return handleProxySOCKS5(params)
	} else {
		return handleProxyHTTP(params)
	}
}

func handleProxyHTTP(params url.Values) handlerFunc {
	address := params.Get("host") + ":" + params.Get("port")
	connect := ":443 HTTP/1.1\r\n"
	if params.Has("username") {
		auth := base64.StdEncoding.EncodeToString(
			[]byte(params.Get("username") + ":" + params.Get("password")),
		)
		connect += "Proxy-Authorization: Basic " + auth + "\r\n\r\n"
	} else {
		connect += "\r\n"
	}

	connectTimeout := pkg.ParseTimeout(params.Get("connect_timeout"), 5*time.Second)
	responseTimeout := pkg.ParseTimeout(params.Get("response_timeout"), 0)
	proxyConnectTimeout := pkg.ParseTimeout(params.Get("proxy_connect_timeout"), 5*time.Second)

	dialer := net.Dialer{Timeout: proxyConnectTimeout}

	return func(src net.Conn, host string, hello []byte) error {
		dst, err := dialer.Dial("tcp", address)
		if err != nil {
			return err
		}
		//noinspection GoUnhandledErrorResult
		defer dst.Close()

		if connectTimeout > 0 {
			_ = dst.SetDeadline(time.Now().Add(connectTimeout))
		}

		if _, err = dst.Write([]byte("CONNECT " + host + connect)); err != nil {
			return err
		}

		b := make([]byte, 4096)
		if _, err = dst.Read(b); err != nil {
			return err
		}

		b = nil
		_ = dst.SetDeadline(time.Time{})

		if err = exchangeHello(dst, src, hello, responseTimeout); err != nil {
			return err
		}

		hello = nil
		ioCopy(dst, src)
		return nil
	}
}

func handleProxySOCKS5(params url.Values) handlerFunc {
	address := params.Get("host") + ":" + params.Get("port")

	var auth *proxy.Auth
	if params.Has("username") {
		auth = &proxy.Auth{
			User:     params.Get("username"),
			Password: params.Get("password"),
		}
	}

	connectTimeout := pkg.ParseTimeout(params.Get("connect_timeout"), 5*time.Second)
	responseTimeout := pkg.ParseTimeout(params.Get("response_timeout"), 0)
	proxyConnectTimeout := pkg.ParseTimeout(params.Get("proxy_connect_timeout"), 5*time.Second)

	var forward proxy.Dialer
	if proxyConnectTimeout > 0 {
		forward = &net.Dialer{Timeout: proxyConnectTimeout}
	}

	dialer, err := proxy.SOCKS5("tcp", address, auth, forward)
	if err != nil {
		return nil
	}

	return func(src net.Conn, host string, hello []byte) error {
		dst, err := proxyDialWithTimeout(dialer, "tcp", host+":443", connectTimeout)
		if err != nil {
			return err
		}
		//noinspection GoUnhandledErrorResult
		defer dst.Close()

		if err = exchangeHello(dst, src, hello, responseTimeout); err != nil {
			return err
		}

		hello = nil
		ioCopy(dst, src)
		return nil
	}
}

func proxyDialWithTimeout(dialer proxy.Dialer, network, address string, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		return dialer.Dial(network, address)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return dialer.(proxy.ContextDialer).DialContext(ctx, network, address)
}

func exchangeHello(dst, src net.Conn, clientHello []byte, timeout time.Duration) error {
	if timeout <= 0 {
		_, err := dst.Write(clientHello)
		return err
	}

	_ = dst.SetDeadline(time.Now().Add(timeout))

	if _, err := dst.Write(clientHello); err != nil {
		return err
	}

	serverHello := make([]byte, 4096)
	n, err := dst.Read(serverHello)
	if err != nil {
		return err
	}

	if _, err = src.Write(serverHello[:n]); err != nil {
		return err
	}

	_ = dst.SetDeadline(time.Time{})
	return nil
}

func ioCopy(dst, src net.Conn) {
	go func() {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
	}()

	_, _ = io.Copy(src, dst)
	_ = src.Close()
}
