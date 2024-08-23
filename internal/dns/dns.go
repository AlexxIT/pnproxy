package dns

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/hosts"
	"github.com/likexian/doh"
	"github.com/miekg/dns"
	"github.com/rs/zerolog/log"
)

func Init() {
	var cfg struct {
		DNS struct {
			Listen string `yaml:"listen"`
			Rules  []struct {
				Name   string `yaml:"name"`
				Action string `yaml:"action"`
			} `yaml:"rules"`
			Default struct {
				Action string `yaml:"action"`
			} `yaml:"default"`
		} `yaml:"dns"`
	}
	app.LoadConfig(&cfg)

	for _, rule := range cfg.DNS.Rules {
		action, params := app.ParseAction(rule.Action)
		switch action {
		case "static":
			domains := hosts.Get(rule.Name)
			log.Debug().Msgf("[dns] static address for %s", domains)
			for _, domain := range domains {
				addStaticIP(domain, params["address"])
			}
		default:
			log.Warn().Msgf("[dns] unknown action: %s", action)
		}
	}

	if dial := parseDefaultAction(cfg.DNS.Default.Action); dial != nil {
		net.DefaultResolver.PreferGo = true
		net.DefaultResolver.Dial = dial
	}

	if cfg.DNS.Listen != "" {
		go serve(cfg.DNS.Listen)
	}
}

func serve(address string) {
	log.Info().Msgf("[dns] listen=%s", address)
	server := &dns.Server{Addr: address, Net: "udp"}
	server.Handler = dns.HandlerFunc(func(wr dns.ResponseWriter, msg *dns.Msg) {
		m := &dns.Msg{}
		m.SetReply(msg)

		if msg.Opcode == dns.OpcodeQuery {
			parseQuery(m)
		}

		_ = wr.WriteMsg(m)
	})

	if err := server.ListenAndServe(); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

func parseQuery(query *dns.Msg) {
	for _, question := range query.Question {
		if question.Qtype == dns.TypeA {
			ips, _ := lookupStaticIP(question.Name)

			if ips == nil {
				ips, _ = net.LookupIP(question.Name)
			}

			for _, ip := range ips {
				if ip.To4() != nil {
					rr := &dns.A{
						Hdr: dns.RR_Header{
							Name:   question.Name,
							Rrtype: question.Qtype,
							Class:  question.Qclass,
							Ttl:    3600,
						},
						A: ip,
					}
					query.Answer = append(query.Answer, rr)
				}
			}
		}
	}
}

type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

func parseDefaultAction(raw string) dialFunc {
	if raw != "" {
		action, params := app.ParseAction(raw)
		switch action {
		case "dns":
			return dialDNS(params)
		case "doh":
			return dialDOH(params)
		}
	}
	return nil
}

func dialDNS(params url.Values) dialFunc {
	if !params.Has("server") {
		return nil
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	address := params.Get("server") + ":53"
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	}
}

func dialDOH(params url.Values) dialFunc {
	var client *doh.DoH

	switch params.Get("provider") {
	case "cloudflare":
		client = doh.Use(doh.CloudflareProvider)
	case "dnspod":
		client = doh.Use(doh.DNSPodProvider)
	case "google":
		client = doh.Use(doh.GoogleProvider)
	case "quad9":
		client = doh.Use(doh.Quad9Provider)
	default:
		client = doh.Use()
	}

	if params.Get("cache") == "true" {
		client = client.EnableCache(true)
	}

	conn := &dohConn{client: client}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return conn, nil
	}
}
