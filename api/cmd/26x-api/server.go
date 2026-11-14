package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "26x_session"
const csrfCookie = "26x_csrf"

type server struct {
	db           *sql.DB
	cfg          config
	logger       *slog.Logger
	limiter      *loginLimiter
	trialLimiter *loginLimiter
	hashSlots    chan struct{}
}

type actor struct {
	id   string
	kind string
	role string
	csrf string
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]loginWindow
}

type loginWindow struct {
	start time.Time
	count int
}

func newServer(db *sql.DB, cfg config, logger *slog.Logger) *server {
	return &server{db: db, cfg: cfg, logger: logger, limiter: &loginLimiter{entries: make(map[string]loginWindow)}, trialLimiter: &loginLimiter{entries: make(map[string]loginWindow)}, hashSlots: make(chan struct{}, 4)}
}

func (s *server) hashPassword(password string) (string, error) {
	s.hashSlots <- struct{}{}
	defer func() { <-s.hashSlots }()
	return hashPassword(password)
}

func (s *server) verifyPassword(encoded, password string) bool {
	s.hashSlots <- struct{}{}
	defer func() { <-s.hashSlots }()
	return verifyPassword(encoded, password)
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/v1/auth/session", s.userLogin)
	mux.HandleFunc("POST /api/v1/auth/trial", s.startTrial)
	mux.HandleFunc("POST /api/v1/me/checkout", s.requireUser(s.withUserCSRF(s.checkout)))
	mux.HandleFunc("POST /api/v1/me/password", s.requireUser(s.withUserCSRF(s.changeMyPassword)))
	mux.HandleFunc("POST /api/v1/me/support", s.requireUser(s.withUserCSRF(s.submitSupport)))
	mux.HandleFunc("GET /api/v1/admin/support", s.requireAdmin(s.listSupport, "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/me", s.requireUser(s.userMe))
	mux.HandleFunc("GET /api/v1/me/plans", s.requireUser(s.listAvailablePlans))
	mux.HandleFunc("GET /api/v1/me/devices", s.requireUser(s.listMyDevices))
	mux.HandleFunc("POST /api/v1/me/devices", s.requireUser(s.withUserCSRF(s.registerDevice)))
	mux.HandleFunc("DELETE /api/v1/me/devices/{id}", s.requireUser(s.withUserCSRF(s.revokeDevice)))
	mux.HandleFunc("GET /api/v1/client/bootstrap", s.requireUser(s.clientBootstrap))
	mux.HandleFunc("GET /api/v1/nodes", s.requireUser(s.clientBootstrap))
	mux.HandleFunc("GET /api/v1/devices/{id}/credentials", s.requireUser(s.deviceCredentials))
	mux.HandleFunc("PUT /api/v1/me/devices/{id}/connection-lease", s.requireUser(s.withUserCSRF(s.acquireConnectionLease)))
	mux.HandleFunc("DELETE /api/v1/me/devices/{id}/connection-lease", s.requireUser(s.withUserCSRF(s.releaseConnectionLease)))
	mux.HandleFunc("DELETE /api/v1/auth/session", s.requireUser(s.withUserCSRF(s.logout)))
	mux.HandleFunc("POST /api/v1/admin/session", s.adminLogin)
	mux.HandleFunc("DELETE /api/v1/admin/session", s.requireAdmin(s.withCSRF(s.logout), "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/me", s.requireAdmin(s.adminMe, "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/overview", s.requireAdmin(s.adminOverview, "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/users", s.requireAdmin(s.listUsers, "owner", "operations", "support"))
	mux.HandleFunc("POST /api/v1/admin/users", s.requireAdmin(s.withCSRF(s.createUser), "owner", "operations"))
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}/status", s.requireAdmin(s.withCSRF(s.changeUserStatus), "owner", "operations"))
	mux.HandleFunc("GET /api/v1/admin/devices", s.requireAdmin(s.listDevices, "owner", "operations", "support"))
	mux.HandleFunc("DELETE /api/v1/admin/devices/{id}", s.requireAdmin(s.withCSRF(s.revokeDeviceByAdmin), "owner", "operations"))
	mux.HandleFunc("POST /api/v1/admin/devices/{id}/node-credentials", s.requireAdmin(s.withCSRF(s.setDeviceNodeCredential), "owner", "operations"))
	mux.HandleFunc("GET /api/v1/admin/plans", s.requireAdmin(s.listPlans, "owner", "operations", "support"))
	mux.HandleFunc("POST /api/v1/admin/plans", s.requireAdmin(s.withCSRF(s.createPlan), "owner", "operations"))
	mux.HandleFunc("POST /api/v1/admin/users/{id}/subscriptions", s.requireAdmin(s.withCSRF(s.assignSubscription), "owner", "operations"))
	mux.HandleFunc("GET /api/v1/admin/nodes", s.requireAdmin(s.scriptNodeList, "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/nodes/records", s.requireAdmin(s.listNodes, "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/nodes/{code}/health", s.requireAdmin(s.scriptNodeAction("health", 60*time.Second), "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/nodes/{code}/traffic", s.requireAdmin(s.scriptNodeAction("traffic", 30*time.Second), "owner", "operations", "support"))
	mux.HandleFunc("POST /api/v1/admin/nodes/{code}/pause", s.requireAdmin(s.withCSRF(s.scriptNodeAction("pause", 60*time.Second)), "owner", "operations"))
	mux.HandleFunc("POST /api/v1/admin/nodes/{code}/resume", s.requireAdmin(s.withCSRF(s.scriptNodeAction("resume", 60*time.Second)), "owner", "operations"))
	mux.HandleFunc("POST /api/v1/admin/nodes", s.requireAdmin(s.withCSRF(s.deployNode), "owner", "operations"))
	mux.HandleFunc("POST /api/v1/admin/nodes/register", s.requireAdmin(s.withCSRF(s.createNode), "owner", "operations"))
	mux.HandleFunc("GET /api/v1/admin/nodes/{code}", s.requireAdmin(s.getNode, "owner", "operations", "support"))
	mux.HandleFunc("PUT /api/v1/admin/nodes/{code}", s.requireAdmin(s.withCSRF(s.editNode), "owner", "operations"))
	mux.HandleFunc("DELETE /api/v1/admin/nodes/{code}", s.requireAdmin(s.withCSRF(s.removeNode), "owner", "operations"))
	mux.HandleFunc("GET /api/v1/admin/nodes/{code}/stats", s.requireAdmin(s.nodeStats, "owner", "operations", "support"))
	mux.HandleFunc("GET /api/v1/admin/users/{id}/usage", s.requireAdmin(s.userUsage, "owner", "operations", "support"))
	mux.HandleFunc("PATCH /api/v1/admin/nodes/{id}/enabled", s.requireAdmin(s.withCSRF(s.setNodeEnabled), "owner", "operations"))
	mux.HandleFunc("PATCH /api/v1/admin/nodes/{id}/health", s.requireAdmin(s.withCSRF(s.recordNodeHealth), "owner", "operations"))
	mux.HandleFunc("GET /api/v1/admin/audit", s.requireAdmin(s.listAudit, "owner", "operations", "support"))
	return s.middleware(mux)
}

func (s *server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.cfg.trustedOrigins[origin] {
				writeError(w, http.StatusForbidden, "origin_not_allowed", "Request origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("http handler panic", "method", r.Method, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Control plane is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "26x-control-plane"})
}

func (s *server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r, s.cfg.trustedProxies)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many sign-in attempts. Try again later.")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide a valid email and password")
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	var id, role, passwordHash string
	var disabledAt sql.NullTime
	var mfaRequired bool
	err := s.db.QueryRowContext(r.Context(), `SELECT id, role, password_hash, disabled_at, mfa_required
		FROM admin_accounts WHERE email = $1`, input.Email).Scan(&id, &role, &passwordHash, &disabledAt, &mfaRequired)
	if err != nil || disabledAt.Valid || !s.verifyPassword(passwordHash, input.Password) {
		s.limiter.fail(clientIP(r, s.cfg.trustedProxies))
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect")
		return
	}
	if mfaRequired {
		writeError(w, http.StatusForbidden, "mfa_setup_required", "This administrator account requires MFA before sign-in is enabled")
		return
	}
	s.limiter.clear(clientIP(r, s.cfg.trustedProxies))
	s.issueSession(w, r, actor{id: id, kind: "admin", role: role})
}

func (s *server) userLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r, s.cfg.trustedProxies)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many sign-in attempts. Try again later.")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide a valid email and password")
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	var id, passwordHash, status string
	err := s.db.QueryRowContext(r.Context(), `SELECT id, password_hash, status FROM users WHERE email = $1`, input.Email).Scan(&id, &passwordHash, &status)
	if err != nil || status != "active" || !s.verifyPassword(passwordHash, input.Password) {
		s.limiter.fail(clientIP(r, s.cfg.trustedProxies))
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect")
		return
	}
	s.limiter.clear(clientIP(r, s.cfg.trustedProxies))
	s.issueSession(w, r, actor{id: id, kind: "user"})
}

func (s *server) issueSession(w http.ResponseWriter, r *http.Request, current actor) {
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create a session")
		return
	}
	csrf, err := randomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create a session")
		return
	}
	expires := time.Now().UTC().Add(12 * time.Hour)
	tokenHash := sha256.Sum256([]byte(token))
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO sessions(actor_kind, actor_id, token_hash, csrf_token, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, current.kind, current.id, tokenHash[:], csrf, expires)
	if err != nil {
		s.logger.Error("create session", "kind", current.kind, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create a session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, MaxAge: 12 * 60 * 60, HttpOnly: true, Secure: s.cfg.cookieSecure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: csrf, Path: "/", Expires: expires, MaxAge: 12 * 60 * 60, Secure: s.cfg.cookieSecure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusCreated, map[string]any{"role": current.role, "expiresAt": expires.Format(time.RFC3339)})
}

func (s *server) requireAdmin(next http.HandlerFunc, allowedRoles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current, err := s.authenticate(r, "admin")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign_in_required", "Sign in to the 26x control console")
			return
		}
		allowed := false
		for _, role := range allowedRoles {
			if current.role == role {
				allowed = true
				break
			}
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "insufficient_role", "Your account cannot perform this action")
			return
		}
		r = r.WithContext(contextWithActor(r, current))
		next(w, r)
	}
}

