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
)

func TestInstallationEnrollmentAndConnectionLimits(t *testing.T) {
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
	s := newServer(db, config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, fixture := range []struct {
		plan                 string
		devices, connections int
	}{{"standard-30d", 3, 1}, {"premium-30d", 6, 2}} {
		t.Run(fixture.plan, func(t *testing.T) {
			var user string
			if err := db.QueryRow(`INSERT INTO users(email,password_hash) VALUES ($1,'unused') RETURNING id::text`, fixture.plan+"@device-test.invalid").Scan(&user); err != nil {
				t.Fatal(err)
			}
			call := func(handler http.HandlerFunc, body, device string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.SetPathValue("id", device)
				w := httptest.NewRecorder()
				handler(w, r.WithContext(contextWithActor(r, actor{id: user})))
				return w
			}
			body := func(i int) string {
				return fmt.Sprintf(`{"platform":"macos","label":"MacBookAir10,1","installationToken":"%032d"}`, i)
			}
			readDevice := func(w *httptest.ResponseRecorder, status int) string {
				t.Helper()
				var device struct{ ID string }
				if w.Code != status || json.Unmarshal(w.Body.Bytes(), &device) != nil || device.ID == "" {
					t.Fatalf("expected %d, got %d %s", status, w.Code, w.Body.String())
				}
				return device.ID
			}
			devices := []string{readDevice(call(s.registerDevice, body(0), ""), 201)}
			if _, err := db.Exec(`INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at)
			  SELECT $1,id,'active',now(),now()+interval '30 days' FROM plans WHERE code=$2`, user, fixture.plan); err != nil {
				t.Fatal(err)
			}
			for i := 1; i < fixture.devices; i++ {
				devices = append(devices, readDevice(call(s.registerDevice, body(i), ""), 201))
			}
			if restored := readDevice(call(s.registerDevice, body(0), ""), 200); restored != devices[0] {
				t.Fatal("existing installation counted as a new device")
			}
			if w := call(s.registerDevice, body(fixture.devices), ""); w.Code != 409 {
				t.Fatalf("device above limit unexpectedly admitted: %d %s", w.Code, w.Body.String())
			}
			for i, id := range devices {
				w := call(s.acquireConnectionLease, `{}`, id)
				expected := 200
				if i >= fixture.connections {
					expected = 409
				}
				if w.Code != expected {
					t.Fatalf("expected lease status %d, got %d %s", expected, w.Code, w.Body.String())
				}
			}
			if w := call(s.revokeDevice, `{}`, devices[len(devices)-1]); w.Code != 204 {
				t.Fatalf("failed to remove a device: %d %s", w.Code, w.Body.String())
			}
			readDevice(call(s.registerDevice, body(fixture.devices), ""), 201)
		})
	}
}
