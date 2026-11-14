# 26x VPN

26x is a cross-platform VPN product for iOS, macOS, Windows, and Android, with an official site and an operations console.

## Project layout

- `client/` — Flutter app and native VPN integration points.
- `web/` — 26x public site, customer account area, and administration console.
- `api/` — control-plane API and database migrations.
- `docs/` — product requirements, architecture, engine/platform decisions, and release notes.

## Current delivery boundary

- A signed macOS preview has been tested with live account login and the Hong Kong VLESS+REALITY node. The iOS source is present and a signed `.ipa` is still pending. The owner handles TestFlight distribution.
- The public site is live at `https://26xvpn.com`; `www` redirects to the apex. The site and control-plane API share the existing Hong Kong development host with Hysteria2 and Xray VLESS+REALITY. Deployment status and the admin access boundary are recorded in `docs/deploy-26xvpn-com.md`.
- Never commit VPN credentials, Apple signing assets, admin passwords, subscription tokens, or production environment files.

## Recommended first milestone

Deliver a vertical slice: user and device records, one managed node, per-device configuration issuance, a responsive public site/admin console, and a client shell that reports real tunnel state. Platform VPN tunnel integrations must report an error until their native core is installed; the UI must not simulate an active connection.

## Current scope

- The client now has Flutter runners for all four requested platforms and an account/device/node shell; it only reports connected when the native adapter reports a running tunnel.
- The macOS tunnel uses libXray and managed per-device credentials. The connection lease migration and API implementation are in this repository, with production rollout pending. The native integrations on the remaining platforms still need end-to-end verification.
- The website includes a product landing page, account sign-in and device/password management, and an admin console with demo/live API modes. The production API and PostgreSQL service are running on the HK host; public account registration and billing are not implemented.
- The admin console uses password authentication, but Nginx currently returns 403 for the console and every admin API route because the owner asked to keep remote admin closed. No IP is allowlisted yet.
- Privacy and terms pages are published as drafts and need product/legal review before commercial onboarding.

## Local requirements

- Node.js 26 LTS for the web workspace.
- Go 1.26 for the control-plane API.
- Flutter stable for Android, iOS, macOS, and Windows client development.
- Xcode with the Network Extension capability and the owner's signing assets for the iOS IPA.

## Development workflow

- Use a branch and pull request for each independent change so client, API, and web work can proceed in parallel. Start with [the architecture](docs/architecture.md) and the relevant component README.
- Generate native libXray dependencies with [the build script](client/scripts/build_libxray.sh). From `client/`, run `./scripts/build_libxray.sh macos` for macOS. See [the macOS setup](client/platforms/macos/README.md) for local signing.

## Local web/API preview

1. Copy `.env.example` to `.env`. Generate a random PostgreSQL password and use the same value in `POSTGRES_PASSWORD` and `DATABASE_URL`; set a separate random admin password of at least 16 characters.
2. Run `docker compose up --build` from this directory.
3. Open `http://localhost:3000` for the site, `/account/login` for the user portal, and `/admin` for the demo console. Demo mode does not persist writes. The API is bound to `127.0.0.1:8080`.

This Compose profile is for local review. The production single-server deployment and current domain/admin status are in `docs/deploy-26xvpn-com.md`.

See `docs/ios-ipa-delivery.md` before producing an iOS archive.

For the owner's registered domain and single-server deployment template, see `docs/deploy-26xvpn-com.md`.

For the read-only diagnosis of the existing Hysteria2/VLESS+REALITY development node and the screenshot's Reality error, see `docs/node-diagnostics-2026-11-13.md`.