func (s *server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current, err := s.authenticate(r, "user")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign_in_required", "Sign in to your 26x account")
			return
		}
		r = r.WithContext(contextWithActor(r, current))
		next(w, r)
	}
}

func (s *server) authenticate(r *http.Request, kind string) (actor, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return actor{}, errors.New("missing session")
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	var current actor
	var expires time.Time
	err = s.db.QueryRowContext(r.Context(), `SELECT s.actor_id::text, s.actor_kind, s.csrf_token, s.expires_at,
		CASE WHEN s.actor_kind = 'admin' THEN COALESCE(a.role, '') ELSE '' END
		FROM sessions s
		LEFT JOIN admin_accounts a ON s.actor_kind = 'admin' AND a.id = s.actor_id AND a.disabled_at IS NULL
		LEFT JOIN users u ON s.actor_kind = 'user' AND u.id = s.actor_id AND u.status = 'active'
		WHERE s.token_hash = $1 AND s.actor_kind = $2 AND s.revoked_at IS NULL AND s.expires_at > now()
		AND ((s.actor_kind = 'admin' AND a.id IS NOT NULL) OR (s.actor_kind = 'user' AND u.id IS NOT NULL))`, tokenHash[:], kind).Scan(&current.id, &current.kind, &current.csrf, &expires, &current.role)
	if err != nil {
		return actor{}, err
	}
	return current, nil
}

