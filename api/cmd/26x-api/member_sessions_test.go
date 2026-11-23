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
		return []*http.Cookie{{Name: sessionCookie, Value: token}, {Name: csrfCookie, Value: "csrf"}}
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
	type loginDevice struct {
		cookies []*http.Cookie
		id      string
	}
	enroll := func(cookies []*http.Cookie, n int) loginDevice {
		t.Helper()
		w := call("POST", "/api/v1/me/devices", fmt.Sprintf(`{"platform":"macos","label":"fixture","installationToken":"%032d"}`, n), false, cookies)
		var item struct{ ID string }
		if (w.Code != 201 && w.Code != 200) || json.Unmarshal(w.Body.Bytes(), &item) != nil || item.ID == "" {
			t.Fatal("enroll", w.Code, w.Body.String())
		}
		return loginDevice{cookies, item.ID}
	}
	deviceLogin := func(n int) loginDevice { return enroll(signIn(false), n) }
	reserve := func(d loginDevice, want int) {
		t.Helper()
		w := call("PUT", "/api/v1/me/devices/"+d.id+"/connection-lease", "{}", false, d.cookies)
		if w.Code != want {
			t.Fatal("lease", w.Code, w.Body.String())
		}
	}
	devices := func(d loginDevice, want int) {
		t.Helper()
		w := call("GET", "/api/v1/me/devices", "", false, d.cookies)
		var list struct{ Items []struct{ ID string } }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != want {
			t.Fatal("device inventory", want, w.Code, w.Body.String())
		}
	}
	standard := func(code string) {
		exec(`INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at)
		SELECT $1,id,'active',now(),now()+interval '30 days' FROM plans WHERE code=$2`, uid, code)
	}
	standard("standard-30d")
	a := enroll(live, 0)
	b, c := deviceLogin(1), deviceLogin(2)
	devices(a, 3)
	reserve(b, 200)
	reserve(a, 409)
	if w := login("wrong-password", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	freshA := deviceLogin(0)
	if freshA.id != a.id {
		t.Fatal("repeat login duplicated device")
	}
	reserve(freshA, 409) // Signing into A does not release B's connection.
	me(web, true, 200)
	web = signIn(true)
	me(b.cookies, false, 200)
	d := deviceLogin(3)
	me(b.cookies, false, 401)
	reserve(b, 401)
	reserve(d, 200)
	me(a.cookies, false, 401)
	me(c.cookies, false, 200)
	devices(d, 3)
	enroll(freshA.cookies, 0) // Restoring an existing session must not reorder devices.
	replacement := deviceLogin(4)
	me(c.cookies, false, 401)
	me(freshA.cookies, false, 200)
	devices(replacement, 3)
	sum := sha256.Sum256([]byte(freshA.cookies[0].Value))
	exec(`UPDATE sessions SET created_at=now()-interval '2 days' WHERE token_hash=$1`, sum[:])
	me(freshA.cookies, false, 200)
	var persistent bool
	if err := db.QueryRow(`SELECT expires_at IS NULL FROM sessions WHERE token_hash=$1`, sum[:]).Scan(&persistent); err != nil || !persistent {
		t.Fatal("app session expires", err)
	}
	if w := call("GET", "/api/v1/nodes", "", true, web); w.Code != 401 {
		t.Fatal("portal allowed VPN", w.Code)
	}
	exec(`UPDATE subscriptions SET status='revoked' WHERE user_id=$1`, uid)
	standard("premium-30d")
	p1, p2, p3 := deviceLogin(5), deviceLogin(6), deviceLogin(7)
	devices(p3, 6)
	reserve(p1, 200) // D is still connected, so both premium places are now in use.
	reserve(p2, 409)
	p4 := deviceLogin(8)
	me(freshA.cookies, false, 401)
	me(p1.cookies, false, 200)
	me(p2.cookies, false, 200)
	me(web, true, 200)
	devices(p4, 6)
	// Simultaneous enrollments remain serialized and preserve the newest six devices.
	out := make([]*httptest.ResponseRecorder, 2)
	waiting := [][]*http.Cookie{signIn(false), signIn(false)}
	var wg sync.WaitGroup
	for i := range waiting {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i] = call("POST", "/api/v1/me/devices", fmt.Sprintf(`{"platform":"windows","label":"fixture","installationToken":"%032d"}`, 20+i), false, waiting[i])
		}(i)
	}
	wg.Wait()
	for i, w := range out {
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		me(waiting[i], false, 200)
	}
	devices(p4, 6)
	me(d.cookies, false, 401)
	me(replacement.cookies, false, 401)
	// Unrestricted users exceed both ordinary ceilings.
	exec(`UPDATE users SET unrestricted=true WHERE id=$1`, uid)
	supers := make([]loginDevice, 8)
	for i := range supers {
		supers[i] = deviceLogin(30 + i)
		reserve(supers[i], 200)
	}
	for _, s := range supers {
		me(s.cookies, false, 200)
	}
	devices(supers[0], 14)
	w := call("DELETE", "/api/v1/auth/session", "", false, supers[0].cookies)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	me(supers[0].cookies, false, 401)
	me(supers[1].cookies, false, 200)
	devices(supers[1], 13)
	exec(`UPDATE users SET status='suspended' WHERE id=$1`, uid)
	me(supers[1].cookies, false, 401)

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
