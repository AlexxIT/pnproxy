package dns

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"time"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/hosts"
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
		fields, params := app.ParseAction(rule.Action)
		switch fields[0] {
		case "static":
			domains := hosts.Get(rule.Name)
			log.Debug().Msgf("[dns] static address for %s", domains)
			for _, domain := range domains {
				addStaticIP(domain, params["address"])
			}
		default:
			log.Warn().Msgf("[dns] unknown action: %s", fields)
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
			parseQuery(m, wr.RemoteAddr())
		}

		_ = wr.WriteMsg(m)
	})

	if err := server.ListenAndServe(); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

func parseQuery(query *dns.Msg, remoteAddr net.Addr) {
	for _, question := range query.Question {
		if question.Qtype == dns.TypeA {
			ips, _ := lookupStaticIP(question.Name)

			if ips == nil {
				ips, _ = net.LookupIP(question.Name)
			}

			log.Trace().Msgf("[dns] query remote_addr=%s name=%s ips=%s", remoteAddr, question.Name, ips)

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
		fields, params := app.ParseAction(raw)
		switch fields[0] {
		case "dns":
			return dialDNS(params)
		case "doh":
			return dialDOH(params)
		case "dot":
			return dialDOT(params)
		}
	}
	return nil
}

func dialDNS(params url.Values) dialFunc {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	address := server(params) + ":53"
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "udp", address)
	}
}

func dialDOT(params url.Values) dialFunc {
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}}
	address := server(params) + ":853"
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp", address)
	}
}

func dialDOH(params url.Values) dialFunc {
	conn := newDoHConn(server(params))
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return conn, nil
	}
}
