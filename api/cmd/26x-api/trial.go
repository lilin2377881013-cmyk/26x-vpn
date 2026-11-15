package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (s *server) startTrial(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Installation string `json:"installation"`
		Platform     string `json:"platform"`
		IdentifyOnly bool   `json:"identifyOnly"`
	}
	if decodeJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_trial", "无效的试用请求")
		return
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(input.Installation)
	platformOK := input.Platform == "ios" || input.Platform == "macos" || input.Platform == "windows" || input.Platform == "android"
	if err != nil || len(secret) != 32 || !platformOK {
		writeError(w, http.StatusBadRequest, "invalid_trial", "无效的试用请求")
		return
	}
	hash := sha256.Sum256(secret)
	ctx := r.Context()
	code := strings.ToUpper(hex.EncodeToString(hash[:8]))
	if input.IdentifyOnly {
		var ends sql.NullTime
		err := s.db.QueryRowContext(ctx, `SELECT t.code, max(sub.ends_at) FROM trial_accounts t
   LEFT JOIN subscriptions sub ON sub.user_id=t.user_id
   WHERE t.installation_hash=$1 GROUP BY t.code`, hash[:]).Scan(&code, &ends)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			trialError(w)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"code": code, "endsAt": nullableTime(ends)})
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		trialError(w)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(260009, hashtext(encode($1::bytea,'hex')))`, hash[:]); err != nil {
		trialError(w)
		return
	}
	var user string
	err = tx.QueryRowContext(ctx, `SELECT user_id::text FROM trial_accounts WHERE installation_hash=$1`, hash[:]).Scan(&user)
	if errors.Is(err, sql.ErrNoRows) {
		ip := "trial:" + clientIP(r, s.cfg.trustedProxies)
		if !s.trialLimiter.allow(ip) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "试用申请过于频繁，请稍后再试")
			return
		}
		s.trialLimiter.fail(ip)
		var healthy bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_endpoints e JOIN nodes n ON n.id=e.node_id
			WHERE n.code<>'hk-01' AND UPPER(n.region_code)='HK' AND e.enabled AND e.protocol='vless_reality'
			AND n.status IN ('online','degraded') AND node_monthly_quota_available(n.id,n.monthly_quota_bytes))`).Scan(&healthy); err != nil || !healthy {
			writeError(w, http.StatusServiceUnavailable, "trial_unavailable", "试用线路暂未开放")
			return
		}
		tag := strings.ToLower(code)
		if err = tx.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES ($1,'!trial') RETURNING id::text`,
			tag+"@trial.invalid").Scan(&user); err != nil {
			trialError(w)
			return
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO trial_accounts VALUES ($1,$2,$3)`, hash[:], user, code); err != nil {
			trialError(w)
			return
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at)
			SELECT $1,id,'active',now(),now()+interval '15 minutes' FROM plans WHERE code='trial-15m'`, user); err != nil {
			trialError(w)
			return
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO devices(user_id,platform,label,last_seen_at) VALUES($1,$2,'试用设备',now())`, user, input.Platform); err != nil {
			trialError(w)
			return
		}
	} else if err != nil {
		trialError(w)
		return
	}
	var ends sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT max(s.ends_at) FROM subscriptions s
  JOIN plans p ON p.id=s.plan_id WHERE s.user_id=$1 AND p.code='trial-15m' AND s.status='active'`, user).Scan(&ends); err != nil {
		trialError(w)
		return
	}
	if !ends.Valid || !ends.Time.After(time.Now()) {
		writeError(w, http.StatusGone, "trial_expired", "15 分钟试用已结束")
		return
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT status='active' FROM users WHERE id=$1`, user).Scan(&active); err != nil || !active {
		writeError(w, http.StatusForbidden, "trial_unavailable", "此试用账号不可用")
		return
	}
	if err = tx.Commit(); err != nil {
		trialError(w)
		return
	}
	s.issueSession(w, r, actor{id: user, kind: "user"})
}

func trialError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "trial_failed", "暂时无法开启试用")
}
