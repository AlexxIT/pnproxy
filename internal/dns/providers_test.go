package dns

import (
	"context"
	"net"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviders(t *testing.T) {
	test := func(f func(params url.Values) dialFunc, provider string) {
		params := map[string][]string{"provider": {provider}}
		resolver := &net.Resolver{PreferGo: true, Dial: f(params)}
		addrs, err := resolver.LookupHost(context.Background(), "dns.google")
		require.Nil(t, err)
		require.Len(t, addrs, 4)
	}

	for provider := range providers {
		test(dialDNS, provider)
		test(dialDOH, provider)
		test(dialDOT, provider)
	}
}
