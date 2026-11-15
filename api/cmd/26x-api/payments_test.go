package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPaymentCheckoutAndNotification(t *testing.T) {
	dsn := os.Getenv("PAYMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PAYMENT_TEST_DATABASE_URL must point to an empty test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := applyMigrations(ctx, db, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := db.QueryRow(`INSERT INTO users(email,password_hash)
		VALUES ('payment-test@example.test','unused') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO plans(code,name,tier,duration_days,price_cents,currency,max_devices)
		VALUES ('paid-test','Payment test','standard',30,3600,'CNY',2)`); err != nil {
		t.Fatal(err)
	}
	var reportedAmount atomic.Int64
	reportedAmount.Store(3600)
	gateway := &paymentGateway{merchant: "1424", secret: "fixture-secret", product: "99982",
		notifyURL: "https://example.test" + paymentNotifyPath, callbackIPs: map[string]struct{}{"192.0.2.1": {}}}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := map[string]string{}
		switch r.URL.Path {
		case "/api/v1/payment/init":
			var body map[string]json.RawMessage
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid checkout request")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for key, raw := range body {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					value = string(raw)
				}
				fields[key] = value
			}
			if fields["product"] != "99982" || string(body["amount"]) != "3600" || fields["notifyUrl"] != gateway.notifyURL {
				t.Errorf("unexpected checkout fields: %v", fields)
			}
		case "/api/v1/payment/query":
			if r.Method != http.MethodGet {
				t.Error("invalid query method")
			}
			for key := range r.URL.Query() {
				fields[key] = r.URL.Query().Get(key)
			}
		default:
			t.Errorf("unexpected gateway path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !gateway.verify(fields) || fields["mchKey"] != "1424" || len(fields["nonce"]) != 24 || len(fields["timestamp"]) != 13 {
			t.Error("invalid signed fields")
		}
		order := gatewayOrder{Merchant: "1424", Number: fields["mchOrderNo"], Product: "99982", Serial: "fixture-serial", Amount: 3600, Status: "PROCESSING"}
		order.URL.PayURL = "https://example.test/pay"
		order.URL.Expire = 2300000000000
		if r.Method == http.MethodGet {
			order.Status, order.RealAmount = "SUCCESS", reportedAmount.Load()
		}
		writeJSON(w, http.StatusOK, map[string]any{"code": "200", "data": order})
	}))
	defer mock.Close()
	gateway.apiOrigin, gateway.client = mock.URL, mock.Client()
	s := newServer(db, config{payment: gateway}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/me/checkout", strings.NewReader(`{"planCode":"paid-test"}`))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.checkout(rec, r.WithContext(contextWithActor(r, actor{id: userID})))
	var response struct{ OrderNo, PayURL string }
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.OrderNo == "" || response.PayURL == "" {
		t.Fatalf("checkout failed: %d %s", rec.Code, rec.Body.String())
	}

	notify := func(ip, actualAmount, sign string) *httptest.ResponseRecorder {
		fields := map[string]string{"mchKey": "1424", "mchOrderNo": response.OrderNo, "serialOrderNo": "fixture-serial",
			"amount": "3600", "realAmount": actualAmount, "payStatus": "SUCCESS", "timestamp": "1880000000000", "nonce": "12345678"}
		if sign == "" {
			sign = strings.ToUpper(gatewaySignature(fields, gateway.secret))
		}
		fields["sign"] = sign
		body, _ := json.Marshal(fields)
		callback := httptest.NewRequest(http.MethodPost, paymentNotifyPath, strings.NewReader(string(body)))
		callback.RemoteAddr = ip + ":12345"
		callback.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		s.paymentNotify(result, callback)
		return result
	}
	if got := notify("192.0.2.2", "3600", "").Code; got != http.StatusForbidden {
		t.Fatalf("unexpected response for unlisted IP: %d", got)
	}
	if got := notify("192.0.2.1", "3600", "invalid").Code; got != http.StatusBadRequest {
		t.Fatalf("unexpected response for invalid signature: %d", got)
	}
	if got := notify("192.0.2.1", "3601", "").Code; got != http.StatusBadRequest {
		t.Fatalf("unexpected response for wrong amount: %d", got)
	}
	reportedAmount.Store(100)
	if got := notify("192.0.2.1", "3600", "").Code; got != http.StatusConflict {
		t.Fatalf("unexpected response for query amount mismatch: %d", got)
	}
	reportedAmount.Store(3600)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			w := notify("192.0.2.1", "3600", "")
			if w.Code != http.StatusOK || w.Body.String() != "SUCCESS" {
				t.Errorf("callback failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	var subscriptions int
	if err := db.QueryRow("SELECT count(*) FROM subscriptions WHERE user_id=$1", userID).Scan(&subscriptions); err != nil || subscriptions != 1 {
		t.Fatalf("expected one subscription, got %d, error %v", subscriptions, err)
	}
}
