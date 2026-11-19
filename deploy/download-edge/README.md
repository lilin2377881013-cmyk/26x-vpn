# Website download mirror

The original `https://26xvpn.com/downloads/<versioned-file>` URLs remain public
and stay in the administration database. Healthy published packages receive a
temporary, non-cacheable redirect to `https://94.103.6.235/downloads/<file>` on
the existing Hong Kong CN2 server. The mirror serves static files directly;
traffic does not return through the website host. Client applications do not
change. Referral click tracking still runs before the download redirect.

This improves the available download route; it is not a geographically
distributed CDN or a guaranteed speed for every mainland ISP. The mirror shares
the node's bandwidth with VPN traffic. No new hosting subscription is required.

## Mirror installation (Hong Kong node)

- Keep Xray and its existing port 8443 unchanged. Use free HTTP/HTTPS ports 80/443.
- Install Nginx, Python 3.11 and a dedicated Certbot 5.4.0 virtual environment at
  `/opt/26x-download-certbot`. Do not replace the OS Python.
- First configure the HTTP server from `edge.conf.example` only, with challenge
  root `/var/www/26x-acme`, and verify it is reachable externally.
- Obtain the certificate (root, no email credentials required):

  ```sh
  /opt/26x-download-certbot/bin/certbot certonly --non-interactive --agree-tos \
    --register-unsafely-without-email --preferred-profile shortlived \
    --webroot --webroot-path /var/www/26x-acme \
    --ip-address 94.103.6.235 --cert-name 26x-download-edge
  ```

- Install the full `edge.conf.example` as `/etc/nginx/conf.d/26x-downloads.conf`,
  validate and reload Nginx. Restore SELinux contexts on `/var/www/26x-downloads`.
- Install and enable `26x-download-certbot-renew.{service,timer}` on the mirror.
  The timer checks renewal every six hours and reloads Nginx after renewal.
  The systemd timer supplies scheduling jitter; Certbot's additional random
  sleep is disabled so it cannot consume the service timeout. Test with
  `certbot renew --dry-run --no-random-sleep-on-renew` and the same deployment hook.

IP certificates require the short-lived profile and automatic renewal. See the
[Let's Encrypt Certbot instructions](https://letsencrypt.org/2026/03/11/shorter-certs-certbot).
HTTPS certificate verification is never disabled for downloads or health checks.

## Origin installation (website/control-plane host)

- Install `sync.py` as `/opt/26x-download-edge/sync.py`.
- Create `/etc/nginx/26x-download-edge-routes.conf` with a comment only. In the
  active HTTPS `26xvpn.com` server block, before the existing `/downloads/`
  location, add `include /etc/nginx/26x-download-edge-routes.conf;`.
  Keep the existing static-file location as the fallback. Back up the active
  Nginx configuration outside `sites-enabled` before editing.
- The script uses the existing private root SSH vault
  `/opt/26x-node/state/.vault-hk-regional-01` and pinned known-hosts file
  `/opt/26x-node/state/bandwidth-known-hosts`. Credentials stay on the server;
  never put them into the repository. SSH host verification remains enabled.
- Run one synchronization and verify all three current platform packages.
- Install and enable `26x-download-edge-{sync,health}.{service,timer}` here.

Synchronization reads the three download channels from PostgreSQL, validates
same-origin filenames, verifies SHA-256, and uploads changed files atomically.
New releases are mirrored within five minutes; until then their original URLs
continue serving the origin files. Old filenames still work at the origin.

Every minute the health job verifies a trusted TLS certificate with at least one
day remaining, file lengths, attachment headers, and byte-range support. On
failure it removes the redirects so subsequent requests use origin downloads.
Only completed, healthy packages are redirected. A failure between health
checks can affect requests until the next check; an already redirected active
download cannot move between servers automatically.

## Verification and rollback

- Check exact file hashes at both hosts, trusted HTTPS, original-link redirects,
  range requests and resumed bytes. Record the actual test location; VPN tests
  do not establish mainland direct-download performance.
- Exercise fallback by temporarily changing a manifest file length and running
  `sync.py --health-only`; the original URL should then serve the source file.
  Restore the manifest and run a successful check to restore redirects.
- To roll back, stop both origin timers and services; replace the route include
  with a comment, then run `nginx -t` and reload. Original links and packages
  remain available. Keep certificate renewal enabled while the mirror is live.
