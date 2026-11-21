//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"testing"
)

func TestWindowsTransportRoute(t *testing.T) {
	for _, protocol := range []string{"vless", "hysteria"} {
		for _, peer := range []string{"203.0.113.7", "2001:db8::7"} {
			t.Run(protocol+"/"+peer, func(t *testing.T) {
				raw := fmt.Sprintf(`{"inbounds":[{"protocol":"tun","settings":{"autoOutboundsInterface":"auto"}}],"outbounds":[{"tag":"26x-proxy","protocol":%q,"settings":{"address":%q}}],"routing":{"rules":[]}}`, protocol, peer)
				encoded, err := configure(context.Background(), []byte(raw), "203.0.112.0/24")
				if err != nil {
					t.Fatal(err)
				}
				var cfg struct {
					Inbounds []struct {
						Settings struct {
							Routes []string `json:"autoSystemRoutingTable"`
							Auto   string   `json:"autoOutboundsInterface"`
							DNS    []string `json:"dns"`
						}
					}
				}
				if err := json.Unmarshal([]byte(encoded), &cfg); err != nil {
					t.Fatal(err)
				}
				tun := cfg.Inbounds[0].Settings
				if tun.Auto != "" || len(tun.DNS) != 1 || tun.DNS[0] != tunnelDNS {
					t.Fatalf("unexpected adapter configuration")
				}
				for addr, want := range map[string]bool{peer: false, "203.0.112.9": false, "203.0.113.8": true, "2001:db8::8": true, tunnelDNS: true} {
					captured := false
					for _, route := range tun.Routes {
						if netip.MustParsePrefix(route).Contains(netip.MustParseAddr(addr)) {
							captured = true
							break
						}
					}
					if captured != want {
						t.Errorf("%s captured=%v, want %v", addr, captured, want)
					}
				}
			})
		}
	}
}
