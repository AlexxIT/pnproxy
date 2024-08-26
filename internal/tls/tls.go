package tls

import (
	"encoding/base64"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/hosts"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"
)

func Init() {
	var cfg struct {
		TLS struct {
			Listen string `yaml:"listen"`
			Rules  []struct {
				Name   string `yaml:"name"`
				Action string `yaml:"action"`
			}
			Default struct {
				Action string `yaml:"action"`
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

		for _, name := range hosts.Get(rule.Name) {
			handlers["."+name] = handler
		}
	}

	defaultHandler = parseAction(cfg.TLS.Default.Action)

	if cfg.TLS.Listen != "" {
		go serve(cfg.TLS.Listen)
	}
}

type handlerFunc func(src net.Conn, host string, hello []byte)

var handlers = map[string]handlerFunc{}
var defaultHandler handlerFunc

func Handle(src net.Conn) {
	defer src.Close()

	remote := src.RemoteAddr().String()

	hello, err := readClientHello(src)
	if err != nil {
		log.Warn().Err(err).Caller().Send()
		return
	}

	domain := parseSNI(hello)
	if domain == "" {
		log.Warn().Msgf("[tls] skip empty domain remote_addr=%s data=%x", remote, hello)
		return
	}

	handler := findHandler(domain)
	if handler == nil {
		log.Trace().Msgf("[tls] skip remote_addr=%s domain=%s", remote, domain)
		return
	}

	log.Trace().Msgf("[tls] open remote_addr=%s domain=%s", remote, domain)

	handler(src, domain, hello)

	log.Trace().Msgf("[tls] close remote_addr=%s", remote)
}

func findHandler(domain string) handlerFunc {
	domain = "." + domain
	for k, handler := range handlers {
		if strings.HasSuffix(domain, k) {
			return handler
		}
	}
	return defaultHandler
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

func parseAction(raw string) handlerFunc {
	if raw != "" {
		action, params := app.ParseAction(raw)
		switch action {
		case "raw_pass":
			return handleRaw(params)
		case "proxy_pass":
			return handleProxy(params)
		case "split_pass":
			return handleSplit(params)
		}
	}
	return nil
}

func handleRaw(params url.Values) handlerFunc {
	forceHost := params.Get("host")
	port := params.Get("port")
	if port == "" {
		port = "443"
	}

	dialer := net.Dialer{Timeout: 5 * time.Second}

	return func(src net.Conn, host string, hello []byte) {
		if forceHost != "" {
			host = forceHost
		}

		dst, err := dialer.Dial("tcp", host+":"+port)
		if err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}
		defer dst.Close()

		if _, err = dst.Write(hello); err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}

		go io.Copy(dst, src)
		io.Copy(src, dst)
	}
}

func handleSplit(params url.Values) handlerFunc {
	return func(src net.Conn, host string, hello []byte) {
		for i := byte(0); i < 3; i++ {
			if err := handleSplitTimeout(src, host, hello, i); err != nil {
				continue
			}
			return
		}
		log.Warn().Msgf("[tcp] split fail host=%s", host)
	}
}

func handleSplitTimeout(src net.Conn, host string, hello []byte, retry byte) error {
	dst, err := net.DialTimeout("tcp", host+":443", 5*time.Second)
	if err != nil {
		return err
	}
	defer dst.Close()

	delay := time.Duration(retry) * 3 * time.Millisecond // 0ms, 3ms, 6ms
	if err = writeSplit(dst, hello, delay); err != nil {
		return err
	}

	timeout := time.Duration(retry+1) * time.Second // 1s, 2s, 3s
	_ = dst.SetReadDeadline(time.Now().Add(timeout))

	b, err := dst.Read(hello)
	if err != nil {
		return err
	}

	_ = dst.SetReadDeadline(time.Time{})

	if retry > 0 {
		log.Debug().Msgf("[tcp] split ok host=%s retry=%d", host, retry)
	}

	if _, err = src.Write(hello[:b]); err != nil {
		return nil
	}

	go io.Copy(dst, src)
	io.Copy(src, dst)

	return nil
}

func writeSplit(conn net.Conn, hello []byte, delay time.Duration) error {
	if delay == 0 {
		for _, b := range hello {
			if _, err := conn.Write([]byte{b}); err != nil {
				return err
			}
		}
	} else {
		t0 := time.Now()
		for i, b := range hello {
			if dt := t0.Add(time.Duration(i) * delay).Sub(time.Now()); dt > 0 {
				time.Sleep(dt)
			}
			if _, err := conn.Write([]byte{b}); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleProxy(params url.Values) handlerFunc {
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

	dialer := net.Dialer{Timeout: 5 * time.Second}

	return func(src net.Conn, host string, hello []byte) {
		dst, err := dialer.Dial("tcp", address)
		if err != nil {
			return
		}
		defer dst.Close()

		if _, err = dst.Write([]byte("CONNECT " + host + connect)); err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}

		b := make([]byte, 1024*4)
		if _, err = dst.Read(b); err != nil {
			return
		}

		if _, err = dst.Write(hello); err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}

		go io.Copy(dst, src)
		io.Copy(src, dst)
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

	dialer, err := proxy.SOCKS5("tcp", address, auth, nil)
	if err != nil {
		return nil
	}

	return func(src net.Conn, host string, hello []byte) {
		dst, err := dialer.Dial("tcp", host+":443")
		if err != nil {
			return
		}
		defer dst.Close()

		if _, err = dst.Write(hello); err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}

		go io.Copy(dst, src)
		io.Copy(src, dst)
	}
}
