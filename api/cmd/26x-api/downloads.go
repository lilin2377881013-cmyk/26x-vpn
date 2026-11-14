package main

import (
	"net/http"
	"net/url"
	"strings"
)

func (s *server) listDownloads(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), "SELECT channel,url FROM download_links")
	if err != nil {
		writeError(w, 500, "downloads_unavailable", "无法读取下载地址")
		return
	}
	defer rows.Close()
	links := make(map[string]string)
	for rows.Next() {
		var channel, address string
		if err := rows.Scan(&channel, &address); err != nil {
			writeError(w, 500, "downloads_unavailable", "无法读取下载地址")
			return
		}
		links[channel] = address
	}
	if rows.Err() != nil {
		writeError(w, 500, "downloads_unavailable", "无法读取下载地址")
		return
	}
	writeJSON(w, 200, map[string]any{"links": links})
}

func (s *server) saveDownloads(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	switch channel {
	case "ios-tf", "ios-store", "macos", "windows", "android":
	default:
		writeError(w, 400, "invalid_channel", "未知下载渠道")
		return
	}
	var input struct {
		URL string `json:"url"`
	}
	if decodeJSON(w, r, &input) != nil {
		writeError(w, 400, "invalid_url", "请输入下载地址")
		return
	}
	address := strings.TrimSpace(input.URL)
	if address != "" {
		link, err := url.Parse(address)
		if err != nil || link.Scheme != "https" || link.Hostname() == "" || link.User != nil || len(address) > 2048 ||
			(channel == "ios-tf" && !strings.EqualFold(link.Hostname(), "testflight.apple.com")) ||
			(channel == "ios-store" && !strings.EqualFold(link.Hostname(), "apps.apple.com")) {
			writeError(w, 400, "invalid_url", "请填写此渠道的有效 HTTPS 地址")
			return
		}
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, 500, "save_failed", "无法保存下载地址")
		return
	}
	defer tx.Rollback()
	if address == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM download_links WHERE channel=$1", channel)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO download_links(channel,url) VALUES($1,$2) ON CONFLICT(channel) DO UPDATE SET url=EXCLUDED.url", channel, address)
	}
	if err == nil {
		err = s.auditWithExecutor(ctx, tx, actorFromContext(r), "downloads.updated", "download_channel", channel, map[string]any{"published": address != ""})
	}
	if err != nil || tx.Commit() != nil {
		writeError(w, 500, "save_failed", "无法保存下载地址")
		return
	}
	w.WriteHeader(204)
}
