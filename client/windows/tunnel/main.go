//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/miekg/dns"
	"github.com/xtls/libxray/xray"
	"go4.org/netipx"
	"golang.org/x/sys/windows"
)

func main() {
	if code := run(); code != "" {
		fmt.Println(code)
		os.Exit(1)
	}
}

const tunnelDNS = "10.26.0.2"

// Reserved worker exit codes let the UI identify a watchdog exit even if the
// core cannot finish shutting down or flush its ordinary error response.
const (
	exitCleanupTimeout           = 70
	exitDNSRollbackTimeout       = 71
	exitDataRollbackTimeout      = 72
	exitSystemDNSRollbackTimeout = 73
)

var errNodeDNS = errors.New("node address lookup failed")

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
	name, _ := windows.UTF16PtrFromString(`Global\26xVPN-Wintun-Worker`)
	lock, err := windows.CreateMutex(nil, false, name)
	if lock != 0 {
		defer windows.CloseHandle(lock)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return "tunnel_busy"
	}
	if err != nil {
		return "tunnel_start_failed"
	}
	input := bufio.NewReader(io.LimitReader(os.Stdin, 1<<20))
	raw, err := input.ReadBytes('\n')
	if err != nil {
		return "profile_invalid"
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = io.Copy(io.Discard, input)
		cancel()
		// Also works when Flutter crashes and graceful shutdown stalls. Production
		// profiles only configure the process-owned Wintun adapter and WFP session.
		time.AfterFunc(10*time.Second, func() { os.Exit(exitCleanupTimeout) })
	}()
	config, err := configure(ctx, raw, string(assets))
	if errors.Is(err, errNodeDNS) {
		return "node_dns_failed"
	}
	if err != nil {
		return "profile_invalid"
	}
	probes, err := xray.PingBatch(
		[]xray.PingBatchItem{{XrayJSON: config, OutboundTag: "26x-proxy"}},
		8, "https://cp.cloudflare.com/generate_204",
	)
	if err != nil {
		return "probe_execution_failed"
	}
	if len(probes) != 1 || !probes[0].Success {
		return "node_unreachable"
	}
	if ctx.Err() != nil {
		return ""
	}
	if code := startCore(ctx, config, xray.RunXray); code != "" {
		return code
	}
	result := checkDataPath(ctx)
	if ctx.Err() == nil && result == "" {
		fmt.Println("connected")
		<-ctx.Done()
	}
	if ctx.Err() != nil {
		result = ""
	}
	// The client closes its pipe on disconnect. Bound cleanup on a failed
	// startup too, even if the parent is still waiting for our result.
	watchdog := time.AfterFunc(10*time.Second, func() {
		switch result {
		case "tunnel_dns_failed":
			os.Exit(exitDNSRollbackTimeout)
		case "tunnel_data_failed":
			os.Exit(exitDataRollbackTimeout)
		case "system_dns_failed":
			os.Exit(exitSystemDNSRollbackTimeout)
		default:
			os.Exit(exitCleanupTimeout)
		}
	})
	defer watchdog.Stop()
	if err := xray.StopXray(); err != nil {
		return "tunnel_stop_failed"
	}
	return result
}

// Only a busy adapter is retried. The worker holds the single-instance mutex,
// so it cannot race a second 26x worker while Windows releases the old device.
func startCore(ctx context.Context, config string, start func(string) error) string {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return "tunnel_start_failed"
		}
		err := start(config)
		if err == nil {
			return ""
		}
		busy := errors.Is(err, windows.ERROR_BUSY) || errors.Is(err, windows.ERROR_DEVICE_IN_USE) ||
			errors.Is(err, windows.ERROR_ALREADY_INITIALIZED) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) ||
			errors.Is(err, windows.ERROR_SHARING_VIOLATION)
		if !busy {
			// Only pass known stage names or a numeric OS error to the UI.
			message := err.Error()
			switch {
			case strings.Contains(message, "unable to set routes"), strings.Contains(message, "unable to set ips"), strings.Contains(message, "unable to set metric and MTU"):
				return "tunnel_routes_failed"
			case strings.Contains(message, "unable to set DNS"):
				return "adapter_dns_failed"
			case strings.Contains(message, "outside the TUN (remove autoSystemWfpBlockLeak"):
				return "tunnel_filter_failed"
			}
			var errno syscall.Errno
			if errors.As(err, &errno) {
				return fmt.Sprintf("windows_core_%d", uint32(errno))
			}
			return "tunnel_core_failed"
		}
		if attempt == 2 {
			return "adapter_busy"
		}
		select {
		case <-ctx.Done():
			return "tunnel_start_failed"
		case <-time.After(time.Duration(attempt+1) * 400 * time.Millisecond):
		}
	}
}

