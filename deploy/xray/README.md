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

## Regional nodes and egress routing

Publish actual endpoints under region codes `HK`, `JP`, `SG`, and `US`
in the admin console. Each published endpoint needs health checks and
per-device credentials before users can connect. The Flutter client groups
the endpoints by region and shows any region without an endpoint as pending.
The current automatic credential reconciler manages one endpoint. Assign
device credentials for additional endpoints through the admin console until
multi-node reconciliation is available.

Apply [the routing example](region-split-routing.json.example) to the
managed VLESS ingress of each region. It has three ordered rules:

1. OpenAI domains use the Japan egress.
2. Streaming domains in the list use the US egress.
3. Remaining traffic from that ingress uses the selected region's local egress.

The example uses VLESS + REALITY with Vision for the Japan and US relays.
Set each relay's address, port, SNI, public key, short ID and dedicated UUID
in the deployed config. Keep relay credentials outside
Git. On the Japan ingress, point the OpenAI rule at `region-direct` and
omit the `jp-openai` outbound. On the US ingress, point the streaming rule
at `region-direct` and omit the `us-streaming` outbound. Route relay traffic directly at its destination. Use a separate inbound tag
for each relay listener or an early rule matching its dedicated VLESS user.

Add the applicable outbounds to the existing configuration. Place the
three rules after any control or policy rules that must take precedence
and before a general catch-all rule. Match the actual client inbound tag
in each rule. Preserve existing API routes and outbounds.

Enable sniffing on the client ingress so DNS-resolved traffic can match
the HTTP Host, TLS server name, or QUIC server name visible to Xray.

```json
"sniffing": {
  "enabled": true,
  "destOverride": ["http", "tls", "quic"],
  "routeOnly": true
}
```

The macOS IPv4 exclusions take precedence before traffic reaches Xray.
The streaming policy covers known service and CDN domains. Add new domains
to the US rule as needed. Traffic whose destination cannot be identified
as streaming follows the selected region's default route. Routing a
service through the US does not establish that its account or subscription
has playback rights there.

Validate the merged configuration with
`xray run -test -config /path/to/merged.json`. Then verify the selected
outbound in Xray access logs and run a playback check for each streaming
service of interest. The OpenAI rules cover chat, login, API and content
domains. See [OpenAI's network guide](https://help.openai.com/en/articles/9247338-network-recommendations-for-chatgpt-errors-on-web-and-apps)
for additional dependencies.
