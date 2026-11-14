# ADR 0001 — Initial platform stack

**Status:** Provisional; the native-engine and license spikes are release gates.

## Decision

- Flutter for a shared four-platform client UI.
- sing-box is the first engine candidate for VLESS+REALITY and Hysteria2.
- Native OS VPN adapters own packet routing and lifecycle.
- Go API + PostgreSQL for account and node control.
- Next.js App Router for 26x public pages and the admin console.

## Reasons

- The app has four target platforms and needs consistent account, node, and connection-state UX.
- The existing node uses VLESS+REALITY and Hysteria2; a multi-protocol core avoids maintaining two independent protocol stacks.
- Native VPN frameworks remain platform-specific; a shared visual layer does not erase OS entitlement or lifecycle requirements.
- A modular monolith keeps the first release operable. The data-plane nodes remain separately deployable.

## Consequences

- The MVP needs Swift, Kotlin, and Windows-native integration work in addition to Dart.
- iOS/macOS builds require the Network Extension capability and Apple signing assets.
- The core integration must be checked against the upstream license and API for every pinned release.
- See [`0002-vpn-engine-selection.md`](0002-vpn-engine-selection.md) for the current protocol-core alternatives and decision gate.
- The website/admin and API will run in separate processes with explicit CORS and authentication boundaries.

## Revisit when

- The engine cannot be distributed under acceptable terms.
- Native packaging makes a reliable Flutter bridge impractical on one or more platforms.
- Production scale requires a separate billing or provisioning service.