func checkDataPath(parent context.Context) string {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	// Use the system resolver and ordinary application sockets after routes and
	// DNS have changed. PingBatch only checks Xray's outbound before startup.
	hosts := []string{"cp.cloudflare.com", "www.gstatic.com"}
	results := make(chan string, len(hosts))
	for _, host := range hosts {
		go func() {
			resolver := &net.Resolver{}
			// Windows can briefly answer from the previous adapter during a switch.
			first, stop := context.WithTimeout(ctx, 4*time.Second)
			ips, err := resolver.LookupNetIP(first, "ip4", host)
			stop()
			if (err != nil || len(ips) == 0) && ctx.Err() == nil {
				select {
				case <-ctx.Done():
				case <-time.After(300 * time.Millisecond):
					ips, err = resolver.LookupNetIP(ctx, "ip4", host)
				}
			}
			if err != nil || len(ips) == 0 {
				results <- "tunnel_dns_failed"
				return
			}
			dialer := net.Dialer{Resolver: resolver}
			transport := &http.Transport{DialContext: func(c context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(c, "tcp4", addr)
			}, DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/generate_204", nil)
			res, err := client.Do(req)
			if err == nil {
				res.Body.Close()
				if res.StatusCode == http.StatusNoContent {
					results <- ""
					return
				}
			}
			results <- "tunnel_data_failed"
		}()
	}
	failure := "tunnel_dns_failed"
	for range hosts {
		result := <-results
		if result == "" {
			return ""
		}
		if result == "tunnel_data_failed" {
			failure = result
		}
	}
	if failure == "tunnel_dns_failed" && parent.Err() == nil {
		// A direct query is diagnostic only. A working Windows system lookup
		// and a successful HTTPS request are still required to report connected.
		probeCtx, stop := context.WithTimeout(parent, 2*time.Second)
		defer stop()
		question := new(dns.Msg)
		question.SetQuestion(dns.Fqdn(hosts[0]), dns.TypeA)
		response, _, err := (&dns.Client{Net: "udp4"}).ExchangeContext(probeCtx, question, net.JoinHostPort(tunnelDNS, "53"))
		if err == nil && response.Rcode == dns.RcodeSuccess {
			for _, answer := range response.Answer {
				if _, ok := answer.(*dns.A); ok {
					return "system_dns_failed"
				}
			}
		}
	}
	return failure
}

func configure(ctx context.Context, raw []byte, directCIDRs string) (string, error) {
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

	// Give Windows IPv4 DNS targets. Translate their port 53 traffic into
	// DNS-over-HTTPS inside Xray so the node only needs to forward TCP 443.
	settings["dns"] = []string{tunnelDNS}
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
	var endpoint netip.Addr
	for _, item := range outbounds {
		outbound, ok := item.(map[string]any)
		if !ok || outbound["tag"] != "26x-proxy" {
			continue
		}
		peer, _ := outbound["settings"].(map[string]any)
		host, _ := peer["address"].(string)
		if host == "" {
			return "", errors.New("missing node address")
		}
		var err error
		endpoint, err = netip.ParseAddr(host)
		if err != nil {
			lookup, cancel := context.WithTimeout(ctx, 4*time.Second)
			ips, err := net.DefaultResolver.LookupNetIP(lookup, "ip", host)
			cancel()
			if err != nil || len(ips) == 0 {
				return "", errNodeDNS
			}
			endpoint = ips[0]
			for _, ip := range ips {
				if ip.Unmap().Is4() {
					endpoint = ip
					break
				}
			}
		}
		peer["address"] = endpoint.Unmap().String()
		// Preserve TLS/Reality serverName. Windows uses its existing route to
		// this exact peer even on a hotspot, Ethernet + Wi-Fi, or after roaming.
		// An omitted option enables Xray's automatic binding when routes
		// exist. On Windows that picker prefers Wi-Fi even when another
		// interface carries the peer. An explicit empty string disables it.
		settings["autoOutboundsInterface"] = ""
		break
	}
	if !endpoint.IsValid() {
		return "", errors.New("missing transport peer")
	}
	routes, err := tunnelRoutes(directCIDRs, endpoint)
	if err != nil {
		return "", err
	}
	if endpoint.Unmap() == netip.MustParseAddr(tunnelDNS) {
		return "", errors.New("node address collides with the tunnel DNS address")
	}
	// Keep this DNS address on the TUN even with overlapping LAN/VPN subnets.
	if !slices.Contains(routes, tunnelDNS+"/32") {
		routes = append(routes, tunnelDNS+"/32")
	}
	settings["autoSystemRoutingTable"] = routes
	config["dns"] = map[string]any{
		"tag":           "26x-doh",
		"queryStrategy": "UseIPv4",
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
			"port": "53", "network": "tcp,udp", "outboundTag": "26x-dns",
		},
		map[string]any{"type": "field", "inboundTag": []string{"26x-tun"}, "ip": []string{"::/0"}, "outboundTag": "block"},
	}, rules...)
	config["log"] = map[string]string{"loglevel": "none"}
	delete(config, "env")
	result, err := json.Marshal(config)
	return string(result), err
}

// Capture all traffic except direct IPv4 ranges and the tunnel's transport
// peer. Windows cannot route the connection carrying the tunnel into itself.
func tunnelRoutes(directCIDRs string, endpoint netip.Addr) ([]string, error) {
	var capture netipx.IPSetBuilder
	capture.AddPrefix(netip.MustParsePrefix("0.0.0.0/0"))
	capture.AddPrefix(netip.MustParsePrefix("::/0"))
	if strings.TrimSpace(directCIDRs) == "" {
		return nil, errors.New("empty IPv4 route list")
	}
	for line := range strings.SplitSeq(directCIDRs, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, errors.New("invalid IPv4 route")
		}
		capture.RemovePrefix(prefix)
	}
	capture.Remove(endpoint.Unmap())
	set, err := capture.IPSet()
	if err != nil {
		return nil, err
	}
	var routes []string
	for _, prefix := range set.Prefixes() {
		routes = append(routes, prefix.String())
	}
	return routes, nil
}
