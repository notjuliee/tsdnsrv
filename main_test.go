package main

import (
	"net/netip"
	"testing"
)

func TestParseListenAddr(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantIP       string
		wantPort     string
		wantProvided bool
		wantErr      bool
	}{
		{
			name:         "port only",
			input:        "53",
			wantPort:     "53",
			wantProvided: false,
		},
		{
			name:         "colon port",
			input:        ":53",
			wantPort:     "53",
			wantProvided: false,
		},
		{
			name:         "ipv4 explicit",
			input:        "100.64.0.10:8053",
			wantIP:       "100.64.0.10",
			wantPort:     "8053",
			wantProvided: true,
		},
		{
			name:         "ipv6 service name",
			input:        "[fd7a:115c:a1e0::1]:domain",
			wantIP:       "fd7a:115c:a1e0::1",
			wantPort:     "53",
			wantProvided: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "bad host",
			input:   "example.com:53",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip, port, provided, err := parseListenAddr(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseListenAddr(%q) expected error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseListenAddr(%q) unexpected error: %v", tc.input, err)
			}
			if provided != tc.wantProvided {
				t.Fatalf("parseListenAddr(%q) provided=%v want %v", tc.input, provided, tc.wantProvided)
			}
			if port != tc.wantPort {
				t.Fatalf("parseListenAddr(%q) port=%q want %q", tc.input, port, tc.wantPort)
			}
			if tc.wantIP == "" {
				if ip.IsValid() {
					t.Fatalf("parseListenAddr(%q) ip=%v, want invalid", tc.input, ip)
				}
			} else {
				wantIP := netip.MustParseAddr(tc.wantIP)
				if ip != wantIP {
					t.Fatalf("parseListenAddr(%q) ip=%v want %v", tc.input, ip, wantIP)
				}
			}
		})
	}
}

func TestResolveTailnetListenAddr(t *testing.T) {
	ipv4 := netip.MustParseAddr("100.64.0.2")
	ipv6 := netip.MustParseAddr("fd7a:115c:a1e0::2")

	tests := []struct {
		name     string
		listen   string
		tails    []netip.Addr
		wantIP   string
		wantPort string
		wantAuto bool
		wantErr  bool
	}{
		{
			name:     "auto chooses ipv4",
			listen:   ":5300",
			tails:    []netip.Addr{ipv4, ipv6},
			wantIP:   ipv4.String(),
			wantPort: "5300",
			wantAuto: true,
		},
		{
			name:     "auto falls back to ipv6",
			listen:   "5301",
			tails:    []netip.Addr{netip.Addr{}, ipv6},
			wantIP:   ipv6.String(),
			wantPort: "5301",
			wantAuto: true,
		},
		{
			name:     "explicit ip preserved",
			listen:   "0.0.0.0:8053",
			tails:    []netip.Addr{ipv4},
			wantIP:   "0.0.0.0",
			wantPort: "8053",
			wantAuto: false,
		},
		{
			name:    "no tailscale ip available",
			listen:  ":8053",
			tails:   nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip, port, auto, err := resolveTailnetListenAddr(tc.listen, tc.tails)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveTailnetListenAddr(%q) expected error", tc.listen)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTailnetListenAddr(%q) unexpected error: %v", tc.listen, err)
			}
			if ip != tc.wantIP {
				t.Fatalf("resolveTailnetListenAddr(%q) ip=%q want %q", tc.listen, ip, tc.wantIP)
			}
			if port != tc.wantPort {
				t.Fatalf("resolveTailnetListenAddr(%q) port=%q want %q", tc.listen, port, tc.wantPort)
			}
			if auto != tc.wantAuto {
				t.Fatalf("resolveTailnetListenAddr(%q) auto=%v want %v", tc.listen, auto, tc.wantAuto)
			}
		})
	}
}
