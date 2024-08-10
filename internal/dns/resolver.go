package dns

import (
	"fmt"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"time"
)

func ResolveDNS(hostname, dnsServer string) ([]string, error) {
	var msg dnsmessage.Message
	msg.Header.RecursionDesired = true
	msg.Questions = []dnsmessage.Question{
		{
			Name:  dnsmessage.MustNewName(hostname + "."),
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
		},
	}

	query, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %v", err)
	}

	serverAddr, err := net.ResolveUDPAddr("udp", dnsServer+":53")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve DNS server address: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial UDP: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = conn.Write(query)
	if err != nil {
		return nil, fmt.Errorf("failed to send DNS query: %v", err)
	}

	response := make([]byte, 512)
	n, err := conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to read DNS response: %v", err)
	}

	var res dnsmessage.Message
	err = res.Unpack(response[:n])
	if err != nil {
		return nil, fmt.Errorf("failed to unpack DNS response: %v", err)
	}

	var ips []string
	for _, answer := range res.Answers {
		if answer.Header.Type == dnsmessage.TypeA {
			ip := answer.Body.(*dnsmessage.AResource).A
			ips = append(ips, net.IP(ip[:]).String())
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no A records found for %s", hostname)
	}

	return ips, nil
}
