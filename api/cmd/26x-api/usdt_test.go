package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Inert, public checksum-valid fixture; never a real receiving wallet.
const testUSDTWallet = usdtContract

func TestUSDTAmountsAndAddresses(t *testing.T) {
	for _, value := range []string{"-1", "1.0000001", "1e6", "NaN", "1/2", "1,000", " 1", ""} {
		if _, err := parseUSDT(value); err == nil {
			t.Errorf("accepted invalid amount %q", value)
		}
	}
	for value, want := range map[string]int64{"1": 1000000, "0.000001": 1, "5.200001": 5200001} {
		got, err := parseUSDT(value)
		if err != nil || got != want {
			t.Fatalf("parse %s: %d %v", value, got, err)
		}
		if formatUSDT(got) != fmt.Sprintf("%d.%06d", want/1000000, want%1000000) {
			t.Fatal("format lost precision")
		}
	}
	if _, err := tronAddressHex(testUSDTWallet); err != nil {
		t.Fatal("provided wallet checksum invalid", err)
	}
	if _, err := tronAddressHex(usdtContract); err != nil {
		t.Fatal(err)
	}
	if _, err := tronAddressHex(testUSDTWallet[:33] + "Y"); err == nil {
		t.Fatal("bad checksum accepted")
	}
	t.Setenv("USDT_ACCEPT_PAYMENTS", "true")
	t.Setenv("USDT_RECEIVE_ADDRESS", testUSDTWallet)
	t.Setenv("USDT_PUBLIC_ORIGIN", "https://cashier.test")
	t.Setenv("TRONGRID_API_KEY", "fixture-key")
	t.Setenv("USDT_PLAN_PRICES", `{"standard-30d":"5"}`)
	g, err := loadUSDTGateway()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := g.quote("standard-30d", 3000); got != 5000000 {
		t.Fatal("fixed price ignored")
	}
	t.Setenv("USDT_PLAN_PRICES", "")
	t.Setenv("USDT_CNY_RATE", "7.1")
	g, err = loadUSDTGateway()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := g.quote("standard-30d", 3000); got != 4225353 {
		t.Fatal("quote rounding", got)
	}
	t.Setenv("TRONGRID_API_KEY", "")
	if _, err = loadUSDTGateway(); err == nil {
		t.Fatal("accepting without monitor key")
	}
}
func receiptFixture(hash, address string, units int64, at time.Time) tronReceipt {
	contract, _ := tronAddressHex(usdtContract)
	recipient, _ := tronAddressHex(address)
	data := fmt.Sprintf(`{"id":%q,"blockNumber":1234,"blockTimeStamp":%d,"receipt":{"result":"SUCCESS"},"log":[{"address":%q,"topics":[%q,%q,%q],"data":%q}]}`, hash, at.UnixMilli(), contract, transferTopic, strings.Repeat("0", 64), strings.Repeat("0", 24)+recipient, fmt.Sprintf("%064x", units))
	var result tronReceipt
	if json.Unmarshal([]byte(data), &result) != nil {
		panic("fixture")
	}
	return result
}
func TestUSDTReceiptEvidence(t *testing.T) {
	hash := strings.Repeat("a", 64)
	r := receiptFixture(hash, testUSDTWallet, 5000001, time.Now())
	records, err := parseTronReceipt(r, hash, testUSDTWallet)
	if err != nil || len(records) != 1 || records[0].units != "5000001" {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	for _, test := range []string{"fake_token", "wrong_recipient", "failed", "wrong_event", "zero", "bad_data"} {
		t.Run(test, func(t *testing.T) {
			r := receiptFixture(hash, testUSDTWallet, 5000001, time.Now())
			switch test {
			case "fake_token":
				r.Logs[0].Address = strings.Repeat("1", 40)
			case "wrong_recipient":
				r.Logs[0].Topics[2] = strings.Repeat("0", 64)
			case "failed":
				r.Receipt.Result = "REVERT"
			case "wrong_event":
				r.Logs[0].Topics[0] = strings.Repeat("0", 64)
			case "zero":
				r.Logs[0].Data = strings.Repeat("0", 64)
			case "bad_data":
				r.Logs[0].Data = "bad"
			}
			records, err := parseTronReceipt(r, hash, testUSDTWallet)
			if err != nil || len(records) != 0 {
				t.Fatalf("invalid evidence accepted: %v", err)
			}
		})
	}
	r.ID = ""
	if _, err := parseTronReceipt(r, hash, testUSDTWallet); err == nil {
		t.Fatal("unconfirmed receipt accepted")
	}
}

func usdtTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("USDT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("USDT_TEST_DATABASE_URL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/usdt_test" {
		t.Fatal("USDT integration tests require dedicated localhost/usdt_test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err = applyMigrations(context.Background(), db, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestUSDTCheckoutMonitorAndReplay(t *testing.T) {
	db := usdtTestDB(t)
	ctx := context.Background()
	var receiptsMu sync.Mutex
	receipts := map[string]tronReceipt{}
	var apiFail, withPages, alipayFail atomic.Bool
	var apiCalls, alipayCalls atomic.Int64
	chain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		if apiFail.Load() {
			w.WriteHeader(429)
			return
		}
		if r.Header.Get("TRON-PRO-API-KEY") != "fixture-key" {
			t.Error("missing chain key")
		}
		if r.Method == "GET" {
			q := r.URL.Query()
			if !strings.HasPrefix(r.URL.Path, "/v1/accounts/") || q.Get("only_confirmed") != "true" || q.Get("contract_address") != usdtContract {
				t.Error("unsafe history query")
			}
			receiptsMu.Lock()
			items := []map[string]string{}
			for hash := range receipts {
				items = append(items, map[string]string{"transaction_id": hash})
			}
			receiptsMu.Unlock()
			meta := map[string]any{}
			if withPages.Load() && q.Get("fingerprint") == "" {
				meta = map[string]any{"fingerprint": "page-two", "links": map[string]string{"next": "https://untrusted.invalid/do-not-follow"}}
			}
			writeJSON(w, 200, map[string]any{"success": true, "data": items, "meta": meta})
			return
		}
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		receiptsMu.Lock()
		receipt := receipts[req["value"]]
		receiptsMu.Unlock()
		writeJSON(w, 200, receipt)
	}))
	defer chain.Close()
	gateway := &paymentGateway{merchant: "merchant", secret: "fixture-secret", product: "99982", notifyURL: "https://cashier.test" + paymentNotifyPath, callbackIPs: map[string]struct{}{}}
	alipay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alipayCalls.Add(1)
		if alipayFail.Load() {
			w.WriteHeader(500)
			return
		}
		var fields map[string]any
		json.NewDecoder(r.Body).Decode(&fields)
		order := gatewayOrder{Merchant: "merchant", Number: fmt.Sprint(fields["mchOrderNo"]), Product: "99982", Serial: "fixture-" + fmt.Sprint(fields["mchOrderNo"]), Amount: 3000, Status: "PROCESSING"}
		order.URL.PayURL = "https://cashier.test/alipay"
		order.URL.Expire = time.Now().Add(10 * time.Minute).UnixMilli()
		writeJSON(w, 200, map[string]any{"code": "200", "data": order})
	}))
	defer alipay.Close()
	gateway.apiOrigin = alipay.URL
	gateway.client = alipay.Client()
	s := newServer(db, config{payment: gateway, trustedOrigins: map[string]bool{"https://cashier.test": true}, usdt: &usdtGateway{accepting: true, address: testUSDTWallet, origin: "https://cashier.test", apiOrigin: chain.URL, apiKey: "fixture-key", prices: map[string]int64{"standard-30d": 5000000}, client: chain.Client()}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	user := func(label string) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`INSERT INTO users(email,password_hash) VALUES($1,'unused') RETURNING id::text`, label+"@example.test").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	type orderInfo struct {
		number, token, user string
		units               int64
	}
	makeOrder := func(uid string) orderInfo {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/me/checkout", strings.NewReader(`{"planCode":"standard-30d"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.checkout(w, r.WithContext(contextWithActor(r, actor{id: uid})))
		var data struct{ OrderNo, PayURL string }
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &data) != nil {
			t.Fatalf("checkout %d %s", w.Code, w.Body.String())
		}
		parts := strings.Split(data.PayURL, "/")
		return orderInfo{number: data.OrderNo, token: parts[len(parts)-1], user: uid}
	}
	choose := func(o orderInfo, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", cashierPath+o.token+"/"+method, nil)
		r.SetPathValue("token", o.token)
		r.SetPathValue("method", method)
		r.Header.Set("Origin", "https://cashier.test")
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		return w
	}
	a := makeOrder(user("payer-a"))
	if alipayCalls.Load() != 0 {
		t.Fatal("checkout prematurely initialized alipay")
	}
	if w := choose(a, "usdt"); w.Code != 503 {
		t.Fatal("stale monitor accepted payment", w.Code)
	}
	if err := s.pollUSDT(ctx); err != nil {
		t.Fatal(err)
	}
	if w := choose(a, "usdt"); w.Code != 303 {
		t.Fatalf("choose USDT %d %s", w.Code, w.Body.String())
	}
	if err := db.QueryRow(`SELECT amount_units FROM usdt_invoices WHERE order_no=$1`, a.number).Scan(&a.units); err != nil {
		t.Fatal(err)
	}
	if a.units != 5000001 {
		t.Fatal("incorrect six-digit code", a.units)
	}
	if w := choose(a, "alipay"); w.Code != 409 {
		t.Fatal("allowed channel switch")
	}
	r := httptest.NewRequest("GET", cashierPath+a.token, nil)
	r.SetPathValue("token", a.token)
	w := httptest.NewRecorder()
	s.cashierPage(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "5.000001") || !strings.Contains(w.Body.String(), testUSDTWallet) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cashier missing exact amount/address or privacy headers")
	}
	qr := httptest.NewRecorder()
	s.usdtQR(qr, r)
	if qr.Code != 200 || !strings.HasPrefix(hex.EncodeToString(qr.Body.Bytes()), "89504e470d0a1a0a") {
		t.Fatal("invalid QR")
	}
	hash := strings.Repeat("a", 64)
	receiptsMu.Lock()
	receipts[hash] = receiptFixture(hash, testUSDTWallet, a.units, time.Now().Add(time.Second))
	receiptsMu.Unlock()
	withPages.Store(true)
	if err := s.pollUSDT(ctx); err != nil {
		t.Fatal(err)
	}
	var paid bool
	if err := db.QueryRow(`SELECT paid_at IS NOT NULL FROM payment_orders WHERE order_no=$1`, a.number).Scan(&paid); err != nil || !paid {
		t.Fatal("confirmed funds did not fulfill", err)
	}
	record := chainTransfer{hash: hash, index: 0, address: testUSDTWallet, units: fmt.Sprint(a.units), block: 1234, at: time.Now().Add(time.Second)}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.recordUSDTTransfer(ctx, record); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM subscriptions WHERE user_id=$1`, a.user).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate subscription", count, err)
	}
	// Codes remain reserved forever, including after expiry and settlement.
	b := makeOrder(user("payer-b"))
	if w := choose(b, "usdt"); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	db.QueryRow(`SELECT amount_units FROM usdt_invoices WHERE order_no=$1`, b.number).Scan(&b.units)
	if b.units == a.units {
		t.Fatal("reused payment identifier")
	}
	if _, err := db.Exec(`UPDATE usdt_invoices SET created_at=now()-interval '30 minutes',expires_at=now()-interval '10 minutes' WHERE order_no=$1`, b.number); err != nil {
		t.Fatal(err)
	}
	late := chainTransfer{hash: strings.Repeat("b", 64), index: 0, address: testUSDTWallet, units: fmt.Sprint(b.units), block: 1235, at: time.Now()}
	if err := s.recordUSDTTransfer(ctx, late); err != nil {
		t.Fatal(err)
	}
	var result string
	db.QueryRow(`SELECT result FROM usdt_transfers WHERE tx_hash=$1`, late.hash).Scan(&result)
	if result != "review" {
		t.Fatal("late payment granted")
	}
	wrong := late
	wrong.hash = strings.Repeat("c", 64)
	wrong.units = "123456789"
	if err := s.recordUSDTTransfer(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT result FROM usdt_transfers WHERE tx_hash=$1`, wrong.hash).Scan(&result)
	if result != "unmatched" {
		t.Fatal("wrong amount guessed")
	}
	// New service objects replay the persisted cursor without re-granting funds.
	if err := newServer(db, s.cfg, s.logger).pollUSDT(ctx); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT count(*) FROM subscriptions WHERE user_id=$1`, a.user).Scan(&count)
	if count != 1 {
		t.Fatal("restart duplicated grant")
	}
	var before, after int64
	db.QueryRow(`SELECT scan_through_ms FROM usdt_wallets WHERE address=$1`, testUSDTWallet).Scan(&before)
	apiFail.Store(true)
	if err := s.pollUSDT(ctx); err == nil {
		t.Fatal("API failure ignored")
	}
	db.QueryRow(`SELECT scan_through_ms FROM usdt_wallets WHERE address=$1`, testUSDTWallet).Scan(&after)
	if before != after {
		t.Fatal("failure advanced scan watermark")
	}
	apiFail.Store(false)
	// Provider maintenance must not create a payable Alipay order or block USDT.
	maintenanceOrder := makeOrder(user("payer-maintenance"))
	gateway.maintenance = true
	maintenancePage := httptest.NewRecorder()
	maintenanceRequest := httptest.NewRequest("GET", cashierPath+maintenanceOrder.token, nil)
	s.routes().ServeHTTP(maintenancePage, maintenanceRequest)
	if !strings.Contains(maintenancePage.Body.String(), "支付宝维护中") || strings.Contains(maintenancePage.Body.String(), `/alipay"><button>`) {
		t.Fatal("maintenance page offered unavailable Alipay")
	}
	if w := choose(maintenanceOrder, "alipay"); w.Code != 503 || alipayCalls.Load() != 0 {
		t.Fatal("maintenance contacted provider", w.Code)
	}
	if err := s.pollUSDT(ctx); err != nil {
		t.Fatal(err)
	}
	if w := choose(maintenanceOrder, "usdt"); w.Code != 303 {
		t.Fatal("maintenance prevented USDT selection", w.Code)
	}
	gateway.maintenance = false
	c := makeOrder(user("payer-alipay"))
	if w := choose(c, "alipay"); w.Code != 303 || w.Header().Get("Location") != "https://cashier.test/alipay" {
		t.Fatalf("alipay broken: %d %s", w.Code, w.Body.String())
	}
	if alipayCalls.Load() != 1 {
		t.Fatal("unexpected provider orders", alipayCalls.Load())
	}
	// A public checkout token cannot be used from an unrelated site.
	bad := httptest.NewRequest("POST", cashierPath+c.token+"/alipay", nil)
	bad.SetPathValue("token", c.token)
	bad.SetPathValue("method", "alipay")
	bad.Header.Set("Origin", "https://attacker.invalid")
	badW := httptest.NewRecorder()
	s.selectCashier(badW, bad)
	if badW.Code != 403 {
		t.Fatal("cross-origin payment accepted")
	}
	// Concurrent orders allocate distinct suffixes with database serialization.
	const n = 8
	orders := make([]orderInfo, n)
	for i := range orders {
		orders[i] = makeOrder(user(fmt.Sprintf("parallel-%d", i)))
	}
	for _, o := range orders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := choose(o, "usdt"); w.Code != 303 {
				t.Errorf("concurrent allocation: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if err := db.QueryRow(`SELECT count(*)=count(DISTINCT payment_code) FROM usdt_invoices`).Scan(&paid); err != nil || !paid {
		t.Fatal("code collision")
	}
	// Expired invoices and failed gateway calls must never free a payment code
	// or unlock a second payable channel on the same order.
	d := makeOrder(user("failed-provider"))
	alipayFail.Store(true)
	if w := choose(d, "alipay"); w.Code != 502 {
		t.Fatal("expected provider failure", w.Code)
	}
	if w := choose(d, "usdt"); w.Code != 409 {
		t.Fatal("provider timeout reopened another channel", w.Code)
	}
	alipayFail.Store(false)
	if w := choose(d, "alipay"); w.Code != 303 {
		t.Fatal("retry failed unexpectedly", w.Code)
	}
	// Active, finite members can renew before expiry; settlement appends time.
	renewUser := user("early-renewal")
	var originalEnd time.Time
	if err := db.QueryRow(`INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at)
 SELECT $1,id,'active',now()-interval '20 days',now()+interval '10 days' FROM plans WHERE code='standard-30d' RETURNING ends_at`, renewUser).Scan(&originalEnd); err != nil {
		t.Fatal(err)
	}
	renewOrder := makeOrder(renewUser)
	if err := s.pollUSDT(ctx); err != nil {
		t.Fatal(err)
	}
	if w := choose(renewOrder, "usdt"); w.Code != 303 {
		t.Fatal("active member could not renew", w.Code, w.Body.String())
	}
	for replay := 0; replay < 2; replay++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.grantOrderTx(ctx, tx, renewOrder.number, "renewal-fixture-serial", "usdt"); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var renewalStart, renewalEnd time.Time
	var subscriptionCount int
	if err := db.QueryRow(`SELECT sub.starts_at,sub.ends_at FROM payment_orders o JOIN subscriptions sub ON sub.id=o.subscription_id WHERE o.order_no=$1`, renewOrder.number).Scan(&renewalStart, &renewalEnd); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT count(*) FROM subscriptions WHERE user_id=$1`, renewUser).Scan(&subscriptionCount)
	if !renewalStart.Equal(originalEnd) || !renewalEnd.Equal(originalEnd.AddDate(0, 0, 30)) || subscriptionCount != 2 {
		t.Fatal("renewal lost remaining time or replay issued extra membership")
	}
	permanentUser := user("permanent-checkout")
	if _, err := db.Exec(`INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at) SELECT $1,id,'active',now(),NULL FROM plans WHERE code='standard-30d'`, permanentUser); err != nil {
		t.Fatal(err)
	}
	permanentRequest := httptest.NewRequest("POST", "/api/v1/me/checkout", strings.NewReader(`{"planCode":"standard-30d"}`))
	permanentRequest.Header.Set("Content-Type", "application/json")
	permanentResponse := httptest.NewRecorder()
	s.checkout(permanentResponse, permanentRequest.WithContext(contextWithActor(permanentRequest, actor{id: permanentUser})))
	if permanentResponse.Code != 409 || !strings.Contains(permanentResponse.Body.String(), "永久会员，无需续费") {
		t.Fatal("permanent membership rejection was unclear", permanentResponse.Code, permanentResponse.Body.String())
	}
	// Once new USDT payments are disabled, existing confirmed transfers still settle.
	s.cfg.usdt.accepting = false
	e := orders[0]
	db.QueryRow(`SELECT amount_units FROM usdt_invoices WHERE order_no=$1`, e.number).Scan(&e.units)
	r2 := receiptFixture(strings.Repeat("d", 64), testUSDTWallet, e.units, time.Now().Add(time.Second))
	receiptsMu.Lock()
	receipts[r2.ID] = r2
	receiptsMu.Unlock()
	if err := s.pollUSDT(ctx); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT paid_at IS NOT NULL FROM payment_orders WHERE order_no=$1`, e.number).Scan(&paid)
	if !paid {
		t.Fatal("disabling new orders stopped settlement")
	}
	s.cfg.usdt.accepting = true
	// Manual TXID rechecks use chain evidence and are audited, not client assertions.
	var owner string
	if err := db.QueryRow(`INSERT INTO admin_accounts(email,password_hash,role,mfa_required) VALUES('usdt-owner@example.test','unused','owner',false) RETURNING id::text`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/admin/usdt/recheck", strings.NewReader(`{"txHash":"`+r2.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	s.recheckUSDT(out, req.WithContext(contextWithActor(req, actor{id: owner, kind: "admin", role: "owner"})))
	if out.Code != 200 {
		t.Fatalf("recheck failed %d %s", out.Code, out.Body.String())
	}
	db.QueryRow(`SELECT count(*) FROM audit_events WHERE action='usdt.transaction_rechecked'`).Scan(&count)
	if count != 1 {
		t.Fatal("manual recheck missing audit")
	}
	out = httptest.NewRecorder()
	s.listPaymentOrders(out, httptest.NewRequest("GET", "/", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), "usdtAmount") || !strings.Contains(out.Body.String(), "txHash") {
		t.Fatal("admin missing USDT fields", out.Body.String())
	}
	out = httptest.NewRecorder()
	s.listUSDTTransfers(out, httptest.NewRequest("GET", "/", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), "review") {
		t.Fatal("admin missing review transfers")
	}
	// Unknown/tampered capabilities reveal no payment information.
	invalid := httptest.NewRequest("GET", cashierPath+"bad", nil)
	invalid.SetPathValue("token", "bad")
	out = httptest.NewRecorder()
	s.cashierPage(out, invalid)
	if out.Code != 404 {
		t.Fatal("bad capability accepted")
	}
	// Simulate stale monitoring: no newly issued USDT invoice is payable.
	if _, err := db.Exec(`UPDATE usdt_wallets SET last_success_at=now()-interval '5 minutes'`); err != nil {
		t.Fatal(err)
	}
	stale := makeOrder(user("stale-monitor"))
	if w := choose(stale, "usdt"); w.Code != 503 {
		t.Fatal("stale monitor accepted new invoice", w.Code)
	}
	// Stop accidental use of the same six-digit code after old orders expire.
	var reserved int
	if err := db.QueryRow(`SELECT count(*) FROM usdt_invoices WHERE amount_units=$1`, b.units).Scan(&reserved); err != nil || reserved != 1 {
		t.Fatal("expired identifier removed")
	}
	// Exhaust the bounded suffix window using expired fixture invoices. This
	// must fail closed rather than reusing codes or increasing the surcharge.
	if _, err := db.Exec(`UPDATE usdt_wallets SET last_success_at=now()`); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO payment_orders(order_no,user_id,plan_id,product,amount_cents,duration_days,payment_channel)
      SELECT 'reserved_'||g,$1,p.id,'USDT_TRC20',3000,30,'usdt' FROM generate_series(1,10000) g CROSS JOIN plans p
      WHERE p.code='standard-30d' AND NOT EXISTS(SELECT 1 FROM usdt_invoices WHERE address=$2 AND payment_code=g)`, a.user, testUSDTWallet)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO usdt_invoices(order_no,address,token_contract,payment_code,quote_units,amount_units,status,created_at,expires_at)
      SELECT o.order_no,$1,$2,substring(o.order_no FROM 10)::integer,5000000,5000000+substring(o.order_no FROM 10)::integer,'expired',now()-interval '2 hours',now()-interval '1 hour'
      FROM payment_orders o WHERE o.order_no LIKE 'reserved_%'`, testUSDTWallet, usdtContract)
	if err != nil {
		t.Fatal(err)
	}
	full := makeOrder(user("full-window"))
	if w := choose(full, "usdt"); w.Code != 503 {
		t.Fatal("exhausted identifiers were reused or price window expanded", w.Code)
	}
	if apiCalls.Load() < 3 {
		t.Fatal("chain confirmation not checked")
	}
}
