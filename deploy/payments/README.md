# USDT checkout operations

The design and acceptance checklist are in `docs/usdt-checkout-plan.md`.
The feature is disabled by default. No client changes are needed. Existing
Alipay checkout and previously issued cashier tickets keep their behavior when
USDT acceptance is off.

## Configuration

Copy `usdt.env.example` into a private server environment file (mode 600). Ensure `TRUSTED_ORIGINS` includes the same HTTPS origin as
`USDT_PUBLIC_ORIGIN`. Provide
a checksum-valid TRON mainnet receiving address, your own TronGrid chain-data API
key, and explicit fixed USDT plan prices. No wallet private key or mnemonic is
used anywhere in this service. The example prices are the selected release prices; Both checkout choices are enabled after the provider restored Alipay and its production initialization/redirect checks passed.
The official USDT contract is pinned in code; arbitrary tokens cannot be enabled.

Fixed prices and conversion calculations use exact integer/rational arithmetic.
Each window has 10,000 identifiers shared across integer-priced plans. The default
window adds 0.000001 to 0.010000 USDT. A wallet recovered after losing its
original invoice ledger can use `usdt_wallets.invoice_code_offset=10000` when
its previous prices were all integers and it only issued codes 000001–010000.
Configure the wallet row before enabling checkout. This window requires whole
USDT prices and adds 0.010001 to 0.020000 USDT. The cashier displays the maximum
adjustment before selection and the exact amount before transfer. Exhaustion
stops issuance. Paid and expired identifiers stay reserved.

Back up the wallet setting with the invoice ledger. After any further data
loss, restore both or choose another verified unused range or receiving address.
Keep prior wallet records so the observer can reconcile their transfers.

`USDT_ACCEPT_PAYMENTS=false` disables new USDT selections; retain the receiving
address and chain API configuration so the background monitor still settles
existing invoices. Do not remove old wallet records or invoices. Closing the
feature must not strand already-payable transfers.

## Release sequence

1. Back up the control-plane database and application. Apply migration 020 only
   as part of a tested server release; it is additive and existing orders default
   to the Alipay channel.
2. Run race-detected USDT tests against **local/usdt_test** and the existing
   Alipay payment regression against a separate empty database. Use no production
   credentials or production orders. Frontend: lint, typecheck and build.
3. Use the opt-in mainnet smoke test against **local/usdt_preview**, with a
   separately controlled small payment sent manually by the operator. It never
   sends crypto, signs transactions, or grants real production membership.
4. Confirm the actual receipt is solidified, full amount matches, one test
   subscription was granted, and repeat scans do not grant another subscription.
5. Review the public price list and private wallet configuration. Only after
   acceptance enable the production service, observer and then new checkout.
6. Check legacy Alipay callbacks, the payment selector, QR code, exact amount,
   payment status and the administration order/transfer views after release.

The current public source has no production wallet or API key. Keep these out
of GitHub, screenshots and build logs.

## Tests and manual acceptance

Environment variables for automated integration tests:

```
USDT_TEST_DATABASE_URL=postgres://...@127.0.0.1:PORT/usdt_test?sslmode=disable
PAYMENT_TEST_DATABASE_URL=postgres://...@127.0.0.1:PORT/alipay_test?sslmode=disable
```

`TestUSDTCheckoutMonitorAndReplay` recreates the `public` schema only in the
explicit localhost/usdt_test database. It must never target production. The
legacy Alipay test expects a fresh database for each run.

To run the manual smoke test set `USDT_MAINNET_PREVIEW=1`,
`USDT_PREVIEW_DATABASE_URL` to a fresh localhost/usdt_preview database,
`USDT_PREVIEW_RECEIVE_ADDRESS` to the operator's own address and
`USDT_PREVIEW_OUTPUT` to a private output folder; run only
`go test -run '^TestUSDTMainnetPreview$' -count=1 -v -timeout 0 ./cmd/26x-api`.
It binds **127.0.0.1 only**, labels the page as an isolated test and writes the
local preview link to `preview.json`. Click USDT on the page to allocate the
visible amount. Send only if the address, network and amount are verified by
the wallet owner. Public chain reads are used in this explicit test if no API
key is supplied; production acceptance requires its own key.

On an actual successful payment the test writes `confirmed.json`, verifies that
a second scan did not issue membership again, and keeps the page available.
Create an empty `stop` file in the private output folder to end the test. The
smoke test fails if stopped before a confirmed transfer, or if the invoice
expires without payment. A mock receipt is never evidence of mainnet success.

## Support and recovery

The admin order view shows the method, USDT amount, address and paid transaction
hash. The transfer table also lists unmatched/late payments. Authorized owner
and operations staff can request a chain-only recheck of a TXID; this is audited
and never accepts client-supplied amounts, screenshots or recipient overrides.
Each `(tx_hash, log_index)` is consumed at most once. Late or unmatched-amount
payments enter review instead of being guessed. A mistyped amount that exactly
matches another valid invoice cannot be distinguished from that invoice
using amount-only matching; six-digit codes are not payer authentication.

If an exceptional payment merits a manual membership grant after investigation,
use the existing audited subscription assignment flow, record the case and
avoid granting it again if the same transaction is resubmitted. Do not edit
confirmed transfer evidence or reuse an expired payment code.

Rollback to legacy checkout: turn acceptance off, verify Alipay works and leave
the observer running for issued USDT invoices. Keep the additive tables and
records for settlement and reconciliation.

## Deployment status (2026-11-20, Asia/Shanghai)

The API, migration 020, observer and admin UI are installed. The dedicated chain
key was verified and remains only in the private server environment. Both choices are now enabled: `USDT_ACCEPT_PAYMENTS=true` and
`ALIPAY_MAINTENANCE=false`. The merchant reported Alipay maintenance complete;
production order initialization and the existing cashier redirect passed using a
retired synthetic test account, without another payment or membership grant.
For a future provider outage, set `ALIPAY_MAINTENANCE=true` to display maintenance
and pause new Alipay requests while retaining callbacks and USDT processing.
See `docs/usdt-test-report.md` for historical acceptance and current limits.

When deploying the Next.js standalone directory, create `.next/cache` and assign
it to the service account before restarting: the existing hardened systemd unit
requires that directory for its writable-path namespace. Do not weaken the unit
to work around a missing deployment directory.