func (s *server) withCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current, err := s.authenticate(r, "admin")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign_in_required", "Sign in to the 26x control console")
			return
		}
		csrf, err := r.Cookie(csrfCookie)
		if err != nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(current.csrf)) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(current.csrf)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_check_failed", "Refresh the console and try again")
			return
		}
		r = r.WithContext(contextWithActor(r, current))
		next(w, r)
	}
}

func (s *server) withUserCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current, err := s.authenticate(r, "user")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign_in_required", "Sign in to your 26x account")
			return
		}
		csrf, err := r.Cookie(csrfCookie)
		if err != nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(current.csrf)) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(current.csrf)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_check_failed", "Refresh the client session and try again")
			return
		}
		r = r.WithContext(contextWithActor(r, current))
		next(w, r)
	}
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		hash := sha256.Sum256([]byte(cookie.Value))
		_, _ = s.db.ExecContext(r.Context(), "UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND actor_id = $2", hash[:], current.id)
	}
	clearCookie(w, sessionCookie, true, s.cfg.cookieSecure)
	clearCookie(w, csrfCookie, false, s.cfg.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) adminMe(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	var email string
	if err := s.db.QueryRowContext(r.Context(), "SELECT email::text FROM admin_accounts WHERE id = $1", current.id).Scan(&email); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": current.id, "email": email, "role": current.role, "csrfToken": current.csrf})
}

func (s *server) userMe(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	var email, status string
	var planName, planTier, trialCode sql.NullString
	var endsAt sql.NullTime
	err := s.db.QueryRowContext(r.Context(), `SELECT u.email::text, u.status,
		(SELECT p.name FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		 WHERE sub.user_id = u.id AND sub.status = 'active' AND sub.starts_at <= now()
		 AND (sub.ends_at IS NULL OR sub.ends_at > now()) ORDER BY sub.ends_at DESC NULLS FIRST LIMIT 1),
		(SELECT p.tier FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		 WHERE sub.user_id = u.id AND sub.status = 'active' AND sub.starts_at <= now()
		 AND (sub.ends_at IS NULL OR sub.ends_at > now()) ORDER BY sub.ends_at DESC NULLS FIRST LIMIT 1),
		(SELECT ends_at FROM subscriptions WHERE user_id = u.id AND status = 'active' AND starts_at <= now()
		 AND (ends_at IS NULL OR ends_at > now()) ORDER BY ends_at DESC NULLS FIRST LIMIT 1)
		,(SELECT code FROM trial_accounts t WHERE t.user_id=u.id)
		FROM users u WHERE u.id = $1`, current.id).Scan(&email, &status, &planName, &planTier, &endsAt, &trialCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read account")
		return
	}
	var subscriptionEnds any
	if endsAt.Valid {
		subscriptionEnds = endsAt.Time.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": current.id, "email": email, "status": status, "trialCode": nullableString(trialCode), "planName": nullableString(planName), "planTier": nullableString(planTier), "subscriptionEndsAt": subscriptionEnds, "csrfToken": current.csrf})
}

func (s *server) listAvailablePlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id::text, code, name, tier, duration_days, price_cents, currency, quota_bytes, max_devices
		FROM plans WHERE active = TRUE AND price_cents IS NOT NULL ORDER BY tier, duration_days, name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read available plans")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, code, name, tier, currency string
		var durationDays, maxDevices int
		var price, quota sql.NullInt64
		if err := rows.Scan(&id, &code, &name, &tier, &durationDays, &price, &currency, &quota, &maxDevices); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read available plans")
			return
		}
		items = append(items, map[string]any{"id": id, "code": code, "name": name, "tier": tier, "durationDays": durationDays, "priceCents": nullableInt64(price), "currency": currency, "quotaBytes": nullableInt64(quota), "maxDevices": maxDevices})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read available plans")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &input); err != nil || len(input.NewPassword) < 16 || len(input.NewPassword) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_password", "The new password must be between 16 and 256 characters")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update password")
		return
	}
	defer tx.Rollback()
	var passwordHash string
	if err := tx.QueryRowContext(r.Context(), "SELECT password_hash FROM users WHERE id = $1 AND status = 'active' FOR UPDATE", current.id).Scan(&passwordHash); err != nil || !s.verifyPassword(passwordHash, input.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Current password is incorrect")
		return
	}
	newHash, err := s.hashPassword(input.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update password")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2", newHash, current.id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update password")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE sessions SET revoked_at = now()
		WHERE actor_kind = 'user' AND actor_id = $1 AND revoked_at IS NULL`, current.id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke existing sessions")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update password")
		return
	}
	clearCookie(w, sessionCookie, true, s.cfg.cookieSecure)
	clearCookie(w, csrfCookie, false, s.cfg.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listMyDevices(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	rows, err := s.db.QueryContext(r.Context(), `SELECT id::text, platform, label, app_version, core_version, last_seen_at, created_at
		FROM devices WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, current.id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read devices")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, platform, label string
		var appVersion, coreVersion sql.NullString
		var lastSeen sql.NullTime
		var created time.Time
		if err := rows.Scan(&id, &platform, &label, &appVersion, &coreVersion, &lastSeen, &created); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read devices")
			return
		}
		items = append(items, map[string]any{"id": id, "platform": platform, "label": label, "appVersion": nullableString(appVersion), "coreVersion": nullableString(coreVersion), "lastSeenAt": nullableTime(lastSeen), "createdAt": created.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) registerDevice(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	var input struct {
		Platform    string `json:"platform"`
		Label       string `json:"label"`
		AppVersion  string `json:"appVersion"`
		CoreVersion string `json:"coreVersion"`
	}
	if err := decodeJSON(w, r, &input); err != nil || !validPlatform(input.Platform) || len(strings.TrimSpace(input.Label)) < 1 || len(input.Label) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_device", "Device fields are invalid")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to register device")
		return
	}
	defer tx.Rollback()
	// Serialize concurrent device registrations for this account before checking
	// its limit; otherwise two requests could both consume the last available slot.
	var lockedUserID string
	if err := tx.QueryRowContext(r.Context(), "SELECT id::text FROM users WHERE id = $1 FOR UPDATE", current.id).Scan(&lockedUserID); err != nil {
		writeError(w, http.StatusUnauthorized, "sign_in_required", "Sign in to your 26x account")
		return
	}
	var deviceLimit int
	err = tx.QueryRowContext(r.Context(), `SELECT p.max_devices FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		WHERE s.user_id = $1 AND s.status = 'active' AND s.starts_at <= now() AND (s.ends_at IS NULL OR s.ends_at > now())
		ORDER BY s.ends_at DESC NULLS FIRST LIMIT 1`, current.id).Scan(&deviceLimit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusForbidden, "subscription_required", "An active 26x subscription is required to register a device")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to check device limit")
		}
		return
	}
	var count int
	if err := tx.QueryRowContext(r.Context(), "SELECT count(*) FROM devices WHERE user_id = $1 AND revoked_at IS NULL", current.id).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to check device limit")
		return
	}
	if count >= deviceLimit {
		writeError(w, http.StatusConflict, "device_limit_reached", "Remove a device before adding another")
		return
	}
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO devices(user_id, platform, label, app_version, core_version, last_seen_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), now()) RETURNING id::text`, current.id, input.Platform, strings.TrimSpace(input.Label), input.AppVersion, input.CoreVersion).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to register device")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to register device")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "platform": input.Platform, "label": strings.TrimSpace(input.Label), "credentialStatus": "pending"})
}

func (s *server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	current := actorFromContext(r)
	id := r.PathValue("id")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke device")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE devices SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL", id, current.id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_device", "Device could not be revoked")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, http.StatusNotFound, "device_not_found", "Device does not exist")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE device_node_credentials SET status = 'revoking', reconcile_after = now() WHERE device_id = $1 AND status IN ('pending', 'active', 'error')`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke device credentials")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) clientBootstrap(w http.ResponseWriter, r *http.Request) {
	s.writeClientNodes(w, r, r.URL.Query().Get("deviceId"), false)
}

