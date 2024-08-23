package dns

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/idna"
)

type dohConn struct {
	server   string
	deadline time.Time
	pool     sync.Pool
}

func (d *dohConn) Read(b []byte) (n int, err error) {
	req, ok := d.pool.Get().(*dns.Msg)
	if !ok {
		return
	}

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
			msg, err := d.query(ctx, question.Name)
			if err != nil {
				continue
			}
			for _, answer := range msg.Answer {
				if answer.Type != dns.TypeA {
					continue
				}
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   question.Name,
						Rrtype: question.Qtype,
						Class:  question.Qclass,
						Ttl:    answer.TTL,
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
}

func (d *dohConn) query(ctx context.Context, name string) (*dohMsg, error) {
	if addrs, ok := dohStatic[name]; ok {
		msg := &dohMsg{}
		for _, addr := range addrs {
			msg.Answer = append(msg.Answer, dohAnswer{
				Name: name, Type: dns.TypeA, TTL: 3600, Data: addr,
			})
		}
		return msg, nil
	}

	if len(name) == 0 {
		return nil, errors.New("doh: empty name")
	}

	name, err := dohName(name[:len(name)-1])
	if err != nil {
		return nil, err
	}

	log.Trace().Msgf("[dns] query name=%s", name)

	params := url.Values{}
	params.Add("name", name)
	params.Add("type", "A")

	req, err := http.NewRequestWithContext(ctx, "GET", d.server+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	msg := &dohMsg{}

	if err = json.NewDecoder(res.Body).Decode(msg); err != nil {
		return nil, err
	}

	return msg, nil
}

type dohQuestion struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
}

type dohAnswer struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
	TTL  uint32 `json:"TTL"`
	Data string `json:"data"`
}

type dohMsg struct {
	Question []dohQuestion `json:"Question"`
	Answer   []dohAnswer   `json:"Answer"`
}

func dohName(s string) (string, error) {
	return idna.New(
		idna.MapForLookup(),
		idna.Transitional(true),
		idna.StrictDomainName(false),
	).ToASCII(s)
}
