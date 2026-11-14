package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
)

var iosVersionPattern = regexp.MustCompile(`^[0-9]{1,3}([.][0-9]{1,3}){0,2}$`)

type deviceSystem struct {
	Name    string `json:"osName"`
	Version string `json:"osVersion"`
}

func (info deviceSystem) valid() bool {
	return (info.Name == "iOS" || info.Name == "iPadOS") && iosVersionPattern.MatchString(info.Version)
}

func (s *server) updateDeviceSystem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var info deviceSystem
	if !uuidPattern.MatchString(id) || decodeJSON(w, r, &info) != nil || !info.valid() {
		writeError(w, http.StatusBadRequest, "invalid_device_system", "Provide a valid iOS or iPadOS version")
		return
	}
	var updated string
	err := s.db.QueryRowContext(r.Context(), `UPDATE devices SET ios_system=$3, ios_version=$4, last_seen_at=now()
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND platform='ios' RETURNING id::text`,
		id, actorFromContext(r).id, info.Name, info.Version).Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "device_not_found", "iOS device not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update device system")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