func (s *server) deviceCredentials(w http.ResponseWriter, r *http.Request) {
	s.writeClientNodes(w, r, r.PathValue("id"), true)
}

func (s *server) writeClientNodes(w http.ResponseWriter, r *http.Request, rawDeviceID string, credentialsOnly bool) {
	current := actorFromContext(r)
	deviceID := strings.ToLower(strings.TrimSpace(rawDeviceID))
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, "device_required", "Register this device before requesting node configuration")
		return
	}
	if !uuidPattern.MatchString(deviceID) {
		writeError(w, http.StatusBadRequest, "invalid_device", "Device ID must be a UUID")
		return
	}
	var deviceExists bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(
		SELECT 1 FROM devices WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	)`, deviceID, current.id).Scan(&deviceExists); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to check device access")
		return
	}
	if !deviceExists {
		writeError(w, http.StatusNotFound, "device_not_found", "Register or select an active device before requesting node configuration")
		return
	}
	var subscriptionActive bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(
		SELECT 1 FROM subscriptions WHERE user_id = $1 AND status = 'active' AND starts_at <= now() AND (ends_at IS NULL OR ends_at > now())
	)`, current.id).Scan(&subscriptionActive); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to check subscription access")
		return
	}
	if !subscriptionActive {
		writeError(w, http.StatusForbidden, "subscription_required", "An active 26x subscription is required to load nodes")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT n.id::text, n.code, n.name, n.region_code, n.status,
		e.id::text, e.protocol, e.host, e.port, e.transport, e.server_name, e.public_key, e.short_id,
		COALESCE(c.status, 'unavailable'), c.credential_ciphertext, c.key_version
		FROM nodes n JOIN node_endpoints e ON e.node_id = n.id AND e.enabled = TRUE
		JOIN devices d ON d.id = $1 AND d.user_id = $2 AND d.revoked_at IS NULL
		LEFT JOIN device_node_credentials c ON c.device_id = d.id AND c.endpoint_id = e.id AND c.revoked_at IS NULL
			AND (NOT $3 OR NOT c.managed OR EXISTS (
				SELECT 1 FROM connection_leases l WHERE l.device_id = d.id AND l.expires_at > now()
			))
		WHERE n.status IN ('online', 'degraded')
		AND CASE WHEN EXISTS(SELECT 1 FROM trial_accounts WHERE user_id=$2)
			THEN e.id=$4::uuid
			ELSE NOT $5 OR e.id<>$4::uuid END
		AND EXISTS (
			SELECT 1 FROM subscriptions
			WHERE user_id = d.user_id AND status = 'active' AND starts_at <= now()
			AND (ends_at IS NULL OR ends_at > now())
		)
		ORDER BY n.name, e.protocol, e.id`, deviceID, current.id, s.nodeRequiresLease(), s.managedEndpointID(), s.cfg.managedVLESS != nil && s.cfg.managedVLESS.trialOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read available nodes")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	credentialsProvisioned := false
	for rows.Next() {
		var nodeID, code, name, region, status, endpointID, protocol, host, transport, credentialStatus string
		var serverName, publicKey, shortID, keyVersion sql.NullString
		var credentialCiphertext []byte
		var port int
		if err := rows.Scan(&nodeID, &code, &name, &region, &status, &endpointID, &protocol, &host, &port, &transport, &serverName, &publicKey, &shortID, &credentialStatus, &credentialCiphertext, &keyVersion); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read available nodes")
			return
		}
		var connectionProfile map[string]any
		connectable := false
		if credentialStatus == "active" && len(credentialCiphertext) > 0 && keyVersion.Valid {
			secret, openErr := openNodeCredential(s.cfg.credentialKey, credentialCiphertext, deviceID, endpointID, keyVersion.String)
			if openErr == nil {
				profileOK := false
				switch protocol {
				case "hysteria2":
					profileOK = serverName.Valid && serverName.String != "" && secret.Credential != "" && secret.HysteriaObfsType == "" && secret.HysteriaObfsPass == ""
				case "vless_reality":
					profileOK = serverName.Valid && serverName.String != "" && publicKey.Valid && publicKey.String != "" && shortID.Valid && secret.Credential != "" && secret.Fingerprint != ""
				}
				if profileOK {
					connectable = true
					credentialsProvisioned = true
					connectionProfile = map[string]any{"protocol": protocol, "host": host, "port": port, "credential": secret.Credential, "serverName": nullableString(serverName), "publicKey": nullableString(publicKey), "shortId": nullableString(shortID), "fingerprint": secret.Fingerprint, "flow": secret.Flow, "spiderX": secret.SpiderX, "alpn": secret.ALPN, "obfsType": secret.HysteriaObfsType, "obfsPassword": secret.HysteriaObfsPass}
				}
			} else {
				credentialStatus = "error"
			}
		}
		if credentialsOnly {
			if connectable {
				items = append(items, map[string]any{"endpointId": endpointID, "connectionProfile": connectionProfile})
			}
			continue
		}
		items = append(items, map[string]any{"id": nodeID, "code": code, "name": name, "region": region, "status": status, "endpoint": map[string]any{"id": endpointID, "protocol": protocol, "host": host, "port": port, "transport": transport, "serverName": nullableString(serverName), "publicKey": nullableString(publicKey), "shortId": nullableString(shortID)}, "credentialStatus": credentialStatus, "connectable": connectable, "connectionProfile": connectionProfile})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read available nodes")
		return
	}
	var trial bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM trial_accounts WHERE user_id=$1)`, current.id).Scan(&trial); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to check trial access")
		return
	}
	regionalLeases := !trial && (!s.nodeRequiresLease() || s.cfg.managedVLESS.trialOnly)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "credentialsProvisioned": credentialsProvisioned, "connectionLeases": true, "regionalLeases": regionalLeases})
}

