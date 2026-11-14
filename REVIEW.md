# 26x VPN — review notes

**Review package:** 13 November 2026  
**Brand:** 26x  
**Release state:** public development-preview site is deployed; the VPN service is not production-ready.

## Delivered in this workspace

- Responsive 26x public site for `26xvpn.com`, with clearly marked development status and draft privacy/terms pages.
- Customer account pages for sign-in, account/subscription status, registered-device review/revocation, and password changes.
- Go/PostgreSQL control-plane source for admin sessions, users, plans, subscriptions, devices, node metadata, and audit history.
- Next.js admin console with demo mode, live API mode, user status changes, plan assignment, device revocation, node/plan creation, and audit view.
- Flutter 3.50 client source and generated runner projects for iOS, macOS, Windows, and Android; the shared app covers sign-in/password change, secure session/device-ID storage, device registration/revocation, node display, and OS tunnel-state plumbing.
- iOS/macOS Network Extension templates and Android tunnel bridge/service templates. The runners do not yet contain a working protocol engine; all platforms stay disconnected/unavailable.
- Architecture, product requirements, release roadmap, protocol-engine/license options, and signed IPA handoff documentation.
- Read-only development-node diagnostics documenting the active Hysteria2 and VLESS+REALITY services and the evidence gathered for the screenshot's Reality error.
- Live website deployment at `https://26xvpn.com`, with Let's Encrypt TLS, a PostgreSQL-backed control-plane API, and systemd-managed services. The admin console and admin API are denied from all public IPs while the owner keeps remote access closed.

## Verified locally

- Go control-plane binary compiles with Go 1.26. On the Ubuntu host, the API service applied the migration, created the owner admin record, and returned a healthy response on its loopback health endpoint.
- Next.js site, customer account pages, and admin console pass the production build and TypeScript phase.
- The production site was rebuilt on Ubuntu 24.04 x64 with Node.js 26.13.1 and responds over public HTTPS: apex homepage and account login return 200, `www` redirects to apex, and `/admin` plus admin API requests return 403. Nginx and both app services are active.
- A Let's Encrypt certificate for both hostnames is valid until 11 February 2027; the Certbot automatic renewal timer is active.
- Runtime listener review confirmed that the site and API bind to loopback, Hysteria2 still uses UDP 443, and Xray still uses TCP 8443.
- `npm run lint` passes. `npm audit --omit=dev` reports no production dependency vulnerabilities. The full tree has 7 moderate advisories in transitive, development-only TypeScript ESLint/minimatch packages; `npm audit fix` made no lockfile changes, so no major-version override was applied without validation.
- Local browser review covered the homepage, account sign-in screen, and admin demo dashboard at the available narrow viewport. The production admin endpoints are blocked at Nginx as requested; no customer user account exists yet, so customer sign-in and subscription flows remain unverified.
- iOS/macOS extension entitlement and Info.plist templates pass `plutil -lint`.
- `flutter pub get`, `dart format`, and `flutter analyze` pass with Flutter 3.50; the analyzer reports no issues. Native platform builds were not run.
- The workspace has no full Xcode installation and reports zero valid code-signing identities. No Apple Team ID, app record, provisioning profiles, or Packet Tunnel entitlement were provided.

## Items that block a real VPN release

1. Select the protocol-engine route and complete the license/third-party review before bundling it.
2. Implement the node agent, encrypted per-device credentials, device proof-of-possession, health checks, and credential reconciliation.
3. Implement OS-specific tunnel behavior and package runners for iOS, macOS, Android, and Windows.
4. Implement administrator MFA and production account recovery/verification.
5. Add a fixed operator IP to the Nginx admin allowlist before remote admin access is needed; implement administrator MFA and finalize privacy/support/terms content before commercial onboarding.

The current API deliberately marks every node as not connectable. A successful button press or fake tunnel state is not presented as a working VPN connection.
Xray is active and TCP 8443 was reachable during a read-only check. See [`docs/node-diagnostics-2026-11-13.md`](docs/node-diagnostics-2026-11-13.md); the exact profile mismatch remains unproven because the active subscription entry was not compared.

## Important boundary

The VPN server remains a development data-plane node. The deployed site is a public development preview, not evidence that the VPN clients or tunnels are ready for use. The Compose profile binds the API and site to localhost and is for local review.

See [`docs/roadmap.md`](docs/roadmap.md) for milestone gates, [`docs/deploy-26xvpn-com.md`](docs/deploy-26xvpn-com.md) for the domain/server handoff, and [`docs/ios-ipa-delivery.md`](docs/ios-ipa-delivery.md) for the IPA handoff requirements.
