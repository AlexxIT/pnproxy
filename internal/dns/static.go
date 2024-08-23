package dns

import (
	"net"
	"strings"
)

var static = map[string][]net.IP{}

func addStaticIP(name string, addrs []string) {
	var ips []net.IP
	for _, addr := range addrs {
		ips = append(ips, net.ParseIP(addr))
	}
	// use suffix point, because all DNS queries has it
	// use prefix point, because support subdomains by default
	static["."+name+"."] = ips
}

func lookupStaticIP(name string) ([]net.IP, error) {
	name = "." + name
	for suffix, items := range static {
		if strings.HasSuffix(name, suffix) {
			return items, nil
		}
	}
	return nil, nil
}
