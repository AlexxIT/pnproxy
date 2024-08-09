package dns

import (
	"math/rand"
	"strings"

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

	action, params := app.ParseAction(cfg.DNS.Default.Action)
	if action == "doh" {
		initDOH(params.Get("provider"), params.Get("cache") == "true")
	}

	for _, rule := range cfg.DNS.Rules {
		action, params = app.ParseAction(rule.Action)
		switch action {
		case "static":
			domains := hosts.Get(rule.Name)
			log.Debug().Msgf("[dns] static address for %s", domains)
			for _, domain := range domains {
				// use suffix point, because all DNS queries has it
				// use prefix point, because support subdomains by default
				static["."+domain+"."] = params["address"]
			}
		default:
			log.Warn().Msgf("[dns] unknown action: %s", action)
		}
	}

	if cfg.DNS.Listen != "" {
		go serve(cfg.DNS.Listen)
	}
}

var static = map[string][]string{
	"cloudflare-dns.com.": {"104.16.249.249", "104.16.248.249"},
	"dns.google.":         {"8.8.4.4", "8.8.8.8"},
	"dns9.quad9.net.":     {"9.9.9.9", "149.112.112.9"},
	"dns10.quad9.net.":    {"9.9.9.10", "149.112.112.10"},
	"dns11.quad9.net.":    {"9.9.9.11", "149.112.112.11"},
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

func parseQuery(m *dns.Msg) {
	for _, q := range m.Question {
		if q.Qtype == dns.TypeA {
			ip := resolveStatic(q.Name)

			if ip == "" {
				if client == nil {
					continue
				}
				if ip, _ = Resolve(q.Name); ip == "" {
					continue
				}
			}

			log.Trace().Msgf("[dns] resolve domain=%s ipv4=%s", q.Name, ip)

			rr, err := dns.NewRR(q.Name + " A " + ip + "\n")
			if err != nil {
				continue
			}

			m.Answer = append(m.Answer, rr)
		}
	}
}

func resolveStatic(name string) string {
	name = "." + name
	for suffix, items := range static {
		if strings.HasSuffix(name, suffix) {
			if len(items) == 1 {
				return items[0]
			}
			return items[rand.Int()%len(items)]
		}
	}
	return ""
}
