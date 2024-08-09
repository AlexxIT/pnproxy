package dns

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/likexian/doh"
	"github.com/likexian/doh/dns"
)

var client *doh.DoH

func initDOH(provider string, cache bool) {
	switch provider {
	case "cloudflare":
		client = doh.Use(doh.CloudflareProvider)
	case "dnspod":
		client = doh.Use(doh.DNSPodProvider)
	case "google":
		client = doh.Use(doh.GoogleProvider)
	default:
		client = doh.Use(doh.Quad9Provider)
	}

	if cache {
		client = client.EnableCache(true)
	}
}

func Resolve(domain string) (string, error) {
	if net.ParseIP(domain) != nil {
		return domain, nil
	}

	if client == nil {
		return domain, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := client.Query(ctx, dns.Domain(domain), dns.TypeA)
	if err != nil {
		return "", err
	}

	for _, a := range result.Answer {
		if a.Type == 1 {
			return a.Data, nil
		}
	}

	return "", errors.New("dns: can't resolve domain: " + domain)
}
