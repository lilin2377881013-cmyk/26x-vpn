package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func memberChannel(r *http.Request) string {
	if r.Header.Get("X-26x-Portal") == "1" {
		return "portal"
	}
	return "app"
}

// The website issues account-management sessions. VPN admission uses an app session.
func portalAllows(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/v1/me", "/api/v1/me/orders", "/api/v1/me/plans", "/api/v1/me/checkout",
		"/api/v1/me/password", "/api/v1/me/support", "/api/v1/me/devices", "/api/v1/auth/session":
		return true
	}
	return false
}

func (s *server) userAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "sign_in_required", "Sign in to your 26x account")
		return
	}
	s.logger.Error("authenticate user", "error", err)
	writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Account service is temporarily unavailable")
}

// verifiedHash is the credential checked by login or written by registration.
// Admin logins and installation-based guest trials keep their bounded sessions.
func (s *server) issueSession(w http.ResponseWriter, r *http.Request, current actor, verifiedHash string) {
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
	tokenHash := sha256.Sum256([]byte(token))
	cookieTTL := 12 * time.Hour
	expires := time.Now().UTC().Add(cookieTTL)
	var expiresAt any = expires.Format(time.RFC3339)
	if verifiedHash == "" {
		_, err = s.db.ExecContext(r.Context(), `INSERT INTO sessions(actor_kind,actor_id,token_hash,csrf_token,expires_at)
			VALUES ($1,$2,$3,$4,$5)`, current.kind, current.id, tokenHash[:], csrf, expires)
	} else {
		err = s.createMemberSession(r.Context(), current.id, verifiedHash, memberChannel(r), tokenHash[:], csrf)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Sign-in details changed. Sign in again.")
			return
		}
		// Native clients persist the values in secure storage. Browsers impose their own cookie lifetime.
		expiresAt = nil
		cookieTTL = 400 * 24 * time.Hour
		expires = time.Now().UTC().Add(cookieTTL)
	}
	if err != nil {
		s.logger.Error("create session", "kind", current.kind, "error", err)
		writeError(w, http.StatusServiceUnavailable, "session_unavailable", "Unable to create a session")
		return
	}
	sessionName, csrfName := sessionNames(r, current.kind)
	http.SetCookie(w, &http.Cookie{Name: sessionName, Value: token, Path: "/", Expires: expires, MaxAge: int(cookieTTL.Seconds()), HttpOnly: true, Secure: s.cfg.cookieSecure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: csrfName, Value: csrf, Path: "/", Expires: expires, MaxAge: int(cookieTTL.Seconds()), Secure: s.cfg.cookieSecure, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"role": current.role, "expiresAt": expiresAt})
}

func (s *server) createMemberSession(ctx context.Context, user, verifiedHash, channel string, tokenHash []byte, csrf string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var unrestricted bool
	err = tx.QueryRowContext(ctx, `SELECT unrestricted FROM users
		WHERE id=$1 AND password_hash=$2 AND status='active' FOR UPDATE`, user, verifiedHash).Scan(&unrestricted)
	if err != nil {
		return err
	}
	var multipleApps bool
	if channel == "app" && !unrestricted {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT p.tier='premium'
			FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
			WHERE sub.user_id=$1 AND sub.status='active' AND sub.starts_at<=now()
			AND (sub.ends_at IS NULL OR sub.ends_at>now())
			ORDER BY sub.ends_at DESC NULLS FIRST LIMIT 1),false)`, user).Scan(&multipleApps)
		if err != nil {
			return err
		}
	}
	if !unrestricted && !multipleApps {
		// Unknown pre-upgrade tokens are displaced by an app login. A web login
		// only replaces a known web token so a purchase cannot disconnect an app.
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now()
			WHERE actor_kind='user' AND actor_id=$1 AND revoked_at IS NULL
			AND (member_channel=$2 OR ($2='app' AND member_channel='legacy'))`, user, channel)
		if err != nil {
			return err
		}
		if channel == "app" {
			_, err = tx.ExecContext(ctx, `WITH released AS (
				DELETE FROM connection_leases WHERE user_id=$1 RETURNING device_id
			) UPDATE device_node_credentials c SET reconcile_after=now()
			FROM released WHERE c.device_id=released.device_id AND c.managed`, user)
			if err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(actor_kind,actor_id,token_hash,csrf_token,member_channel,expires_at)
		VALUES ('user',$1,$2,$3,$4,NULL)`, user, tokenHash, csrf, channel)
	if err != nil {
		return err
	}
	return tx.Commit()
}
