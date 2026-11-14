# ADR 0002 — VPN protocol engine

**Status:** `libXray` v26.11.9 is pinned for the first integration slice. Review its license notices and the exact dependency set before distributing an IPA.

## Requirements

- Connect to the current VLESS+REALITY and Hysteria2 node types.
- Run packet routing through iOS/macOS Network Extension, Android `VpnService`, and a Windows TUN/service adapter.
- Accept a short-lived, device-authorized connection profile from the control plane; never embed a reusable subscription credential in the app.
- Report real operating-system tunnel state and stop traffic when a profile is stopped or revoked.

## Decision

Use the upstream `XTLS/libXray` wrapper around Xray-core for the first client engine. Pin release `v26.11.9` and its matching core until an explicit upgrade review. Current Xray documentation lists both VLESS and Hysteria as outbound protocols, and the Hysteria transport documentation describes the Hysteria2-compatible QUIC transport. Xray's TUN implementation documents iOS, Android, macOS, and Windows support. Build the engine from its pinned source; do not download an unverified binary into a release build.

The app sends one protocol-neutral `TunnelProfile` over its native bridge. A platform adapter adds the OS-provided TUN descriptor and builds a minimal Xray JSON config. Hysteria2 profiles use Xray's native `hysteria` outbound with version 2; VLESS profiles use the VLESS outbound with Reality. The API must validate and authorize the profile before delivery. Profile secrets must never enter logs, analytics, crash reports, or public node metadata.

This avoids the earlier sing-box/GPL integration candidate. Xray-core is distributed under MPL-2.0; `libXray` itself is MIT. Preserve both license notices, review the complete transitive dependency list, and publish modifications to MPL-covered files as required by that license. This decision is an engineering direction, not legal advice.

## Platform lifecycle

- **iOS:** `NEPacketTunnelProvider` owns the core in a Packet Tunnel extension process. The containing app requests VPN permission and installs/updates a `NETunnelProviderManager` profile. The extension builds and starts Xray only after it receives a validated profile and a Network Extension TUN descriptor. The app reports connected only from `NEVPNStatus.connected`.
- **Android:** a foreground `VpnService` requests consent, establishes the TUN descriptor and system routes, registers libXray's socket-protection callback, then starts Xray. The app reports connected only after the service and engine are running.
- **macOS:** use a Packet Tunnel Network Extension and the same profile format after the iOS path is proven.
- **Windows:** run the Xray core in a dedicated service with a signed Wintun adapter and authenticated app-to-service IPC. Do not load another independent Go runtime in the same process.

`libXray` embeds a Go runtime. Its upstream documentation warns against loading another independently built Go/cgo/gomobile runtime in the same OS process. Keep the engine in one runtime per process; the iOS Packet Tunnel extension is a separate process from the Flutter app.

## Build and release gates

- Build Android and Apple artifacts from the pinned upstream tag with its maintained build script. Record the source commit and hashes of generated AAR/XCFramework artifacts.
- The existing workspace has Command Line Tools but no full Xcode installation, Apple signing identity, Team ID, or Packet Tunnel entitlement. A signed IPA cannot be produced here until Xcode and owner signing inputs are available.
- Validate actual startup, DNS, IPv4/IPv6 routes, socket protection, sleep/wake, revocation, and traffic routing on real devices. A successful Xray config parse is not proof of an established VPN tunnel.
- Obtain legal review of the exact `libXray`/Xray-core revision, dependency notices, and Apple distribution model before TestFlight distribution.

## Sources

- [libXray upstream and build instructions](https://github.com/XTLS/libXray)
- [libXray releases](https://github.com/XTLS/libXray/releases)
- [Xray outbound protocol list](https://xtls.github.io/en/config/outbounds/)
- [Xray Hysteria outbound](https://xtls.github.io/en/config/outbounds/hysteria.html)
- [Xray TUN integration notes](https://github.com/XTLS/Xray-core/blob/main/proxy/tun/README.md)
- [Xray-core MPL-2.0 license](https://github.com/XTLS/Xray-core/blob/main/LICENSE)
- [libXray MIT license](https://github.com/XTLS/libXray/blob/main/LICENSE)
