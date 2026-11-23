package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRegistrationGrantsOneFixedTrial(t *testing.T) {
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
	mux := s.routes()
	call := func(method, path, body string, cookies []*http.Cookie, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
			if c.Name == csrfCookie {
				r.Header.Set("X-CSRF-Token", c.Value)
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: want %d got %d %s", method, path, want, w.Code, w.Body)
		}
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		data["cookies"] = w.Result().Cookies()
		return data
	}
	details := `{"email":"registered-trial@example.test","password":"a-valid-test-password"}`
	for _, body := range []string{`{"identifyOnly":true}`, `{}`} {
		retired := call("POST", "/api/v1/auth/trial", body, nil, 403)
		if len(retired["cookies"].([]*http.Cookie)) != 0 {
			t.Fatal("anonymous session issued")
		}
	}
	call("GET", "/api/v1/nodes?deviceId=00000000-0000-0000-0000-000000000001", "", nil, 401)
	reg := call("POST", "/api/v1/auth/register", details, nil, 201)
	cookies := reg["cookies"].([]*http.Cookie)
	me := call("GET", "/api/v1/me", "", cookies, 200)
	if me["planCode"] != "trial-15m" || me["email"] != "registered-trial@example.test" {
		t.Fatal(me["planCode"], me["email"])
	}
	if _, ok := me["trialCode"]; ok {
		t.Fatal("guest identity exposed")
	}
	expiry, err := time.Parse(time.RFC3339, me["subscriptionEndsAt"].(string))
	if err != nil || time.Until(expiry) < 14*time.Minute || time.Until(expiry) > 15*time.Minute {
		t.Fatal("bad deadline", expiry)
	}
	enroll := func(c []*http.Cookie, sequence int) string {
		return call("POST", "/api/v1/me/devices", fmt.Sprintf(`{"platform":"macos","label":"test","installationToken":"%032d"}`, sequence), c, 201)["id"].(string)
	}
	device := enroll(cookies, 1)
	leasePath := "/api/v1/me/devices/" + device + "/connection-lease"
	call("PUT", leasePath, `{"region":"JP"}`, cookies, 403)
	l := call("PUT", leasePath, `{}`, cookies, 200)
	if l["endpointId"] != endpoint {
		t.Fatal("wrong regional trial assignment")
	}
	nodes := call("GET", "/api/v1/nodes?deviceId="+device, "", cookies, 200)
	if len(nodes["items"].([]any)) == 0 {
		t.Fatal("missing trial node")
	}
	for _, n := range nodes["items"].([]any) {
		if n.(map[string]any)["region"] != "HK" {
			t.Fatal("trial has non-HK endpoint")
		}
	}
	call("POST", "/api/v1/auth/register", details, nil, 409)
	second := call("POST", "/api/v1/auth/session", details, nil, 201)["cookies"].([]*http.Cookie)
	other := enroll(second, 2)
	if device == other {
		t.Fatal("test requires distinct installations")
	}
	again := call("GET", "/api/v1/me", "", second, 200)
	if again["subscriptionEndsAt"] != me["subscriptionEndsAt"] {
		t.Fatal("new login extended trial")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM subscriptions WHERE user_id=$1", me["id"]).Scan(&count); err != nil || count != 1 {
		t.Fatal("trial count", count, err)
	}
	if _, err := db.Exec(`UPDATE subscriptions SET starts_at=now()-interval '16 minutes',ends_at=now()-interval '1 minute' WHERE user_id=$1`, me["id"]); err != nil {
		t.Fatal(err)
	}
	call("PUT", "/api/v1/me/devices/"+other+"/connection-lease", `{}`, second, 403)
	if call("GET", "/api/v1/me", "", second, 200)["planCode"] != nil {
		t.Fatal("expired trial still active")
	}
	third := call("POST", "/api/v1/auth/session", details, nil, 201)["cookies"].([]*http.Cookie)
	if call("GET", "/api/v1/me", "", third, 200)["planCode"] != nil {
		t.Fatal("login restarted expired trial")
	}
}
