package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *server) submitSupport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
	}
	if decodeJSON(w, r, &body) != nil {
		writeError(w, 400, "invalid_message", "请填写留言")
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" || strings.ContainsRune(body.Message, 0) || utf8.RuneCountInString(body.Message) > 2000 {
		writeError(w, 400, "invalid_message", "请输入 1 至 2000 字的留言")
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		supportError(w)
		return
	}
	defer tx.Rollback()
	user := actorFromContext(r).id
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(260010,hashtext($1))`, user); err != nil {
		supportError(w)
		return
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO support_tickets(user_id,message)
		SELECT $1,$2 WHERE (SELECT count(*) FROM support_tickets
		WHERE user_id=$1 AND created_at>now()-interval '1 hour')<5 RETURNING id::text`, user, body.Message).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 429, "rate_limited", "留言过于频繁，请稍后再试")
		return
	}
	if err != nil || tx.Commit() != nil {
		supportError(w)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *server) listSupport(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT t.id::text,COALESCE(a.code,u.email::text),t.message,t.created_at,t.notified_at
		FROM support_tickets t JOIN users u ON u.id=t.user_id
		LEFT JOIN trial_accounts a ON a.user_id=u.id ORDER BY t.created_at DESC,t.id DESC LIMIT 100`)
	if err != nil {
		supportError(w)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, user, message string
		var created time.Time
		var notified sql.NullTime
		if rows.Scan(&id, &user, &message, &created, &notified) != nil {
			supportError(w)
			return
		}
		items = append(items, map[string]any{"id": id, "user": user, "message": message, "createdAt": created.UTC(), "notified": notified.Valid})
	}
	if rows.Err() != nil {
		supportError(w)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func supportError(w http.ResponseWriter) {
	writeError(w, 500, "support_error", "客服服务暂不可用，请稍后重试")
}

var localTelegramBotToken string

func (s *server) notifySupport(ctx context.Context) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		token = localTelegramBotToken
	}
	chat := strings.TrimSpace(os.Getenv("TELEGRAM_SUPPORT_CHAT_ID"))
	if token == "" || chat == "" {
		return
	}
	httpClient := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		for i := 0; i < 10; i++ {
			var id, message string
			err := s.db.QueryRowContext(ctx, `UPDATE support_tickets SET notify_after=now()+interval '5 minutes'
				WHERE id=(SELECT id FROM support_tickets WHERE notified_at IS NULL AND notify_after<=now()
				ORDER BY notify_after LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id::text,message`).Scan(&id, &message)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
					s.logger.Warn("support notification queue unavailable")
				}
				break
			}
			body, _ := json.Marshal(map[string]string{"chat_id": chat, "text": "26x 收到客服留言\n工单号 " + id + "\n\n" + message + "\n\nhttps://26xvpn.com/admin/support"})
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(body))
			if err != nil {
				s.logger.Warn("invalid Telegram configuration")
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := httpClient.Do(req)
			if err != nil {
				s.logger.Warn("Telegram support notification failed")
				break
			}
			var result struct {
				OK bool `json:"ok"`
			}
			ok := resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&result) == nil && result.OK
			resp.Body.Close()
			if !ok {
				s.logger.Warn("Telegram support notification rejected", "status", resp.StatusCode)
				break
			}
			if _, err = s.db.ExecContext(ctx, `UPDATE support_tickets SET notified_at=now() WHERE id=$1`, id); err != nil {
				s.logger.Warn("support notification status unavailable")
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
