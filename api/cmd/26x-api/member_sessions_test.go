package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPersistentMemberSessions(t *testing.T) {
	dsn := os.Getenv("SESSION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SESSION_TEST_DATABASE_URL must point to an empty test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	// Seed pre-upgrade tokens, then migrate through the regular startup path.
	dir := t.TempDir()
	files, err := filepath.Glob("../../migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(file) >= "021" {
			continue
		}
		source, err := filepath.Abs(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(source, filepath.Join(dir, filepath.Base(file))); err != nil {
			t.Fatal(err)
		}
	}
	if err := applyMigrations(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	hash, err := hashPassword("correct-password-12345")
	if err != nil {
		t.Fatal(err)
	}
	var uid string
	if err = db.QueryRow(`INSERT INTO users(email,password_hash) VALUES ('member@fixture.invalid',$1) RETURNING id`, hash).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	old := func(token string, expired, revoked bool) []*http.Cookie {
		sum := sha256.Sum256([]byte(token))
		exec(`INSERT INTO sessions(actor_kind,actor_id,token_hash,csrf_token,expires_at,revoked_at)
			VALUES('user',$1,$2,'csrf',now()+(CASE WHEN $3 THEN interval '-1 hour' ELSE interval '1 hour' END),
			CASE WHEN $4 THEN now() END)`, uid, sum[:], expired, revoked)
		return []*http.Cookie{{Name: sessionCookie, Value: token}}
	}
	live := old("legacy_live", false, false)
	expired := old("legacy_expired", true, false)
	revoked := old("legacy_revoked", false, true)
	if err = applyMigrations(ctx, db, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	server := newServer(db, config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := server.routes()
	call := func(method, path, body string, portal bool, cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if portal {
			r.Header.Set("X-26x-Portal", "1")
		}
		for _, cookie := range cookies {
			r.AddCookie(cookie)
			if strings.HasSuffix(cookie.Name, "csrf") {
				r.Header.Set("X-CSRF-Token", cookie.Value)
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	me := func(cookies []*http.Cookie, portal bool, want int) {
		t.Helper()
		w := call("GET", "/api/v1/me", "", portal, cookies)
		if w.Code != want {
			t.Fatalf("me: want %d, got %d %s", want, w.Code, w.Body.String())
		}
	}
	login := func(password string, portal bool) *httptest.ResponseRecorder {
		return call("POST", "/api/v1/auth/session", fmt.Sprintf(`{"email":"member@fixture.invalid","password":%q}`, password), portal, nil)
	}
	signIn := func(portal bool) []*http.Cookie {
		t.Helper()
		w := login("correct-password-12345", portal)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["expiresAt"] != nil {
			t.Fatal("unexpected deadline", w.Body.String())
		}
		return w.Result().Cookies()
	}
	me(live, false, 200)
	me(expired, false, 401)
	me(revoked, false, 401)
	if w := login("wrong-password", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	me(live, false, 200)
	web := signIn(true)
	me(live, false, 200)
	a := signIn(false)
	me(live, false, 401)
	me(web, true, 200)
	exec(`UPDATE sessions SET created_at=now()-interval '2 days' WHERE actor_id=$1`, uid)
	me(a, false, 200)
	var permanent bool
	sum := sha256.Sum256([]byte(a[0].Value))
	if err := db.QueryRow(`SELECT expires_at IS NULL FROM sessions WHERE token_hash=$1`, sum[:]).Scan(&permanent); err != nil || !permanent {
		t.Fatal("member session expires", err)
	}
	var device string
	if err := db.QueryRow(`INSERT INTO devices(user_id,platform,label)
		VALUES ($1,'macos','test') RETURNING id`, uid).Scan(&device); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO connection_leases(device_id,user_id,token_hash,expires_at)
		VALUES ($1,$2,decode('01','hex'),now()+interval '90 seconds')`, device, uid)
	b := signIn(false)
	var oldLease bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM connection_leases WHERE user_id=$1)`, uid).Scan(&oldLease); err != nil || oldLease {
		t.Fatal("old app still occupies a connection", err)
	}
	me(a, false, 401)
	me(b, false, 200)
	me(web, true, 200)
	if w := call("DELETE", "/api/v1/auth/session", "", false, a); w.Code != 401 {
		t.Fatal("stale logout", w.Code)
	}
	me(b, false, 200)
	if w := call("GET", "/api/v1/nodes", "", true, web); w.Code != 401 {
		t.Fatal("portal granted tunnel access")
	}
	// Two correct logins racing for the same ordinary account leave one winner.
	var wg sync.WaitGroup
	out := make([]*httptest.ResponseRecorder, 2)
	for i := range out {
		wg.Add(1)
		go func(i int) { defer wg.Done(); out[i] = login("correct-password-12345", false) }(i)
	}
	wg.Wait()
	survivors := 0
	for _, w := range out {
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		if call("GET", "/api/v1/me", "", false, w.Result().Cookies()).Code == 200 {
			survivors++
		}
	}
	if survivors != 1 {
		t.Fatalf("concurrent login winners: %d", survivors)
	}
	me(b, false, 401)
	exec(`INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at)
		SELECT $1,id,'active',now(),now()+interval '30 days' FROM plans WHERE code='premium-30d'`, uid)
	premiumA, premiumB := signIn(false), signIn(false)
	me(premiumA, false, 200)
	me(premiumB, false, 200)
	exec(`UPDATE subscriptions SET status='revoked' WHERE user_id=$1`, uid)
	exec(`UPDATE users SET unrestricted=true WHERE id=$1`, uid)
	first, second := signIn(false), signIn(false)
	me(first, false, 200)
	me(second, false, 200)
	w := call("DELETE", "/api/v1/auth/session", "", false, first)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	me(first, false, 401)
	me(second, false, 200)
	exec(`UPDATE users SET status='suspended' WHERE id=$1`, uid)
	me(second, false, 401)
}

func TestUserAuthenticationDatabaseUnavailable(t *testing.T) {
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := newServer(db, config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "token"})
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("account restore would discard stored login", w.Code)
	}
}