func (s *server) adminOverview(w http.ResponseWriter, r *http.Request) {
	var users, devices, onlineNodes, activeSubscriptions int64
	queries := []struct {
		dest  *int64
		query string
	}{
		{&users, "SELECT count(*) FROM users WHERE status = 'active'"},
		{&devices, "SELECT count(*) FROM devices WHERE revoked_at IS NULL"},
		{&onlineNodes, "SELECT count(*) FROM nodes WHERE status = 'online'"},
		{&activeSubscriptions, "SELECT count(*) FROM subscriptions WHERE status = 'active' AND (ends_at IS NULL OR ends_at > now())"},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(r.Context(), item.query).Scan(item.dest); err != nil {
			s.logger.Error("overview query", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read service overview")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"activeUsers": users, "activeDevices": devices, "onlineNodes": onlineNodes, "activeSubscriptions": activeSubscriptions, "asOf": time.Now().UTC().Format(time.RFC3339)})
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "Limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 1000000 {
			writeError(w, http.StatusBadRequest, "invalid_offset", "Offset must be between 0 and 1000000")
			return
		}
		offset = parsed
	}
	query := "%" + strings.TrimSpace(r.URL.Query().Get("q")) + "%"
	rows, err := s.db.QueryContext(r.Context(), `SELECT u.id::text, u.email::text, u.status, u.created_at,
		(SELECT count(*) FROM devices d WHERE d.user_id = u.id AND d.revoked_at IS NULL),
		(SELECT p.name FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id WHERE sub.user_id = u.id AND sub.status = 'active' AND (sub.ends_at IS NULL OR sub.ends_at > now()) ORDER BY sub.ends_at DESC NULLS FIRST LIMIT 1),
		(SELECT max(sub.ends_at) FROM subscriptions sub WHERE sub.user_id = u.id AND sub.status = 'active')
		FROM users u WHERE u.email::text ILIKE $1 ORDER BY u.created_at DESC, u.id LIMIT $2 OFFSET $3`, query, limit+1, offset)
	if err != nil {
		s.logger.Error("list users", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read users")
		return
	}
	defer rows.Close()
	users := make([]map[string]any, 0)
	for rows.Next() {
		var id, email, status string
		var created time.Time
		var count int64
		var expires sql.NullTime
		var plan sql.NullString
		if err := rows.Scan(&id, &email, &status, &created, &count, &plan, &expires); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read users")
			return
		}
		var expiry any
		if expires.Valid {
			expiry = expires.Time.UTC().Format(time.RFC3339)
		}
		users = append(users, map[string]any{"id": id, "email": email, "status": status, "createdAt": created.UTC().Format(time.RFC3339), "activeDevices": count, "planName": nullableString(plan), "subscriptionEndsAt": expiry})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read users")
		return
	}
	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users, "limit": limit, "offset": offset, "hasMore": hasMore})
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"temporaryPassword"`
		PlanCode string `json:"planCode"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide a valid email and temporary password")
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if !strings.Contains(input.Email, "@") || len(input.Email) > 320 || len(input.Password) < 16 || len(input.Password) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_user", "Email or temporary password does not meet requirements")
		return
	}
	hash, err := s.hashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create user")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create user")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO users(email, password_hash) VALUES ($1, $2) RETURNING id::text`, input.Email, hash).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "user_exists", "An account with this email already exists")
		return
	}
	var subscriptionID string
	if strings.TrimSpace(input.PlanCode) != "" {
		var planID string
		var durationDays int
		if err := tx.QueryRowContext(r.Context(), "SELECT id::text, duration_days FROM plans WHERE code = $1 AND active = TRUE", strings.TrimSpace(input.PlanCode)).Scan(&planID, &durationDays); err != nil {
			writeError(w, http.StatusBadRequest, "plan_not_found", "Active plan does not exist")
			return
		}
		starts := time.Now().UTC()
		ends := starts.Add(time.Duration(durationDays) * 24 * time.Hour)
		if err := tx.QueryRowContext(r.Context(), `INSERT INTO subscriptions(user_id, plan_id, status, starts_at, ends_at)
			VALUES ($1, $2, 'active', $3, $4) RETURNING id::text`, id, planID, starts, ends).Scan(&subscriptionID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to assign plan")
			return
		}
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "user.created", "user", id, map[string]any{"email": input.Email}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the user creation")
		return
	}
	if subscriptionID != "" {
		if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "subscription.assigned", "user", id, map[string]any{"planCode": input.PlanCode, "subscriptionId": subscriptionID}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the subscription assignment")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create user")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "email": input.Email, "status": "active", "subscriptionId": nullableString(sql.NullString{String: subscriptionID, Valid: subscriptionID != ""})})
}

func (s *server) changeUserStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &input); err != nil || (input.Status != "active" && input.Status != "suspended") {
		writeError(w, http.StatusBadRequest, "invalid_status", "Status must be active or suspended")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "User could not be updated")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE users SET status = $1, updated_at = now() WHERE id = $2", input.Status, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user", "User could not be updated")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, http.StatusNotFound, "user_not_found", "User does not exist")
		return
	}
	if input.Status == "suspended" {
		if _, err := tx.ExecContext(r.Context(), `UPDATE sessions SET revoked_at = now()
			WHERE actor_kind = 'user' AND actor_id = $1 AND revoked_at IS NULL`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke user sessions")
			return
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE device_node_credentials c SET status = 'revoking'
			FROM devices d WHERE c.device_id = d.id AND d.user_id = $1
			AND c.status IN ('pending', 'active', 'error')`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke user credentials")
			return
		}
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "user.status_changed", "user", id, map[string]any{"status": input.Status}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the user status change")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "User could not be updated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": input.Status})
}

