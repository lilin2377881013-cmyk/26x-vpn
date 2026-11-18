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
	"testing"
)

func TestReferralVisitAndDownload(t *testing.T) {
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
	var admin string
	err = db.QueryRow(`INSERT INTO admin_accounts(email,password_hash,role,mfa_required)
		VALUES('referral-fixture@example.invalid','unused','owner',false) RETURNING id::text`).Scan(&admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO download_links(channel,url) VALUES ('android','https://example.invalid/app.apk')`); err != nil {
		t.Fatal(err)
	}
	s := newServer(db, config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/referrals", strings.NewReader(`{"name":"代理测试"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.createReferral(w, req.WithContext(contextWithActor(req, actor{id: admin})))
	var link struct{ Code string }
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &link) != nil || !referralCodePattern.MatchString(link.Code) {
		t.Fatalf("create referral failed: %d %s", w.Code, w.Body.String())
	}
	routes := s.routes()
	visit := "/api/v1/referrals/" + link.Code
	for _, p := range []struct{ path, ua, location string }{
		{visit, "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X)", "/?ref=" + link.Code},
		{visit + "/downloads/android", "Mozilla/5.0 (Linux; Android 14) Mobile", "https://example.invalid/app.apk"},
	} {
		req := httptest.NewRequest(http.MethodGet, p.path, nil)
		req.Header.Set("User-Agent", p.ua)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		if w.Code != http.StatusFound || w.Header().Get("Location") != p.location {
			t.Fatalf("unexpected redirect: %d %s", w.Code, w.Header().Get("Location"))
		}
	}
	req = httptest.NewRequest(http.MethodHead, visit, nil)
	routes.ServeHTTP(httptest.NewRecorder(), req)
	var rows, visits, clicks int64
	if err := db.QueryRow(`SELECT count(*),
			COALESCE(sum(count) FILTER(WHERE channel='' AND device='iPhone'),0),
			COALESCE(sum(count) FILTER(WHERE channel='android' AND device='Android 手机'),0)
		FROM referral_daily WHERE code=$1`, link.Code).Scan(&rows, &visits, &clicks); err != nil || rows != 2 || visits != 1 || clicks != 1 {
		t.Fatalf("unexpected analytics counts: %d %d %d (%v)", rows, visits, clicks, err)
	}
	w = httptest.NewRecorder()
	s.listReferrals(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/referrals", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"visits":1,"downloads":1`) {
		t.Fatalf("unexpected totals: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/referrals", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request reached referral stats: %d", w.Code)
	}
}
