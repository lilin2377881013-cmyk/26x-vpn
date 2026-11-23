package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"strings"
)

func (s *server) registerAccount(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, s.cfg.trustedProxies)
	if !s.registrationLimiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "注册请求过多，请稍后再试")
		return
	}
	s.registrationLimiter.fail(ip)
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_registration", "请填写邮箱地址和密码")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.HasSuffix(email, "@trial.invalid") {
		writeError(w, http.StatusBadRequest, "invalid_email", "请填写有效的邮箱地址")
		return
	}
	if len(input.Password) < 12 || len(input.Password) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_password", "密码长度须为 12 至 256 字节")
		return
	}
	hash, err := s.hashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration_failed", "注册失败，请稍后再试")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration_failed", "注册失败，请稍后再试")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO users(email,password_hash) VALUES($1,$2)
		ON CONFLICT (email) DO NOTHING RETURNING id::text`, email, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "email_registered", "此邮箱已注册，请登录")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO subscriptions(user_id,plan_id,status,starts_at,ends_at)
			VALUES ($1,(SELECT id FROM plans WHERE code='trial-15m'),'active',now(),now()+interval '15 minutes')`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration_failed", "注册失败，请稍后再试")
		return
	}
	s.issueSession(w, r, actor{id: id, kind: "user"}, hash)
}
