//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xtls/libxray/xray"
	"golang.org/x/sys/windows"
)

func main() {
	if code := run(); code != "" {
		fmt.Println(code)
		os.Exit(1)
	}
}

func run() string {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return "vpn_permission_denied"
	}
	executable, err := os.Executable()
	if err != nil {
		return "engine_unavailable"
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(executable), "wintun.dll")); err != nil {
		return "engine_unavailable"
	}
	assets, err := os.ReadFile(filepath.Join(filepath.Dir(executable), "direct-ipv4-cidrs.txt"))
	if err != nil {
		return "engine_unavailable"
	}
	routes, err := tunnelRoutes(string(assets))
	if err != nil {
		return "engine_unavailable"
	}
	input := bufio.NewReader(io.LimitReader(os.Stdin, 1<<20))
	raw, err := input.ReadBytes('\n')
	if err != nil {
		return "profile_invalid"
	}
	config, err := configure(raw, routes)
	if err != nil {
		return "profile_invalid"
	}
	probes, err := xray.PingBatch(
		[]xray.PingBatchItem{{XrayJSON: config, OutboundTag: "26x-proxy"}},
		8, "https://cp.cloudflare.com/generate_204",
	)
	if err != nil || len(probes) != 1 || !probes[0].Success {
		return "node_unreachable"
	}
	if err := xray.RunXray(config); err != nil {
		return "tunnel_start_failed"
	}
	fmt.Println("connected")
	// Closing the app-owned pipe also releases the tunnel after an app crash.
	_, _ = io.Copy(io.Discard, input)
	if err := xray.StopXray(); err != nil {
		return "tunnel_stop_failed"
	}
	return ""
}

func configure(raw []byte, routes []string) (string, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", err
	}
	inbounds, ok := config["inbounds"].([]any)
	if !ok || len(inbounds) != 1 {
		return "", errors.New("missing TUN inbound")
	}
	tun, ok := inbounds[0].(map[string]any)
	if !ok || tun["protocol"] != "tun" {
		return "", errors.New("invalid TUN inbound")
	}
	settings, ok := tun["settings"].(map[string]any)
	if !ok {
		return "", errors.New("missing TUN settings")
	}
	settings["autoSystemRoutingTable"] = routes
	settings["autoOutboundsInterface"] = "auto"
	// Give Windows IPv4 DNS targets. Translate their port 53 traffic into
	// DNS-over-HTTPS inside Xray so the node only needs to forward TCP 443.
	settings["dns"] = []string{"1.1.1.1", "1.0.0.1"}
	settings["autoSystemWfpBlockLeak"] = []string{"dns", "misconfigtun"}
	routing, ok := config["routing"].(map[string]any)
	if !ok {
		return "", errors.New("missing routing configuration")
	}
	rules, ok := routing["rules"].([]any)
	if !ok {
		return "", errors.New("missing routing rules")
	}
	outbounds, ok := config["outbounds"].([]any)
	if !ok || len(outbounds) == 0 {
		return "", errors.New("missing outbounds")
	}
	config["dns"] = map[string]any{
		"tag": "26x-doh",
		"servers": []string{
			"https://1.1.1.1/dns-query",
			"https://1.0.0.1/dns-query",
		},
	}
	config["outbounds"] = append(outbounds, map[string]string{
		"tag": "26x-dns", "protocol": "dns",
	})
	routing["rules"] = append([]any{
		map[string]any{"type": "field", "inboundTag": []string{"26x-doh"}, "outboundTag": "26x-proxy"},
		map[string]any{
			"type": "field", "inboundTag": []string{"26x-tun"},
			"ip": settings["dns"], "port": "53", "network": "tcp,udp", "outboundTag": "26x-dns",
		},
	}, rules...)
	config["log"] = map[string]string{"loglevel": "none"}
	delete(config, "env")
	result, err := json.Marshal(config)
	return string(result), err
}

type network struct {
	from, to uint64
}

// Install the IPv4 complement in the Wintun route table. Destinations
// omitted from that table are handled by the physical network.
func tunnelRoutes(cidrList string) ([]string, error) {
	var excluded []network
	for line := range strings.SplitSeq(cidrList, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, errors.New("invalid IPv4 route")
		}
		octets := prefix.Addr().As4()
		start := uint64(octets[0])<<24 | uint64(octets[1])<<16 | uint64(octets[2])<<8 | uint64(octets[3])
		excluded = append(excluded, network{start, start + 1<<uint(32-prefix.Bits())})
	}
	if len(excluded) == 0 {
		return nil, errors.New("empty IPv4 route list")
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].from < excluded[j].from })
	routes := []string{"::/0"}
	addRange := func(from, to uint64) {
		for from < to {
			spanBits := min(bits.TrailingZeros64(from), bits.Len64(to-from)-1)
			addr := netip.AddrFrom4([4]byte{byte(from >> 24), byte(from >> 16), byte(from >> 8), byte(from)})
			routes = append(routes, netip.PrefixFrom(addr, 32-spanBits).String())
			from += 1 << uint(spanBits)
		}
	}

	var cursor uint64
	for _, entry := range excluded {
		addRange(cursor, entry.from)
		cursor = max(cursor, entry.to)
	}
	addRange(cursor, 1<<32)
	return routes, nil
}
