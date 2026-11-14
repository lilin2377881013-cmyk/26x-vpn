# 26x delivery roadmap

This plan separates the reviewable product shell from the work required for a real, supportable VPN release. No milestone should report a tunnel as connected until the operating system confirms it.

## Milestone 0 — Reviewable development workspace

**Status: client and API scaffold implemented; public development-preview site deployed on 13 November 2026.**

- Brand site, customer account pages, and draft legal/support pages.
- Go/PostgreSQL control-plane scaffold for users, subscriptions, devices, node metadata, admin sessions, and audit events.
- Admin console for overview, users, device revocation, plans, nodes, and audit history; the customer portal supports sign-in, account/device review, device revocation, and password changes.
- `26xvpn.com` is live over HTTPS; `www` redirects to the apex. The admin UI/API remain denied publicly until an operator IP is supplied.
- Shared Flutter account/device/node UI and generated iOS/macOS/Windows/Android app runners, with native tunnel reference bridges.
- Dart dependencies resolve and `flutter analyze` is clean; no platform-native build has yet been validated.
- Fail-closed behavior: no protocol engine is bundled and bootstrap marks nodes unavailable.
- Authenticated users can change their password; the API revokes all active user sessions after a change.

## Milestone 1 — Release and security decisions

1. Review the pinned `libXray` v26.11.9 release, MPL-2.0/MIT notices, transitive dependency list, and source-distribution obligations in [`adr/0002-vpn-engine-selection.md`](adr/0002-vpn-engine-selection.md) before distributing a client.
2. Confirm whether to keep `26xvpn.com` on the Hong Kong host for a private beta or move the web/control plane to a separate host. Choose service regions, support workflow, privacy retention policy, and whether payments are in scope.
3. Implement administrator MFA, recovery procedures, verified email/password reset, and an account/device deletion path.
4. Define trusted proxy CIDRs, TLS termination, rate limits, secrets management, encrypted backups, restore drills, and monitoring.
5. Replace draft privacy and terms text with the actual data flows, retention schedule, service regions, and support contact.

**Exit gate:** no public production deployment until MFA, operational recovery, privacy text, and secrets handling are ready.

## Milestone 2 — Node control and credential lifecycle

1. Specify a node-agent protocol with mutual authentication, signed desired state, replay protection, key rotation, and a narrow firewall surface.
2. Add health checks that separate listener reachability, protocol handshake, and outbound reachability.
3. Implement envelope encryption for per-device protocol credentials with a key held outside PostgreSQL.
4. Implement idempotent provisioning and revocation, reconciliation status, and safe retries.
5. Bind each installation to a device proof-of-possession key. Treat a client-supplied device ID as a lookup hint only, never as authorization.
6. Extend client bootstrap to return a short-lived device-bound configuration only after account, subscription, device proof, node, and credential checks pass.

**Exit gate:** an integration environment can create, connect, revoke, and verify removal of one device credential without exposing node private keys or writing traffic destinations to logs.

## Milestone 3 — iOS reference release and signed IPA

1. Pin a legally approved protocol engine and integrate it into the packet-tunnel extension.
2. Add the Network Extension target, provider configuration, on-demand/lifecycle behavior, reconnect handling, DNS/routes, and system state reporting.
3. Add a signed-in user flow, permission explanation, account deletion, privacy disclosures, diagnostics, and localized failure guidance.
4. Configure the owner's Team ID, bundle identifiers, App Store Connect record, distribution identity, profiles, and Packet Tunnel entitlement.
5. Build an archive, export the signed IPA, record commit/core/build versions and SHA-256, and retain dSYMs.

The agreed handoff is the signed `.ipa`; the owner performs App Store Connect upload, TestFlight review submission, and tester management. Apple's current [VPN app review rules](https://developer.apple.com/app-store/review/guidelines/) require the appropriate Network Extension API and organization account enrollment. See [TestFlight overview](https://developer.apple.com/help/app-store-connect/test-a-beta-version/testflight-overview/).

**Exit gate:** install the uploaded build on a clean test device, grant VPN permission, connect to the controlled test node, verify OS tunnel state and traffic routing, then revoke the device and confirm the session stops.

## Milestone 4 — macOS and Android parity

- macOS: package a separate Network Extension target, apply the required signing/notarization setup, and verify sleep/wake and user switching.
- Android: implement `VpnService` permission and foreground-service lifecycle, always-on behavior where supported, and DNS/route handling.
- Reuse the same backend credential policy and sanitized diagnostics; never reuse a device credential across platforms.

**Exit gate:** repeat the clean-install, connect, reconnect, revoke, and upgrade scenarios on the supported OS matrix.

## Milestone 5 — Windows and service launch

- Implement and sign the Windows TUN/service integration, authenticated app-to-service IPC, upgrade/rollback, and uninstall cleanup.
- Complete support runbooks, node capacity alerts, release rollback, incident response, and restore drills.
- Publish installers, privacy/terms, support details, versioned release notes, and a public service status page.

**Exit gate:** the owner approves production readiness after release artifacts, operations, support, privacy, and service recovery have all been reviewed.

## Current external dependencies

- Apple Developer organization account, Team ID, bundle identifiers, signing identity/profiles, and Packet Tunnel entitlement.
- A macOS host with full Xcode. The current workspace host only has Command Line Tools, which cannot run `xcodebuild` archives.
- A legal decision on the VPN engine license and the product's source-distribution model.
- Owner-provided Apple Developer organization, Team ID, bundle IDs, signing assets and Packet Tunnel entitlement; full Xcode is also needed to build an IPA. Privacy/support/terms content still needs approval.
