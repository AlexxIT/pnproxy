package dns

import (
	"reflect"
	"testing"
)

func Test_ResolveDNS(t *testing.T) {
	type args struct {
		hostname  string
		dnsServer string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "Valid hostname and DNS server",
			args: args{
				hostname:  "a.root-servers.net",
				dnsServer: "8.8.8.8",
			},
			want:    []string{"198.41.0.4"},
			wantErr: false,
		},
		{
			name: "Valid hostname with unreachable DNS server",
			args: args{
				hostname:  "example.com",
				dnsServer: "192.0.2.1", // Using an IP from reserved ranges for testing
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Invalid hostname with valid DNS server",
			args: args{
				hostname:  "invalid-domain.tld",
				dnsServer: "8.8.8.8",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Empty hostname",
			args: args{
				hostname:  "",
				dnsServer: "8.8.8.8",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Empty DNS server",
			args: args{
				hostname:  "example.com",
				dnsServer: "",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "CNAME resolution for domain cname.info",
			args: args{
				hostname:  "cname.info",
				dnsServer: "8.8.8.8",
			},
			want:    []string{"43.142.246.57"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDNS(tt.args.hostname, tt.args.dnsServer)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveDNS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveDNS() = %v, want %v", got, tt.want)
			}
		})
	}
}
