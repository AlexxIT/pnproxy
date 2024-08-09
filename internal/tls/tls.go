package tls

import (
	"encoding/base64"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/dns"
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

	b := make([]byte, 1024*8)
	n, err := src.Read(b)
	if err != nil {
		log.Warn().Err(err).Caller().Send()
		return
	}

	domain := parseSNI(b[:n])

	handler := findHandler(domain)
	if handler == nil {
		log.Trace().Msgf("[tls] skip remote_addr=%s domain=%s", remote, domain)
		return
	}

	host, err := dns.Resolve(domain)
	if err != nil {
		log.Warn().Err(err).Caller().Send()
		return
	}

	log.Trace().Msgf("[tls] open remote_addr=%s domain=%s host=%s", remote, domain, host)

	handler(src, host, b[:n])

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
	log.Debug().Msgf("[tls] listen=%s", address)
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

	return func(src net.Conn, host string, hello []byte) {
		if forceHost != "" {
			host = forceHost
		}

		dst, err := net.DialTimeout("tcp", host+":"+port, 5*time.Second)
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
	sleepN := 100
	sleepD := time.Millisecond

	if params.Has("sleep") {
		s := strings.Split(params.Get("sleep"), "/")
		sleepN, _ = strconv.Atoi(s[0])
		sleepD, _ = time.ParseDuration(s[1])
	}

	return func(src net.Conn, host string, hello []byte) {
		dst, err := net.DialTimeout("tcp", host+":443", 5*time.Second)
		if err != nil {
			log.Warn().Err(err).Caller().Send()
			_ = src.Close()
			return
		}
		defer dst.Close()

		if err = writeSplit(dst, hello, sleepN, sleepD); err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}

		go io.Copy(dst, src)
		io.Copy(src, dst)
	}
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
			_ = src.Close()
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
			_ = src.Close()
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

func writeSplit(conn net.Conn, hello []byte, sleepN int, sleepD time.Duration) error {
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	for i, b := range hello {
		if _, err := conn.Write([]byte{b}); err != nil {
			return err
		}
		if i%sleepN == 0 {
			time.Sleep(sleepD)
		}
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return nil
}