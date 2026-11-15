package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTrialIdentityAndFixedExpiration(t *testing.T) {
	dsn := os.Getenv("DEVICE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("DEVICE_TEST_DATABASE_URL must point to an empty test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := applyMigrations(context.Background(), db, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var endpoint string
	err = db.QueryRow(`WITH node AS (
		INSERT INTO nodes(code,name,region_code,status) VALUES('trial-fixture','Trial fixture','HK','online') RETURNING id
	)
	INSERT INTO node_endpoints(node_id,protocol,host,port,transport)
	SELECT id,'vless_reality','fixture.invalid',8443,'tcp' FROM node RETURNING id::text`).Scan(&endpoint)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(db, config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	installation := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	request := func(identify bool) *httptest.ResponseRecorder {
		input, _ := json.Marshal(map[string]any{"installation": installation, "platform": "macos", "identifyOnly": identify})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/trial", strings.NewReader(string(input)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.startTrial(w, req)
		return w
	}
	readIdentity := func() (string, string) {
		t.Helper()
		w := request(true)
		var identity struct{ Code, EndsAt string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &identity) != nil || len(w.Result().Cookies()) != 0 || identity.Code == "" {
			t.Fatalf("bad identity response: %d %s", w.Code, w.Body.String())
		}
		return identity.Code, identity.EndsAt
	}
	code, ends := readIdentity()
	if ends != "" {
		t.Fatal("looking up code started the trial")
	}
	if w := request(false); w.Code != 201 {
		t.Fatalf("trial failed: %d %s", w.Code, w.Body.String())
	}
	var user, device string
	if err := db.QueryRow(`SELECT t.user_id::text,d.id::text FROM trial_accounts t JOIN devices d ON d.user_id=t.user_id WHERE t.code=$1`, code).Scan(&user, &device); err != nil {
		t.Fatal(err)
	}
	requestLease := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", device)
		w := httptest.NewRecorder()
		s.acquireConnectionLease(w, req.WithContext(contextWithActor(req, actor{id: user})))
		return w
	}
	if w := requestLease(`{"region":"JP"}`); w.Code != http.StatusForbidden {
		t.Fatalf("trial picked a different region: %d %s", w.Code, w.Body.String())
	}
	w := requestLease(`{}`)
	var lease struct{ EndpointID, Token string }
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &lease) != nil || lease.EndpointID != endpoint {
		t.Fatalf("legacy trial client did not reserve regional HK: %d %s", w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"token": lease.Token})
	if w := requestLease(string(body)); w.Code != http.StatusOK {
		t.Fatalf("HK trial lease could not renew: %d %s", w.Code, w.Body.String())
	}
	current, ends := readIdentity()
	expiry, err := time.Parse(time.RFC3339, ends)
	if current != code || err != nil || time.Until(expiry) < 14*time.Minute || time.Until(expiry) > 15*time.Minute {
		t.Fatalf("unexpected trial code or deadline %q %q", current, ends)
	}
	if w := request(false); w.Code != 201 {
		t.Fatalf("trial could not resume: %d %s", w.Code, w.Body.String())
	}
	if _, resumedUntil := readIdentity(); resumedUntil != ends {
		t.Fatal("resuming extended the trial")
	}
	if _, err := db.Exec(`UPDATE subscriptions SET starts_at=now()-interval '16 minutes',ends_at=now()-interval '1 minute'
		WHERE user_id=(SELECT user_id FROM trial_accounts WHERE code=$1)`, code); err != nil {
		t.Fatal(err)
	}
	if w := request(false); w.Code != http.StatusGone {
		t.Fatalf("expired trial restarted: %d %s", w.Code, w.Body.String())
	}
}
