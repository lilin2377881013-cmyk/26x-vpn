package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

const cashierPath = "/api/v1/payments/checkout/"

//go:embed usdt_cashier.html
var cashierHTML string
var cashierTemplate = template.Must(template.New("cashier").Parse(cashierHTML))

type checkoutOrder struct {
	number, userID, planID, planCode, planName, channel string
	cents                                               int64
	expires                                             time.Time
	paid                                                sql.NullTime
	cashier                                             sql.NullString
}

func (s *server) checkoutSelection(w http.ResponseWriter, r *http.Request, code string) {
	g := s.cfg.usdt
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "checkout_failed", "Unable to create payment")
		return
	}
	number, err := randomToken(18)
	if err != nil {
		writeError(w, 500, "checkout_failed", "Unable to create payment")
		return
	}
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(20 * time.Minute)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 503, "checkout_failed", "Payment unavailable")
		return
	}
	defer tx.Rollback()
	user := actorFromContext(r).id
	var allowed bool
	err = tx.QueryRowContext(r.Context(), `SELECT status='active' AND NOT EXISTS(SELECT 1 FROM trial_accounts WHERE user_id=users.id)
 AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE user_id=users.id AND status='active' AND ends_at IS NULL) FROM users WHERE id=$1 FOR UPDATE`, user).Scan(&allowed)
	if err != nil || !allowed {
		writeError(w, 409, "checkout_not_allowed", "Please sign in with an eligible account")
		return
	}
	var count int
	if tx.QueryRowContext(r.Context(), `SELECT count(*) FROM payment_orders WHERE user_id=$1 AND checkout_expires_at>now() AND paid_at IS NULL`, user).Scan(&count) != nil {
		writeError(w, 503, "checkout_failed", "Payment unavailable")
		return
	}
	if count >= 3 {
		writeError(w, 429, "pending_payments", "Finish existing payment orders before creating more")
		return
	}
	var cents int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO payment_orders(order_no,user_id,plan_id,product,amount_cents,duration_days,payment_channel,checkout_token_hash,checkout_expires_at)
 SELECT $1,$2,id,'selection',price_cents,duration_days,'selection',$3,$4 FROM plans WHERE code=$5 AND active AND currency='CNY' AND price_cents BETWEEN 2000 AND 200000 RETURNING amount_cents`, "26x_"+number, user, hash[:], expires, code).Scan(&cents)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "plan_unavailable", "Plan unavailable")
		return
	}
	if err != nil || tx.Commit() != nil {
		writeError(w, 503, "checkout_failed", "Payment unavailable")
		return
	}
	writeJSON(w, 201, map[string]any{"orderNo": "26x_" + number, "payUrl": g.origin + cashierPath + token, "expiresAt": expires.Format(time.RFC3339)})
}

func (s *server) cashierOrder(ctx context.Context, token string) (checkoutOrder, error) {
	var o checkoutOrder
	if len(token) != 43 {
		return o, sql.ErrNoRows
	}
	hash := sha256.Sum256([]byte(token))
	err := s.db.QueryRowContext(ctx, `SELECT o.order_no,o.user_id::text,o.plan_id::text,p.code,p.name,o.payment_channel,o.amount_cents,o.checkout_expires_at,o.paid_at,o.cashier_url
 FROM payment_orders o JOIN plans p ON p.id=o.plan_id WHERE o.checkout_token_hash=$1 AND o.checkout_expires_at>now()-interval '7 days'`, hash[:]).Scan(&o.number, &o.userID, &o.planID, &o.planCode, &o.planName, &o.channel, &o.cents, &o.expires, &o.paid, &o.cashier)
	return o, err
}
func cashierHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}
func (s *server) cashierPage(w http.ResponseWriter, r *http.Request) {
	cashierHeaders(w)
	// Form navigations may redirect to the existing external Alipay cashier.
	// Keep POST targets local in the template, and allow its HTTPS redirects
	// plus the one explicitly configured legacy HTTP gateway origin.
	policy := w.Header().Get("Content-Security-Policy")
	formSources := "form-action 'self' https:"
	if pg := s.cfg.payment; pg != nil {
		if u, e := url.Parse(pg.cashierOrigin); e == nil && u.Scheme == "http" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(u.Host, "; ") {
			formSources += " " + u.Scheme + "://" + u.Host
		}
	}
	w.Header().Set("Content-Security-Policy", strings.Replace(policy, "form-action 'self'", formSources, 1))

	o, err := s.cashierOrder(r.Context(), r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := map[string]any{"Testing": s.cfg.usdt != nil && s.cfg.usdt.testMode, "Order": o.number, "Plan": o.planName, "Yuan": fmt.Sprintf("%d.%02d", o.cents/100, o.cents%100), "Token": r.PathValue("token"), "Root": cashierPath, "Paid": o.paid.Valid, "Expired": time.Now().After(o.expires), "Channel": o.channel, "Alipay": s.cfg.payment != nil, "USDT": false}
	if g := s.cfg.usdt; g != nil && g.accepting {
		if quote, e := g.quote(o.planCode, o.cents); e == nil {
			data["USDT"] = true
			data["Quote"] = formatUSDT(quote)
		}
	}
	if o.channel == "usdt" {
		var address, status string
		var amount int64
		var expires time.Time
		if s.db.QueryRowContext(r.Context(), `SELECT address,amount_units,status,expires_at FROM usdt_invoices WHERE order_no=$1`, o.number).Scan(&address, &amount, &status, &expires) != nil {
			http.Error(w, "Payment unavailable", 503)
			return
		}
		data["Address"], data["Amount"], data["Code"], data["Expires"], data["Expired"] = address, formatUSDT(amount), fmt.Sprintf("%06d", amount%usdtUnit), expires.Local().Format("2006-01-02 15:04:05 MST"), time.Now().After(expires)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cashierTemplate.Execute(w, data); err != nil {
		s.logger.Error("cashier render", "error", err)
	}
}
func (s *server) selectCashier(w http.ResponseWriter, r *http.Request) {
	cashierHeaders(w)
	g := s.cfg.usdt
	if g == nil {
		http.Error(w, "Payment unavailable", 503)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != g.origin {
		http.Error(w, "Invalid origin", 403)
		return
	}
	o, err := s.cashierOrder(r.Context(), r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	method := r.PathValue("method")
	if method != "alipay" && method != "usdt" {
		http.NotFound(w, r)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Payment unavailable", 503)
		return
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(r.Context(), `SELECT payment_channel,paid_at,cashier_url FROM payment_orders WHERE order_no=$1 FOR UPDATE`, o.number).Scan(&o.channel, &o.paid, &o.cashier)
	if err != nil {
		http.Error(w, "Payment unavailable", 503)
		return
	}
	if o.paid.Valid {
		http.Redirect(w, r, cashierPath+r.PathValue("token"), 303)
		return
	}
	if time.Now().After(o.expires) {
		http.Error(w, "Order expired; create a new order in 26x", 410)
		return
	}
	if o.channel != "selection" && o.channel != method {
		http.Error(w, "Payment method already selected; use the existing order", 409)
		return
	}
	if method == "usdt" {
		if !g.accepting {
			http.Error(w, "USDT payments temporarily unavailable", 503)
			return
		}
		if o.channel == "selection" {
			quote, e := g.quote(o.planCode, o.cents)
			if e != nil {
				http.Error(w, "USDT price unavailable", 503)
				return
			}
			if err = s.allocateUSDTInvoice(r.Context(), tx, o, quote); err != nil {
				s.logger.Warn("USDT allocation unavailable", "error", err)
				http.Error(w, "USDT payments temporarily unavailable; try later", 503)
				return
			}
		}
	} else {
		pg := s.cfg.payment
		if pg == nil {
			http.Error(w, "Alipay unavailable", 503)
			return
		}
		// Lock the choice durably before an external provider can create a
		// payable order. An API timeout must not reopen USDT on the same order.
		if o.channel == "selection" {
			if _, err = tx.ExecContext(r.Context(), `UPDATE payment_orders SET payment_channel='alipay',product=$2 WHERE order_no=$1`, o.number, pg.product); err != nil {
				http.Error(w, "Payment unavailable", 503)
				return
			}
			if err = tx.Commit(); err != nil {
				http.Error(w, "Payment unavailable", 503)
				return
			}
			tx, err = s.db.BeginTx(r.Context(), nil)
			if err != nil {
				http.Error(w, "Payment unavailable", 503)
				return
			}
			defer tx.Rollback()
			if err = tx.QueryRowContext(r.Context(), `SELECT payment_channel,paid_at,cashier_url FROM payment_orders WHERE order_no=$1 FOR UPDATE`, o.number).Scan(&o.channel, &o.paid, &o.cashier); err != nil {
				http.Error(w, "Payment unavailable", 503)
				return
			}
			if o.paid.Valid {
				http.Redirect(w, r, cashierPath+r.PathValue("token"), 303)
				return
			}
		}
		if !o.cashier.Valid {
			provider, e := pg.request(r.Context(), http.MethodPost, "init", o.number, o.cents)
			if e != nil {
				http.Error(w, "Alipay unavailable; retry this order", 502)
				return
			}
			address := pg.checkoutURL(provider.URL.PayURL, provider.URL.Expire)
			if !pg.matches(provider, o.number, pg.product, o.cents) || provider.Status != "PROCESSING" || address == "" || provider.URL.Expire <= time.Now().UnixMilli() {
				http.Error(w, "Invalid cashier response", 502)
				return
			}
			_, err = tx.ExecContext(r.Context(), `UPDATE payment_orders SET payment_channel='alipay',product=$2,provider_order_no=$3,cashier_url=$4 WHERE order_no=$1`, o.number, pg.product, provider.Serial, address)
			if err != nil {
				http.Error(w, "Payment unavailable", 503)
				return
			}
			o.cashier = sql.NullString{String: address, Valid: true}
		}
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Payment unavailable", 503)
		return
	}
	if method == "alipay" {
		http.Redirect(w, r, o.cashier.String, 303)
	} else {
		http.Redirect(w, r, cashierPath+r.PathValue("token"), 303)
	}
}
func (s *server) allocateUSDTInvoice(ctx context.Context, tx *sql.Tx, o checkoutOrder, quote int64) error {
	g := s.cfg.usdt
	if _, err := tx.ExecContext(ctx, `INSERT INTO usdt_wallets(address,scan_through_ms) VALUES($1,$2) ON CONFLICT DO NOTHING`, g.address, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		return err
	}
	// No address accepts new invoices while monitoring is stale or unavailable.
	var monitored bool
	if err := tx.QueryRowContext(ctx, `SELECT last_success_at>now()-interval '2 minutes' FROM usdt_wallets WHERE address=$1`, g.address).Scan(&monitored); err != nil || !monitored {
		return errors.New("chain monitoring is not ready")
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "invoice:"+g.address); err != nil {
		return err
	}
	var units int64
	err := tx.QueryRowContext(ctx, `SELECT candidate FROM generate_series($2::bigint+1,$2::bigint+10000) candidate
 WHERE candidate % 1000000 > 0 AND NOT EXISTS(SELECT 1 FROM usdt_invoices WHERE address=$1 AND payment_code=candidate % 1000000)
 ORDER BY candidate LIMIT 1`, g.address, quote).Scan(&units)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO usdt_invoices(order_no,address,token_contract,payment_code,quote_units,amount_units,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7)`, o.number, g.address, usdtContract, units%usdtUnit, quote, units, o.expires); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE payment_orders SET payment_channel='usdt',product='USDT_TRC20' WHERE order_no=$1`, o.number)
	return err
}
func (s *server) usdtQR(w http.ResponseWriter, r *http.Request) {
	cashierHeaders(w)
	o, err := s.cashierOrder(r.Context(), r.PathValue("token"))
	if err != nil || o.channel != "usdt" || o.paid.Valid || time.Now().After(o.expires) {
		http.NotFound(w, r)
		return
	}
	var address string
	if s.db.QueryRowContext(r.Context(), `SELECT address FROM usdt_invoices WHERE order_no=$1`, o.number).Scan(&address) != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = tronAddressHex(address); err != nil {
		http.Error(w, "Invalid address", 503)
		return
	}
	png, err := qrcode.Encode(address, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "QR unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

// All checkout capabilities are generated from 256 bits of randomness. They do
// not contain account credentials, allow changing the owner, or grant membership.
func normalizedTransferContract(value string) string {
	return strings.TrimPrefix(strings.ToLower(value), "0x")
}
