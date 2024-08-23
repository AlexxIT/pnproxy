package dns

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/likexian/doh"
	dohDNS "github.com/likexian/doh/dns"
	"github.com/miekg/dns"
	"github.com/rs/zerolog/log"
)

type dohConn struct {
	client   *doh.DoH
	deadline time.Time
	pool     sync.Pool
}

func (d *dohConn) Read(b []byte) (n int, err error) {
	req := d.pool.Get().(*dns.Msg)
	res := &dns.Msg{}
	res.SetReply(req)

	if req.Opcode == dns.OpcodeQuery {
		d.handle(res)
	}

	msg, err := res.Pack()
	if err != nil {
		return
	}

	return copy(b, msg), nil
}

func (d *dohConn) Write(b []byte) (n int, err error) {
	msg := new(dns.Msg)
	if err = msg.Unpack(b); err != nil {
		return
	}
	d.pool.Put(msg)
	return len(b), nil
}

func (d *dohConn) Close() error {
	return nil
}

func (d *dohConn) LocalAddr() net.Addr {
	return nil
}

func (d *dohConn) RemoteAddr() net.Addr {
	return nil
}

func (d *dohConn) SetDeadline(t time.Time) error {
	d.deadline = t
	return nil
}

func (d *dohConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (d *dohConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (d *dohConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return
}

func (d *dohConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return
}

func (d *dohConn) handle(query *dns.Msg) {
	ctx := context.Background()

	if !d.deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, d.deadline)
		defer cancel()
	}

	for _, question := range query.Question {
		switch question.Qtype {
		case dns.TypeA:
			answers := d.query(ctx, question.Name)
			for _, answer := range answers {
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   question.Name,
						Rrtype: question.Qtype,
						Class:  question.Qclass,
						Ttl:    uint32(answer.TTL),
					},
					A: net.ParseIP(answer.Data),
				}
				query.Answer = append(query.Answer, rr)
			}
		}
	}
}

var dohStatic = map[string][]string{
	"cloudflare-dns.com.": {"104.16.249.249", "104.16.248.249"},
	"dns.google.":         {"8.8.4.4", "8.8.8.8"},
	"dns9.quad9.net.":     {"9.9.9.9", "149.112.112.9"},
	"dns10.quad9.net.":    {"9.9.9.10", "149.112.112.10"},
	"dns11.quad9.net.":    {"9.9.9.11", "149.112.112.11"},
}

func (d *dohConn) query(ctx context.Context, name string) []dohDNS.Answer {
	if addrs, ok := dohStatic[name]; ok {
		var answers []dohDNS.Answer
		for _, addr := range addrs {
			answers = append(answers, dohDNS.Answer{
				Name: name, TTL: 3600, Data: addr,
			})
		}
		return answers
	}

	if len(name) == 0 {
		return nil
	}

	name = name[:len(name)-1]

	log.Trace().Msgf("[dns] query name=%s", name)

	res, _ := d.client.Query(ctx, dohDNS.Domain(name), dohDNS.TypeA)
	if res == nil {
		return nil
	}

	return res.Answer
}
