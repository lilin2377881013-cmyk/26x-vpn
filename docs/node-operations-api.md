# Node operations API and script contract

The API listens on `http://127.0.0.1:8080`. Admin reads require an admin
session cookie. Admin mutations also use the existing `X-CSRF-Token`.
Owner and operations can mutate nodes. Support can read nodes and usage.

| Method and path | Behavior |
| --- | --- |
| `POST /api/v1/admin/nodes` | Run `26x-node add` and return 201 with `{ok, node}` after the node is registered. |
| `GET /api/v1/admin/nodes` | Run `26x-node list --json` and return its JSON. |
| `GET /api/v1/admin/nodes/{code}` | Return `{node}` with endpoint metadata and the active credential user count. |
| `PUT /api/v1/admin/nodes/{code}` | Save `name`, `region_code` and `capacity_limit` and return `{ok, node}`. Code and endpoints identify the existing deployment. |
| `DELETE /api/v1/admin/nodes/{code}` | Run `26x-node remove CODE --json` in up to 60 seconds. |
| `GET /api/v1/admin/nodes/{code}/health` | Run `26x-node health --node CODE --json` in up to 60 seconds. |
| `GET /api/v1/admin/nodes/{code}/traffic` | Run `26x-node traffic --node CODE --json`. |
| `POST /api/v1/admin/nodes/{code}/pause` | Run `26x-node pause CODE --json`. |
| `POST /api/v1/admin/nodes/{code}/resume` | Run `26x-node resume CODE --json`. |
| `GET /api/v1/admin/nodes/{code}/stats` | Run `26x-node traffic --node CODE --json` and return its totals plus up to 30 days of stored usage. |
| `GET /api/v1/admin/users` | Return `{items, limit, offset, hasMore}`. Supports `q`, `limit` and `offset`. |
| `GET /api/v1/admin/users/{id}/usage` | Return `{user, days, bytes_up, bytes_down, daily}`. Defaults to 30 days and accepts `?days=1` through `90`. |

The existing metadata-only form uses `POST /api/v1/admin/nodes/register`.
The existing overview uses `GET /api/v1/admin/nodes/records` for database
records. The new node list expects `list --json` to return `{ok, nodes:[...]}`
with `code`, `name`, `region_code`, `status`, `users` and `capacity_limit`
per row. `healthCheckedAt` is optional.

## Script I/O

The executable defaults to `/opt/26x-node/bin/26x-node`, overridable via
`NODE_COMMAND`. The API runs it as its system user and passes arguments
directly. No shell interprets the arguments.

- Deployment JSON accepts `code`, `name`, `region_code`, `host`,
  `ssh_port`, `ssh_user`, `ssh_password`, `capacity_limit` and `port`.
  The command is `add --code CODE --name NAME --region REGION --host HOST --ssh-pass PASSWORD --capacity COUNT --json`.
  Nondefault ports or SSH users add `--ssh-port`, `--ssh-user` or `--port`.
  Defaults are SSH port 22, SSH user root and VPN port 8443.
  The script registers its deployment in `nodes` and `node_endpoints`.
  The API waits up to 180 seconds. Concurrent API deployments return 409.
- The password is sent through the JSON request body, then passed to the
  CLI through its specified argument. It is excluded from API logs, database
  records, audit and responses. The script owns its mode-0600 vault file.
- Successful script results must exit zero and emit a JSON object with
  `"ok": true`. Run failures or `"ok": false` return an API error without
  returning raw script logs. Commands other than add may return their
  result fields directly.
- `remove` owns node retirement. `pause` and `resume` set the runtime
  and database states used by the client.
- `traffic --node CODE --json` reads the latest snapshot and returns
  `ok`, `node`, `users`, `traffic_used_gb` and `traffic_limit_gb`
  (nullable). The cron collection writes daily totals.

Migration 007 adds `nodes.ssh_port`, `nodes.ssh_user` and
`node_usage_daily(node_id, usage_day, bytes_up, bytes_down)`. Use byte
totals per UTC day and upsert by `(node_id, usage_day)`. User and device
totals use the existing `usage_daily` with `(device_id, usage_day)`.
Entries returned in `daily` contain `day`, `bytes_up` and `bytes_down`.
An absent day means no sample was stored. Node capacity counts users or
devices according to the operator's deployment sizing and is separate from
a bandwidth quota.

## Client endpoints and credentials

`GET /api/v1/nodes?deviceId=UUID` returns the active subscription's node
list in `items` for an owned, unrevoked device. The older bootstrap
endpoint continues to return the same payload.

`GET /api/v1/devices/UUID/credentials` returns
`{items: [{endpointId, connectionProfile}], credentialsProvisioned, connectionLeases}`.
Only profiles usable by that device are included. Both endpoints require
the signed-in user's session.

For `grant`, create a distinct VLESS UUID per device credential and
activate it on the node before marking the database record active. An
authenticated admin can store it through
`POST /api/v1/admin/devices/{id}/node-credentials` with
`endpointId`, `credential`, `flow` and `fingerprint`. If the script writes
directly, use the existing AES-256-GCM format. The `v1` ciphertext is the
12-byte nonce followed by ciphertext and tag. Associated data is
`26x/node-credential/v1/DEVICE_UUID/ENDPOINT_UUID`. Plaintext is JSON with
`credential`, `flow` and `fingerprint`. Store encrypted bytes in
`credential_ciphertext` and set `key_version='v1'`. The raw UUID is never
a database column value. For revoke, remove the credential from Xray and
mark the record revoked. Preserve managed rows owned by the API reconciler.

The four initial endpoints have a profile for the registered macOS test
device. Independent credentials on the regional nodes are issued by the
operator's grant script.
