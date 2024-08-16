package app

import (
	"net/url"
	"testing"

	"reflect"
)

func TestParseAction(t *testing.T) {
	tests := []struct {
		input  string
		action string
		params url.Values
	}{
		{
			input:  "static address 127.0.0.1",
			action: "static",
			params: url.Values{"address": {"127.0.0.1"}},
		},
		{
			input:  "redirect scheme https",
			action: "redirect",
			params: url.Values{"scheme": {"https"}},
		},
		{
			input:  "proxy_pass host 123.123.123.123 port 3128",
			action: "proxy_pass",
			params: url.Values{"host": {"123.123.123.123"}, "port": {"3128"}},
		},
		{
			input:  "split_pass sleep 100/1ms",
			action: "split_pass",
			params: url.Values{"sleep": {"100/1ms"}},
		},
		{
			input:  "dns server 8.8.8.8 cache true",
			action: "dns",
			params: url.Values{"server": {"8.8.8.8"}, "cache": {"true"}},
		},
		{
			input:  "dns server 8.8.8.8 server 8.8.4.4 cache true",
			action: "dns",
			params: url.Values{"server": {"8.8.8.8", "8.8.4.4"}, "cache": {"true"}},
		},
		{
			input:  "", // Testing empty input case
			action: "",
			params: nil,
		},
	}

	for _, tt := range tests {
		action, params := ParseAction(tt.input)
		if action != tt.action {
			t.Errorf("ParseAction(%q) action = %q; want %q", tt.input, action, tt.action)
		}
		if !reflect.DeepEqual(params, tt.params) {
			t.Errorf("ParseAction(%q) params = %v; want %v", tt.input, params, tt.params)
		}
	}
}