func (s *server) listDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT d.id::text, d.user_id::text, u.email::text, d.platform, d.label,
		d.app_version, d.core_version, d.last_seen_at, d.created_at, d.revoked_at
		FROM devices d JOIN users u ON u.id = d.user_id
		ORDER BY d.created_at DESC LIMIT 200`)
	if err != nil {
		s.logger.Error("list devices", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read devices")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, userID, email, platform, label string
		var appVersion, coreVersion sql.NullString
		var lastSeen, revoked sql.NullTime
		var created time.Time
		if err := rows.Scan(&id, &userID, &email, &platform, &label, &appVersion, &coreVersion, &lastSeen, &created, &revoked); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read devices")
			return
		}
		items = append(items, map[string]any{
			"id": id, "userId": userID, "email": email, "platform": platform, "label": label,
			"appVersion": nullableString(appVersion), "coreVersion": nullableString(coreVersion),
			"lastSeenAt": nullableTime(lastSeen), "createdAt": created.UTC().Format(time.RFC3339), "revokedAt": nullableTime(revoked),
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read devices")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": 200})
}

func (s *server) revokeDeviceByAdmin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke device")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE devices SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1", id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_device", "Device could not be revoked")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, http.StatusNotFound, "device_not_found", "Device does not exist")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE device_node_credentials SET status = 'revoking', reconcile_after = now()
		WHERE device_id = $1 AND status IN ('pending', 'active', 'error')`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke device credentials")
		return
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "device.revoked", "device", id, map[string]any{"source": "admin"}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the device revocation")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id::text, code, name, tier, duration_days, price_cents, currency, quota_bytes, max_devices, active
		FROM plans ORDER BY active DESC, tier, duration_days, name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read plans")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, code, name, tier, currency string
		var days int
		var price, quota sql.NullInt64
		var devices int
		var active bool
		if err := rows.Scan(&id, &code, &name, &tier, &days, &price, &currency, &quota, &devices, &active); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read plans")
			return
		}
		items = append(items, map[string]any{"id": id, "code": code, "name": name, "tier": tier, "durationDays": days, "priceCents": nullableInt64(price), "currency": currency, "quotaBytes": nullableInt64(quota), "maxDevices": devices, "active": active})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) createPlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code         string `json:"code"`
		Name         string `json:"name"`
		Tier         string `json:"tier"`
		DurationDays int    `json:"durationDays"`
		PriceCents   int64  `json:"priceCents"`
		QuotaBytes   *int64 `json:"quotaBytes"`
		MaxDevices   int    `json:"maxDevices"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_plan", "Plan fields are invalid")
		return
	}
	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.Tier = strings.ToLower(strings.TrimSpace(input.Tier))
	if input.Tier == "" {
		input.Tier = "standard"
	}
	if len(input.Code) < 2 || len(input.Code) > 48 || len(input.Name) < 2 || len(input.Name) > 80 || (input.Tier != "standard" && input.Tier != "premium") || input.DurationDays < 1 || input.DurationDays > 3650 || input.PriceCents < 1 || input.MaxDevices < 1 || input.MaxDevices > 32 || (input.QuotaBytes != nil && *input.QuotaBytes < 0) {
		writeError(w, http.StatusBadRequest, "invalid_plan", "Plan fields are invalid")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create plan")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO plans(code, name, tier, duration_days, price_cents, currency, quota_bytes, max_devices, active)
		VALUES ($1, $2, $3, $4, $5, 'CNY', $6, $7, TRUE) RETURNING id::text`, strings.TrimSpace(input.Code), strings.TrimSpace(input.Name), input.Tier, input.DurationDays, input.PriceCents, input.QuotaBytes, input.MaxDevices).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "plan_conflict", "Plan could not be created; check whether its code already exists")
		return
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "plan.created", "plan", id, map[string]any{"code": input.Code, "tier": input.Tier, "durationDays": input.DurationDays, "priceCents": input.PriceCents, "maxDevices": input.MaxDevices}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record plan creation")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create plan")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "code": input.Code, "name": input.Name, "tier": input.Tier, "durationDays": input.DurationDays, "priceCents": input.PriceCents, "currency": "CNY", "active": true})
}

