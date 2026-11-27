package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var referralCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16}$`)

func (s *server) createReferral(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_referral", "请输入代理名称")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_referral", "代理名称须为 1 至 80 个字符")
		return
	}
	code, err := randomToken(12)
	ctx := r.Context()
	if err == nil {
		var tx *sql.Tx
		tx, err = s.db.BeginTx(ctx, nil)
		if err == nil {
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, "INSERT INTO referral_links(code,name) VALUES ($1,$2)", code, input.Name)
			if err == nil {
				err = s.auditWithExecutor(ctx, tx, actorFromContext(r), "referral.created", "referral", code, map[string]any{"name": input.Name})
			}
			if err == nil {
				err = tx.Commit()
			}
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "referral_failed", "无法生成代理链接")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"code": code, "name": input.Name})
}

func (s *server) listReferrals(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT l.code,l.name,l.created_at,
		COALESCE(sum(d.count) FILTER(WHERE d.channel=''),0),
		COALESCE(sum(d.count) FILTER(WHERE d.channel<>''),0)
		FROM referral_links l LEFT JOIN referral_daily d ON d.code=l.code
		GROUP BY l.code ORDER BY l.created_at DESC,l.code`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "referral_failed", "无法读取代理数据")
		return
	}
	defer rows.Close()
	type referral struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		CreatedAt string `json:"createdAt"`
		Visits    int64  `json:"visits"`
		Downloads int64  `json:"downloads"`
	}
	items := make([]referral, 0)
	for rows.Next() {
		var row referral
		if err := rows.Scan(&row.Code, &row.Name, &row.CreatedAt, &row.Visits, &row.Downloads); err != nil {
			writeError(w, http.StatusInternalServerError, "referral_failed", "无法读取代理数据")
			return
		}
		items = append(items, row)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "referral_failed", "无法读取代理数据")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) referralStats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var detail json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(json_agg(report ORDER BY day DESC,device,channel),'[]')
		FROM (SELECT day::text AS day,device,channel,count FROM referral_daily
		WHERE code=$1 AND day >= (now() AT TIME ZONE 'Asia/Shanghai')::date-29) report`, code).Scan(&detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "referral_failed", "无法读取统计明细")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": detail})
}

func referralDevice(r *http.Request) string {
	ua := strings.ToLower(r.UserAgent())
	switch {
	case strings.Contains(ua, "ipad") || (strings.Contains(ua, "macintosh") && strings.Contains(ua, "mobile")):
		return "iPad"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipod"):
		return "iPhone"
	case strings.Contains(ua, "android"):
		if strings.Contains(ua, "mobile") {
			return "Android 手机"
		}
		return "Android 平板"
	case strings.Contains(ua, "windows phone"):
		return "Windows 手机"
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os x"):
		return "macOS"
	case strings.Contains(ua, "cros"):
		return "ChromeOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return "其他 / 未识别"
	}
}

func (s *server) countReferral(r *http.Request, code, channel string) {
	if !referralCodePattern.MatchString(code) || r.Method != http.MethodGet ||
		strings.Contains(strings.ToLower(r.Header.Get("Sec-Purpose")+" "+r.Header.Get("Purpose")), "prefetch") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO referral_daily(code,day,device,channel)
  SELECT code,(now() AT TIME ZONE 'Asia/Shanghai')::date,$2,$3 FROM referral_links WHERE code=$1
  ON CONFLICT (code,day,device,channel) DO UPDATE SET count=referral_daily.count+1`, code, referralDevice(r), channel)
	if err != nil {
		s.logger.Warn("referral analytics unavailable", "error", err)
	}
}

func (s *server) referralVisit(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	target := "/"
	if referralCodePattern.MatchString(code) {
		target = "/?ref=" + code
		if !s.cfg.websiteVisitLog {
			s.countReferral(r, code, "")
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *server) referralDownload(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	var target string
	if err := s.db.QueryRowContext(r.Context(), "SELECT url FROM download_links WHERE channel=$1", channel).Scan(&target); err != nil || !validPaymentURL(target) {
		http.NotFound(w, r)
		return
	}
	s.countReferral(r, r.PathValue("code"), channel)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
}
