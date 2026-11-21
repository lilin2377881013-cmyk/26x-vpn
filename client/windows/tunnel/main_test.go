//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"testing"

	"github.com/xtls/xray-core/infra/conf"
	xraytun "github.com/xtls/xray-core/proxy/tun"
	"golang.org/x/sys/windows"
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
						Settings conf.TunConfig
					}
				}
				if err := json.Unmarshal([]byte(encoded), &cfg); err != nil {
					t.Fatal(err)
				}
				tun := cfg.Inbounds[0].Settings
				built, err := tun.Build()
				if err != nil {
					t.Fatal(err)
				}
				if built.(*xraytun.Config).AutoOutboundsInterface != "" || !slices.Contains(tun.AutoSystemRoutingTable, tunnelDNS+"/32") || len(tun.DNS) != 1 || tun.DNS[0] != tunnelDNS {
					t.Fatalf("unexpected adapter configuration")
				}
				for addr, want := range map[string]bool{peer: false, "203.0.112.9": false, "203.0.113.8": true, "2001:db8::8": true, tunnelDNS: true} {
					captured := false
					for _, route := range tun.AutoSystemRoutingTable {
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

func TestStartCoreAfterAdapterRelease(t *testing.T) {
	calls := 0
	result := startCore(context.Background(), "", func(string) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("starting: %w", windows.ERROR_BUSY)
		}
		return nil
	})
	if result != "" || calls != 2 {
		t.Fatalf("result=%q calls=%d", result, calls)
	}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("unable to set DNS: %w", windows.ERROR_ACCESS_DENIED), "adapter_dns_failed"},
		{fmt.Errorf("adapter creation: %w", windows.ERROR_ACCESS_DENIED), "windows_core_5"},
		{errors.New("invalid config with private contents"), "tunnel_core_failed"},
	} {
		calls = 0
		got := startCore(context.Background(), "", func(string) error { calls++; return tc.err })
		if got != tc.want || calls != 1 {
			t.Fatalf("got=%q calls=%d", got, calls)
		}
	}
}
