package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

var installationTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

func (s *server) updateDeviceLabel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Label string `json:"label"`
	}
	if !uuidPattern.MatchString(id) || decodeJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_device", "设备型号无效")
		return
	}
	label := strings.TrimSpace(input.Label)
	if label == "" || len([]rune(label)) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_device", "设备型号无效")
		return
	}
	var updated string
	err := s.db.QueryRowContext(r.Context(), `UPDATE devices SET label=$3,last_seen_at=now()
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL RETURNING id::text`, id, actorFromContext(r).id, label).Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "device_not_found", "设备已移除")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "device_update_failed", "无法更新设备型号")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