func (s *server) assignSubscription(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	var input struct {
		PlanCode string `json:"planCode"`
	}
	if err := decodeJSON(w, r, &input); err != nil || strings.TrimSpace(input.PlanCode) == "" {
		writeError(w, http.StatusBadRequest, "invalid_subscription", "A plan code is required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to assign subscription")
		return
	}
	defer tx.Rollback()
	var planID, planName string
	var durationDays int
	err = tx.QueryRowContext(r.Context(), `SELECT id::text, name, duration_days FROM plans WHERE code = $1 AND active = TRUE`, strings.TrimSpace(input.PlanCode)).Scan(&planID, &planName, &durationDays)
	if err != nil {
		writeError(w, http.StatusNotFound, "plan_not_found", "Active plan does not exist")
		return
	}
	starts := time.Now().UTC()
	ends := starts.Add(time.Duration(durationDays) * 24 * time.Hour)
	var subscriptionID string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO subscriptions(user_id, plan_id, status, starts_at, ends_at)
		VALUES ($1, $2, 'active', $3, $4) RETURNING id::text`, userID, planID, starts, ends).Scan(&subscriptionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user", "Subscription could not be assigned")
		return
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "subscription.assigned", "user", userID, map[string]any{"planCode": input.PlanCode, "subscriptionId": subscriptionID}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the subscription assignment")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to assign subscription")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": subscriptionID, "planName": planName, "startsAt": starts.Format(time.RFC3339), "endsAt": ends.Format(time.RFC3339)})
}

func (s *server) readNodes(ctx context.Context, code string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.id::text, n.code, n.name, n.region_code, n.status, n.health_checked_at,
		n.capacity_limit, n.ssh_port, n.ssh_user,
		(SELECT count(DISTINCT d.user_id) FROM node_endpoints ne
		 JOIN device_node_credentials c ON c.endpoint_id=ne.id AND c.status='active' AND c.revoked_at IS NULL
		 JOIN devices d ON d.id=c.device_id AND d.revoked_at IS NULL WHERE ne.node_id=n.id),
		e.id::text, e.protocol, e.host, e.port, e.transport, e.server_name, e.public_key, e.short_id, e.enabled
		FROM nodes n LEFT JOIN node_endpoints e ON e.node_id = n.id
		WHERE $1 = '' OR n.code = $1 ORDER BY n.name, e.protocol, e.id`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]map[string]any, 0)
	byID := make(map[string]map[string]any)
	for rows.Next() {
		var id, code, name, region, status string
		var checked sql.NullTime
		var capacity sql.NullInt64
		var sshPort, users int64
		var sshUser string
		var endpointID, protocol, host, transport, serverName, publicKey, shortID sql.NullString
		var port sql.NullInt64
		var enabled sql.NullBool
		if err := rows.Scan(&id, &code, &name, &region, &status, &checked, &capacity, &sshPort, &sshUser, &users, &endpointID, &protocol, &host, &port, &transport, &serverName, &publicKey, &shortID, &enabled); err != nil {
			return nil, err
		}
		node, ok := byID[id]
		if !ok {
			var health any
			if checked.Valid {
				health = checked.Time.UTC().Format(time.RFC3339)
			}
			var limit any
			if capacity.Valid {
				limit = capacity.Int64
			}
			node = map[string]any{"id": id, "code": code, "name": name, "region": region, "region_code": region, "status": status, "healthCheckedAt": health, "capacity_limit": limit, "ssh_port": sshPort, "ssh_user": sshUser, "users": users, "endpoints": make([]map[string]any, 0)}
			byID[id] = node
			nodes = append(nodes, node)
		}
		if endpointID.Valid {
			endpoints := node["endpoints"].([]map[string]any)
			endpoints = append(endpoints, map[string]any{"id": endpointID.String, "protocol": protocol.String, "host": host.String, "port": port.Int64, "transport": transport.String, "serverName": nullableString(serverName), "publicKey": nullableString(publicKey), "shortId": nullableString(shortID), "enabled": enabled.Bool})
			node["endpoints"] = endpoints
		}
	}
	return nodes, rows.Err()
}

func (s *server) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.readNodes(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read nodes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "items": nodes})
}

func (s *server) createNode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code       string `json:"code"`
		Name       string `json:"name"`
		Region     string `json:"region"`
		Host       string `json:"host"`
		Protocol   string `json:"protocol"`
		Port       int    `json:"port"`
		Transport  string `json:"transport"`
		ServerName string `json:"serverName"`
		PublicKey  string `json:"publicKey"`
		ShortID    string `json:"shortId"`
	}
	if err := decodeJSON(w, r, &input); err != nil || !validNodeInput(input.Code, input.Name, input.Region, input.Host, input.Protocol, input.Port, input.Transport, input.ServerName, input.PublicKey, input.ShortID) {
		writeError(w, http.StatusBadRequest, "invalid_node", "Node endpoint fields are invalid")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create node")
		return
	}
	defer tx.Rollback()
	var nodeID, endpointID string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO nodes(code, name, region_code, status) VALUES ($1, $2, $3, 'provisioning') RETURNING id::text`, strings.TrimSpace(input.Code), strings.TrimSpace(input.Name), strings.ToUpper(strings.TrimSpace(input.Region))).Scan(&nodeID)
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO node_endpoints(node_id, protocol, host, port, transport, server_name, public_key, short_id)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, '')) RETURNING id::text`, nodeID, input.Protocol, strings.TrimSpace(input.Host), input.Port, input.Transport, strings.TrimSpace(input.ServerName), strings.TrimSpace(input.PublicKey), strings.TrimSpace(input.ShortID)).Scan(&endpointID)
	}
	if err != nil {
		writeError(w, http.StatusConflict, "node_conflict", "Node could not be created; check whether its code or endpoint already exists")
		return
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "node.created", "node", nodeID, map[string]any{"endpointId": endpointID, "protocol": input.Protocol}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record node creation")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to save node")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": nodeID, "endpointId": endpointID, "status": "provisioning"})
}

