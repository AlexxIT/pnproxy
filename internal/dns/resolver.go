package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// ResolveDNS resolves the given domain name using a specified DNS server and returns a list of IP addresses.
//
// Parameters:
//   - domain: The domain name to resolve.
//   - dnsServer: The address of the DNS server to use for resolution. It can include an optional port number,
//     otherwise, it defaults to port 53.
//
// Returns:
//   - A slice of strings containing the resolved IP addresses.
//   - An error if the domain cannot be resolved or if no records are found.
//
// The function creates a custom DNS resolver that uses the specified DNS server. If the DNS server address
// does not specify a port, port 53 is assumed. A context with a timeout of 5 seconds is used to limit the
// duration of the DNS resolution attempt. The function only attempts to resolve IPv4 addresses.
func ResolveDNS(domain string, dnsServer string) ([]string, error) {
	dnsHost, dnsPort, hasPort := strings.Cut(dnsServer, ":")
	if !hasPort {
		dnsPort = "53"
	}
	dnsServer = net.JoinHostPort(dnsHost, dnsPort)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(5)*time.Second)
	defer cancel()
	systemResolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {

		var c net.Conn
		var err error

		var d net.Dialer
		c, err = d.DialContext(ctx, network, dnsServer)

		if err != nil {
			return nil, err
		}
		return c, nil
	}}
	ips, err := systemResolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return nil, fmt.Errorf("could not resolve the domain(system)")
	}

	var results []string

	for _, ip := range ips {
		results = append(results, ip.String())
	}
	if len(results) > 0 {
		return results, nil
	}
	return nil, fmt.Errorf("no record found(system)")
}
