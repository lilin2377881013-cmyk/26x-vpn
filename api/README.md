# 26x Control Plane API

The Go API is the control plane. It owns account/session checks, admin operations, node metadata, and the credential-reconciliation boundary. Versioned SQL files in `migrations/` are applied in filename order under a PostgreSQL advisory lock and recorded in `schema_migrations`.

## Bootstrap

1. Copy the root `.env.example` to `.env` and replace both secrets.
2. Start `db` and `api` with Compose.
3. The first startup creates the schema and one owner account when `INITIAL_ADMIN_EMAIL` and `INITIAL_ADMIN_PASSWORD` are set. Remove those bootstrap variables after the first successful start.
4. The API listens on loopback in the local Compose setup. Put it behind the 26x web origin or a TLS reverse proxy before remote use.

## Implemented endpoints

- `GET /healthz`
- `POST /api/v1/admin/session`, `GET /api/v1/admin/me`, `DELETE /api/v1/admin/session`
- `GET /api/v1/admin/overview`
- `GET/POST /api/v1/admin/users`, `PATCH /api/v1/admin/users/{id}/status`
- `GET /api/v1/admin/devices`, `DELETE /api/v1/admin/devices/{id}` (revoke)
- `GET/POST /api/v1/admin/plans`, `POST /api/v1/admin/users/{id}/subscriptions`
- `GET/POST /api/v1/admin/nodes`
- `POST /api/v1/admin/devices/{id}/node-credentials` (assign an existing protocol credential to a device)
- `PATCH /api/v1/admin/nodes/{id}/enabled` (disable or return to provisioning)
- `PATCH /api/v1/admin/nodes/{id}/health` (record a verified node check)
- `GET /api/v1/admin/audit`
- `POST /api/v1/auth/session`, `GET /api/v1/me`, `GET /api/v1/me/plans`, `POST /api/v1/me/password`, device registration/revocation, and client node bootstrap

Admin sessions use a random, server-side session token in an HttpOnly cookie and a double-submit CSRF token for writes. Passwords use Argon2id. Login attempts are rate limited per direct peer address. When a trusted reverse proxy sits in front of the API, set `TRUSTED_PROXY_CIDRS` to only that proxy's addresses; the API ignores forwarded IP headers from all other peers. Set `COOKIE_SECURE=true` behind HTTPS and keep `TRUSTED_ORIGINS` exact.

## VPN credential delivery

Set `CREDENTIAL_ENCRYPTION_KEY` to a base64-encoded 32-byte key in the API
environment. Provision a working credential on the VPN node first, then use
the private admin API with the device ID, `endpointId`, and `credential`.
For VLESS+REALITY, supply the matching client `fingerprint` and, where
required, `flow`. The endpoint record supplies the address, port, server name,
public key, and short ID. The operation encrypts the credential in PostgreSQL
and does not change the node's inbound configuration.

An authenticated account with an active subscription receives the connection
profile for its active device from `GET /api/v1/client/bootstrap` when the
endpoint is enabled and the node is online or degraded. After verifying an
actual data-plane request, use the private health endpoint with
`{"status":"online"}`. Use `degraded` or `offline` if the check warrants it.
Do not distribute a
shared administrative or legacy profile to customer devices. Revoking the
device stops delivery. Revoke the assigned protocol credential on the node to
stop reuse of a previously delivered credential.

## Not ready for public production

- MFA enrollment and verification are not implemented. The bootstrap owner is created with MFA disabled for local development; do not expose the console publicly until MFA is implemented and enabled.
- User registration, email verification, password recovery, and billing are not implemented.
- The plan catalog supports the `standard` and `premium` tiers, but checkout and payment processing are not implemented; users can view active plans while administrators assign subscriptions.
- Automatic provisioning and revocation on the VPN node, usage ingestion, production observability, and short-lived credentials remain planned work.