func (s *server) setNodeEnabled(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil || input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_node_state", "Provide an enabled value")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update node")
		return
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(r.Context(), `UPDATE nodes SET status = CASE
		WHEN $2 = FALSE THEN 'disabled'
		WHEN status = 'disabled' THEN 'provisioning'
		ELSE status END
		WHERE id = $1 RETURNING status`, id, *input.Enabled).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "node_not_found", "Node does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node", "Node could not be updated")
		return
	}
	if !*input.Enabled {
		if _, err := tx.ExecContext(r.Context(), `UPDATE device_node_credentials c SET status = 'revoking'
			FROM node_endpoints e WHERE c.endpoint_id = e.id AND e.node_id = $1
			AND c.status IN ('pending', 'active', 'error')`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to revoke node credentials")
			return
		}
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "node.enabled_changed", "node", id, map[string]any{"enabled": *input.Enabled, "status": status}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the node change")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update node")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": *input.Enabled, "status": status})
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT e.id, COALESCE(a.email::text, 'system'), e.action, e.target_type, e.target_id, e.metadata, e.created_at
		FROM audit_events e LEFT JOIN admin_accounts a ON a.id = e.actor_admin_id ORDER BY e.created_at DESC LIMIT 100`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read audit log")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var actorEmail, action, targetType, targetID string
		var metadata []byte
		var created time.Time
		if err := rows.Scan(&id, &actorEmail, &action, &targetType, &targetID, &metadata, &created); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read audit log")
			return
		}
		var details any
		_ = json.Unmarshal(metadata, &details)
		items = append(items, map[string]any{"id": id, "actor": actorEmail, "action": action, "targetType": targetType, "targetId": targetID, "metadata": details, "createdAt": created.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type auditExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *server) auditWithExecutor(ctx context.Context, executor auditExecutor, current actor, action, targetType, targetID string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO audit_events(actor_admin_id, action, target_type, target_id, metadata) VALUES ($1, $2, $3, $4, $5)`, current.id, action, targetType, targetID, encoded)
	return err
}

func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) > 1024 {
		for ip, entry := range l.entries {
			if now.Sub(entry.start) > 15*time.Minute {
				delete(l.entries, ip)
			}
		}
	}
	entry := l.entries[key]
	if now.Sub(entry.start) > 15*time.Minute {
		entry = loginWindow{start: now}
	}
	if entry.count >= 10 {
		l.entries[key] = entry
		return false
	}
	return true
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if time.Since(entry.start) > 15*time.Minute {
		entry = loginWindow{start: time.Now()}
	}
	entry.count++
	l.entries[key] = entry
}

func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func validNodeInput(code, name, region, host, protocol string, port int, transport, serverName, publicKey, shortID string) bool {
	if len(code) < 2 || len(code) > 48 || len(name) < 2 || len(name) > 80 || len(region) < 2 || len(region) > 8 || len(host) > 253 || port < 1 || port > 65535 {
		return false
	}
	if net.ParseIP(host) == nil && (strings.ContainsAny(host, "/:@ ") || !strings.Contains(host, ".")) {
		return false
	}
	if (protocol == "hysteria2" && transport != "udp") || (protocol == "vless_reality" && transport != "tcp") || (protocol != "hysteria2" && protocol != "vless_reality") {
		return false
	}
	if protocol == "vless_reality" && (serverName == "" || publicKey == "" || shortID == "") {
		return false
	}
	return true
}

func validPlatform(platform string) bool {
	switch platform {
	case "ios", "macos", "windows", "android":
		return true
	default:
		return false
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		return errors.New("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("request must contain one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func randomToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return host
	}
	if !ipInRanges(peer, trustedProxies) {
		return peer.String()
	}
	chain := make([]net.IP, 0)
	for _, entry := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if forwarded := net.ParseIP(strings.TrimSpace(entry)); forwarded != nil {
			chain = append(chain, forwarded)
		}
	}
	chain = append(chain, peer)
	for i := len(chain) - 1; i >= 0; i-- {
		if !ipInRanges(chain[i], trustedProxies) {
			return chain[i].String()
		}
	}
	return chain[0].String()
}

func parseCIDRs(value string) ([]*net.IPNet, error) {
	ranges := make([]*net.IPNet, 0)
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			if ip := net.ParseIP(entry); ip != nil {
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				_, network, err = net.ParseCIDR(fmt.Sprintf("%s/%d", ip.String(), bits))
			}
		}
		if err != nil {
			return nil, fmt.Errorf("invalid proxy address or CIDR %q", entry)
		}
		ranges = append(ranges, network)
	}
	return ranges, nil
}

func ipInRanges(ip net.IP, ranges []*net.IPNet) bool {
	for _, network := range ranges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func clearCookie(w http.ResponseWriter, name string, httpOnly, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: httpOnly, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func actorFromContext(r *http.Request) actor {
	current, _ := r.Context().Value(actorContextKey{}).(actor)
	return current
}

type actorContextKey struct{}

func contextWithActor(r *http.Request, current actor) context.Context {
	return context.WithValue(r.Context(), actorContextKey{}, current)
}

func contextWithTimeout(r *http.Request, duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), duration)
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullableTime(value sql.NullTime) any {
	if value.Valid {
		return value.Time.UTC().Format(time.RFC3339)
	}
	return nil
}

func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}
