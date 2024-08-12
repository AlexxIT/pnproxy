package dns

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/dns/dnsmessage"
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
		{
			Name:  dnsmessage.MustNewName(hostname + "."),
			Type:  dnsmessage.TypeAAAA,
			Class: dnsmessage.ClassINET,
		},
		{
			Name:  dnsmessage.MustNewName(hostname + "."),
			Type:  dnsmessage.TypeCNAME,
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

	var results []string
	for _, answer := range res.Answers {
		switch answer.Header.Type {
		case dnsmessage.TypeA:
			ip := answer.Body.(*dnsmessage.AResource).A
			results = append(results, net.IP(ip[:]).String())
		case dnsmessage.TypeAAAA:
			ip := answer.Body.(*dnsmessage.AAAAResource).AAAA
			results = append(results, net.IP(ip[:]).String())
		case dnsmessage.TypeCNAME:
			cname := answer.Body.(*dnsmessage.CNAMEResource).CNAME.String()
			// Recursively resolve the CNAME target

			cnameResults, err := ResolveDNS(strings.TrimSuffix(cname, "."), dnsServer)
			if err != nil {
				return nil, err
			}
			results = append(results, cnameResults...)
		default:
			log.Trace().Msgf("[dns] unknown dnsmessage type: %s", answer.Header.Type.String())
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no valid IP addresses found for %s", hostname)
	}

	results = uniqueStrings(results)

	return results, nil
}
func uniqueStrings(input []string) []string {
	uniqueMap := make(map[string]bool)
	uniqueSlice := []string{}

	for _, str := range input {
		if _, exists := uniqueMap[str]; !exists {
			uniqueMap[str] = true
			uniqueSlice = append(uniqueSlice, str)
		}
	}

	return uniqueSlice
}
