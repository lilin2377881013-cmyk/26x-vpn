package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicitly opt-in mainnet smoke test. Uses a separate LOCAL database and
// public read-only chain APIs. The operator sends the visible test amount;
// this code never signs, broadcasts or initiates wallet transfers.
func TestUSDTMainnetPreview(t *testing.T) {
	if os.Getenv("USDT_MAINNET_PREVIEW") != "1" {
		t.Skip("manual mainnet smoke test not requested")
	}
	dsn := os.Getenv("USDT_PREVIEW_DATABASE_URL")
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/usdt_preview" {
		t.Fatal("mainnet preview requires dedicated LOCAL usdt_preview database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = applyMigrations(ctx, db, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &usdtGateway{accepting: true, testMode: true, address: os.Getenv("USDT_PREVIEW_RECEIVE_ADDRESS"), origin: "http://" + listener.Addr().String(), apiOrigin: "https://api.trongrid.io", apiKey: os.Getenv("TRONGRID_API_KEY"), prices: map[string]int64{"usdt-live-smoke": 1000000}, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if _, err = tronAddressHex(g.address); err != nil {
		t.Fatal("USDT_PREVIEW_RECEIVE_ADDRESS required", err)
	}
	s := newServer(db, config{usdt: g, trustedOrigins: map[string]bool{g.origin: true}}, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err = s.pollUSDT(ctx); err != nil {
		t.Fatal("mainnet monitor not ready", err)
	}
	var user, plan string
	if err = db.QueryRow(`INSERT INTO users(email,password_hash) VALUES('local-usdt-smoke@example.test','disabled-test-login') RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO plans(code,name,tier,duration_days,price_cents,currency,max_devices) VALUES('usdt-live-smoke','USDT 收款链路测试（非正式会员订单）','standard',1,700,'CNY',1) RETURNING id::text`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	token, _ := randomToken(32)
	number, _ := randomToken(18)
	number = "smoke_" + number
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(20 * time.Minute)
	if _, err = db.Exec(`INSERT INTO payment_orders(order_no,user_id,plan_id,product,amount_cents,duration_days,payment_channel,checkout_token_hash,checkout_expires_at) VALUES($1,$2,$3,'selection',700,1,'selection',$4,$5)`, number, user, plan, hash[:], expires); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	go s.watchUSDT(ctx)
	outDir := os.Getenv("USDT_PREVIEW_OUTPUT")
	if outDir == "" {
		t.Fatal("USDT_PREVIEW_OUTPUT required")
	}
	if err = os.MkdirAll(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	previewURL := g.origin + cashierPath + token
	metadata := map[string]any{"url": previewURL, "orderNo": number, "address": g.address, "testOnly": true, "minimumTestUSDT": "1.000001", "expiresAt": expires.Format(time.RFC3339), "database": "local/usdt_preview"}
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if err = os.WriteFile(filepath.Join(outDir, "preview.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("ISOLATED MAINNET PREVIEW", previewURL)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	recorded := false
	for range ticker.C {
		var paid bool
		var subscriptions int
		if err = db.QueryRow(`SELECT paid_at IS NOT NULL FROM payment_orders WHERE order_no=$1`, number).Scan(&paid); err != nil {
			t.Fatal(err)
		}
		if paid && !recorded {
			if err = db.QueryRow(`SELECT count(*) FROM subscriptions WHERE user_id=$1`, user).Scan(&subscriptions); err != nil || subscriptions != 1 {
				t.Fatal("invalid fulfillment count", subscriptions, err)
			}
			var txHash string
			if err = db.QueryRow(`SELECT tx_hash FROM usdt_transfers WHERE order_no=$1 AND result='paid'`, number).Scan(&txHash); err != nil {
				t.Fatal(err)
			}
			metadata["confirmed"] = true
			metadata["txHash"] = txHash
			metadata["subscriptionCount"] = subscriptions
			data, _ = json.MarshalIndent(metadata, "", "  ")
			if err = os.WriteFile(filepath.Join(outDir, "confirmed.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = s.pollUSDT(ctx); err != nil {
				t.Fatal(err)
			}
			db.QueryRow(`SELECT count(*) FROM subscriptions WHERE user_id=$1`, user).Scan(&subscriptions)
			if subscriptions != 1 {
				t.Fatal("mainnet replay duplicated subscription")
			}
			t.Log("MAINNET CONFIRMED; replay verified; one local test subscription only", txHash)
			recorded = true
		}
		if _, err = os.Stat(filepath.Join(outDir, "stop")); err == nil {
			if !recorded {
				t.Fatal("mainnet acceptance stopped before an actual confirmed payment")
			}
			return
		}
		if time.Now().After(expires.Add(10*time.Minute)) && !recorded {
			t.Fatal("mainnet payment not completed; production stays disabled")
		}
	}
}
