# VLESS device credential management

Use `device-user.py` on an Xray node with a named VLESS inbound and
`HandlerService` enabled on a permission-restricted Unix socket. Grant socket
access only to the service that manages the node. Use a private directory and
socket mode 0600 for a helper running as the Xray service user.

The command reads the device UUID as an argument and the VLESS credential as
JSON on standard input. Supply the plaintext from the authorized provisioning
process. The credential never appears in command arguments or the helper's
output. For example, with `vless-26x` as the configured inbound tag:

```sh
./device-user.py --socket /run/xray/26x-control.sock --tag vless-26x ensure DEVICE_UUID < PRIVATE_CREDENTIAL_JSON
./device-user.py --socket /run/xray/26x-control.sock --tag vless-26x revoke DEVICE_UUID
```

The JSON has a `credential` UUID and an optional `flow`. The default flow is
`xtls-rprx-vision`. Repeating an operation is safe once the runtime has reached
the requested state. An existing credential mismatch returns an error.

These operations apply to the running Xray instance. The control-plane API can
manage one VLESS endpoint on the same host using the settings in
`../systemd/26x-api.env.example`. Install this helper at
`/opt/26x/api/device-user.py` and grant the API service group access to the
socket with a restricted group and mode 0660. Use a dedicated group for the
socket and keep the API's PostgreSQL and encryption secrets in its environment
file. Configure the selected VLESS inbound for `xtls-rprx-vision`.

PostgreSQL stores encrypted managed credentials. The API reconciles them
periodically, including after Xray restarts. Existing manually assigned
credentials are outside this reconciliation.

The unmodified Xray build rejects fresh connections after revocation but can
keep established streams open. For managed devices, apply
`vless-close-revoked-connections.patch` to Xray core commit `676eaf66f8f0` and
build the node executable. The patch closes established connections for the
removed named user. A local comparison verified that a second named user's
connection could still transfer data after the first was removed. Apply the
patch only to the node build and test it before switching the live service.

Set `MANAGED_VLESS_REQUIRE_LEASE=true` only after installing the patched
node and a client that requests and renews connection leases. For the
managed endpoint the API keeps a slot occupied while the node still has
its previous credential, including after release or expiry. It frees the
slot after reconciliation confirms removal. Manually configured users
must be migrated out of the node's static credentials to enforce the
same limit for those users.
