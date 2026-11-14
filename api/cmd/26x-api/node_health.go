package main

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// recordNodeHealth records an operator's data-plane check. A health report
// cannot enable a disabled node or provision credentials.
func (s *server) recordNodeHealth(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	nodeID := r.PathValue("id")
	if !uuidPattern.MatchString(nodeID) || decodeJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_node_health", "Provide a valid node and health status")
		return
	}
	switch input.Status {
	case "online", "degraded", "offline":
	default:
		writeError(w, http.StatusBadRequest, "invalid_node_health", "Health status must be online, degraded, or offline")
		return
	}

	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record node health")
		return
	}
	defer tx.Rollback()

	var previous string
	err = tx.QueryRowContext(ctx, `SELECT status FROM nodes WHERE id = $1 FOR UPDATE`, nodeID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "node_not_found", "Node does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record node health")
		return
	}
	if previous == "disabled" {
		writeError(w, http.StatusConflict, "node_disabled", "Enable the node before recording its health")
		return
	}
	var checkedAt time.Time
	err = tx.QueryRowContext(ctx, `UPDATE nodes SET status = $2, health_checked_at = clock_timestamp()
		WHERE id = $1 RETURNING health_checked_at`, nodeID, input.Status).Scan(&checkedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record node health")
		return
	}
	if err := s.auditWithExecutor(ctx, tx, actorFromContext(r), "node.health_recorded", "node", nodeID,
		map[string]any{"previousStatus": previous, "status": input.Status}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to audit node health")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record node health")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": nodeID, "status": input.Status, "healthCheckedAt": checkedAt.UTC().Format(time.RFC3339),
	})
}
