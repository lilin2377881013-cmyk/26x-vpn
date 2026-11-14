# 26xvpn.com control-plane deployment

## Current host state (read-only preflight)

On 13 November 2026, the apex and `www` both resolved to the owner's Hong Kong server. The server is Ubuntu 24.04 with Nginx 1.24 and PostgreSQL 16 already running. TCP 80 is reachable and served the default Nginx site; TCP 443 had no listener. Hysteria2 continues to use UDP 443 and Xray VLESS+REALITY uses TCP 8443. Docker is not installed. No server files or services were changed during this preflight.

The site and API run as separate, unprivileged systemd services behind the existing Nginx. Next.js binds to `127.0.0.1:3000`, the Go API binds to `127.0.0.1:8080`, and a dedicated `26x_control` database is owned by a dedicated `26x_app` database role. The existing VPN listeners and PostgreSQL cluster remain in place.

## Live deployment status

Deployed on 13 November 2026. The production build was made on Ubuntu 24.04 x64 with Node.js 26.13.1. Nginx serves `https://26xvpn.com`; `https://www.26xvpn.com` redirects to the apex. Both names have a valid Let's Encrypt certificate expiring 11 February 2027, and the Certbot renewal timer is active.

The `26x-api` and `26x-web` systemd services are active. The API health endpoint returns healthy on loopback, the public homepage and account login page return HTTP 200, and PostgreSQL uses the dedicated `26x_control` database. The service account is unprivileged. The API and web process are not listening on public interfaces.

The owner asked to keep the admin console closed remotely for now. Nginx therefore returns HTTP 403 for `/admin` and `/api/v1/admin/*`, including the password sign-in endpoint. There is no allowlisted operator IP yet. An owner account has been provisioned. Its login details are managed outside this repository. Admin sign-in will remain unreachable until a fixed operator IP is added to both Nginx admin locations.

External verification confirmed the apex homepage and account login return 200, `www` returns 301 to the apex, and admin UI/API requests return 403. Listener verification confirmed Hysteria2 still owns UDP 443 and Xray still owns TCP 8443.

## DNS and ports

- `26xvpn.com` and `www.26xvpn.com` already resolve to this host; no DNS update is needed for the initial site deployment.
- Inbound TCP 80 is used for ACME HTTP validation and redirects. TCP 443 serves the website on the original host. The regional VPN nodes serve Xray on TCP 8443.
- The current site uses same-origin `/api/*` requests. No `api.26xvpn.com` record is required.
- VPN node hostnames and REALITY `serverName` are separate protocol settings; do not change them as part of the website deployment.

## Deployment sequence

1. Install the checksum-verified official Node.js Linux x64 runtime under `/opt/node-v26`. Do not copy a macOS Next.js standalone build onto Linux: build `web/` on the Ubuntu host so Next.js includes Linux runtime dependencies. Use `NEXT_PUBLIC_ADMIN_DEMO_MODE=false`, `NEXT_PUBLIC_SITE_URL=https://26xvpn.com`, and `API_ORIGIN=http://127.0.0.1:8080` for that production build. Cross-compile the Go API for Linux amd64 or build it on the host.
2. Create a system account named `26x` with no interactive shell, then copy source/build output, API binary, and SQL migrations into `/opt/26x`. Install the unit files from `deploy/systemd/`. Keep app files readable only by the `26x` group and keep `/etc/26x/*.env` root-owned with mode `0640`.
3. Create PostgreSQL role `26x_app` and database `26x_control` owned by that role. Use a unique URL-safe random database password; do not reuse the SSH password or the admin password.
4. Configure `/etc/26x/api.env` from `deploy/systemd/26x-api.env.example`. Keep `HTTP_ADDR=127.0.0.1:8080`, `COOKIE_SECURE=true`, the two exact HTTPS origins, and `TRUSTED_PROXY_CIDRS=127.0.0.1/32` because Nginx connects over loopback.
5. Start the API systemd unit and confirm `curl http://127.0.0.1:8080/healthz` returns healthy. The API applies its migration and creates the initial owner account on first start. Remove `INITIAL_ADMIN_EMAIL` and `INITIAL_ADMIN_PASSWORD` from the environment file after bootstrap, then restart the API.
6. Configure `/etc/26x/web.env` from `deploy/systemd/26x-web.env.example`, copy `.next/standalone` and `.next/static` to `/opt/26x/web`, and start the web unit. It listens on loopback only.
7. Install the temporary ACME Nginx host from `deploy/nginx/26xvpn.com-acme.conf.example`; run `nginx -t` and reload Nginx. Obtain the certificate with Certbot using `/var/www/letsencrypt` as its webroot.
8. Replace the temporary host with `deploy/nginx/26xvpn.com.conf.example`, validate its certificate paths with `nginx -t`, then reload Nginx. The final server redirects `www` to the apex and proxies the site/API locally.
9. Verify the homepage over HTTPS, `/api` routes, systemd restart behavior, certificate renewal timer, and that public requests to both `/admin` and `/api/v1/admin/*` receive 403.

## Admin access boundary

The project uses account/password authentication for the console. MFA is not implemented. The checked-in Nginx template therefore denies the `/admin` UI and every `/api/v1/admin/*` endpoint from all remote IPs by default. This matches the owner's current choice to keep remote admin closed. When the owner supplies a fixed public IP, add an `allow <fixed-ip>/32;` line in both admin locations and leave `deny all;` in place, then validate and reload Nginx. Do not open the admin API to the internet without the IP rule.

## Host roles

The original host serves the website, admin console, API and PostgreSQL.
The VPN data plane uses four separate VLESS + REALITY nodes in Hong Kong,
Osaka, Singapore and Los Angeles. The original host's `hk-01` node is disabled,
and its Xray and Hysteria2 listeners have been stopped. The replacement Hong
Kong node is `hk-regional-01`.

Publish the new endpoints with region codes HK, JP, SG and US. Apply the
[regional routing configuration](../deploy/xray/README.md) to their client
inbounds. Device credentials are issued or assigned through the control
plane. Public registration, billing and MFA are pending.
