package main

import (
	"net/http"
	"strconv"
	"time"
)

func (s *server) listWebsiteVisits(w http.ResponseWriter, r *http.Request) {
	var before int64
	if value := r.URL.Query().Get("before"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, 400, "invalid_cursor", "翻页参数无效")
			return
		}
		before = n
	}
	const size = 50
	rows, err := s.db.QueryContext(r.Context(), `SELECT v.id,v.visited_at,v.page,
        CASE WHEN v.referral_code IS NULL THEN '无推广码' ELSE COALESCE(l.name,'未登记推广码') END,
        v.device,v.os,v.browser FROM website_visits v
        LEFT JOIN referral_links l ON l.code=v.referral_code
        WHERE v.visited_at >= now() - interval '30 days' AND ($1::bigint=0 OR (v.visited_at,v.id) <
        (SELECT visited_at,id FROM website_visits WHERE id=$1))
        ORDER BY v.visited_at DESC,v.id DESC LIMIT $2`, before, size+1)
	if err != nil {
		writeError(w, 500, "visits_unavailable", "读取官网访客记录失败")
		return
	}
	defer rows.Close()
	type visit struct {
		ID      string    `json:"id"`
		Time    time.Time `json:"time"`
		Page    string    `json:"page"`
		Source  string    `json:"source"`
		Device  string    `json:"device"`
		OS      string    `json:"os"`
		Browser string    `json:"browser"`
	}
	items := make([]visit, 0, size+1)
	for rows.Next() {
		var v visit
		if err := rows.Scan(&v.ID, &v.Time, &v.Page, &v.Source, &v.Device, &v.OS, &v.Browser); err != nil {
			writeError(w, 500, "visits_unavailable", "读取官网访客记录失败")
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		writeError(w, 500, "visits_unavailable", "读取官网访客记录失败")
		return
	}
	more := len(items) > size
	if more {
		items = items[:size]
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items, "hasMore": more})
}
