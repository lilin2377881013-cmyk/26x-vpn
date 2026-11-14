# Development node check — 2026-11-13

This was a read-only check of the development data-plane host after the client screenshot showed Hysteria2 healthy and VLESS+REALITY in an error state. No service, firewall, database, or configuration was changed. The host address and all VPN credentials are intentionally omitted.

## Observed

| Check | Observation |
|---|---|
| Hysteria service | `hysteria-server` is active; UDP 443 listens on all interfaces. |
| Xray service | `xray` is active; its inbound is VLESS over TCP 8443 with REALITY enabled. |
| REALITY metadata | The active inbound has one configured client, one server name, and one short ID. Values are intentionally not recorded here. |
| TCP reachability | TCP 8443 accepted a connection from the development workstation. TCP 443 did not, which is expected for a UDP-only Hysteria listener. |
| Database/DNS scope | PostgreSQL TCP 5432 and DNS port 53 are bound to loopback. |
| Recent Xray log scan | No lines matching REALITY/handshake/failure terms appeared in the preceding hour. This does not prove that the app sent a handshake during that interval. |

## What this says about the screenshot

The screenshot's Reality error is not explained by a stopped Xray service or a TCP 8443 port that is unreachable from the workstation. The server-side REALITY inbound exists and is listening. This narrows the next check to the client profile and the actual probe/handshake result; it does not establish the exact cause by itself.

Compare the profile entry against the active server parameters for the same node: address, port `8443`, UUID, VLESS flow, TCP transport, REALITY server name, public key, short ID, and client fingerprint. The public key, server name, short ID, and UUID must be taken from the matching active inbound/client entry; do not copy any of those values into this project or a public ticket. Also check whether the app's `Error` badge reports a latency probe failure or a real tunnel attempt. If a connection is attempted again, inspect only the sanitized Xray error code at that time; the prior-hour scan had no matching log lines.

## Boundary

The server was not redeployed, restarted, or edited. The observation that a socket listens on all interfaces is not an AWS security-group audit. PostgreSQL and DNS were observed on loopback; external cloud firewall rules were not inspected. Re-run these checks after any later server or firewall change.
