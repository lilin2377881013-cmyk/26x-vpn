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

## Hong Kong and Japan egress

[The routing example](hk-split-routing.json.example) sends OpenAI domains from
the managed VLESS inbound to a Japan VPS. Remaining traffic from that inbound
uses Hong Kong's own connection. It assumes a dedicated VLESS over TLS
listener on the Japan VPS with a valid certificate for its hostname.

Replace the example address, port, TLS name and UUID with the Japan listener's
actual values. Keep the UUID in the deployed config. Add the two outbounds to
the existing Hong Kong configuration. Add the two rules after any existing
control or policy rules that must take precedence and before any general
catch-all rule. Match the managed inbound's actual tag in both rules. Keep
other inbounds, outbounds and API routing in place.

Enable the following on the managed Hong Kong VLESS inbound so DNS-resolved
connections can match their HTTP Host or TLS server name. Sniffing only
supplies a domain for route selection.

```json
"sniffing": {
  "enabled": true,
  "destOverride": ["http", "tls", "quic"],
  "routeOnly": true
}
```

Run `xray run -test -config /path/to/merged.json` before restarting. Then
compare the IP reported by the ChatGPT or OpenAI trace endpoint with the
IP reported by a neutral IP-check service through the same client. Keep
Japan's hostname and TLS certificate consistent. Traffic without an
observable hostname follows the Hong Kong rule.

The example covers OpenAI-operated domains used for chat, login, API and
content. Verify any additional service dependencies with
[OpenAI's network guide](https://help.openai.com/en/articles/9247338-network-recommendations-for-chatgpt-errors-on-web-and-apps).
