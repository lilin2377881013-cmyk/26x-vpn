package main

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *server) trafficLimits(node bool, update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		read := "SELECT monthly_quota_bytes,bandwidth_mbps FROM users WHERE id=$1"
		if node {
			id = r.PathValue("code")
			read = "SELECT monthly_quota_bytes,bandwidth_mbps FROM nodes WHERE code=$1"
		}
		if (node && !nodeCodePattern.MatchString(id)) || (!node && !uuidPattern.MatchString(id)) {
			writeError(w, 400, "invalid_target", "无效的记录编号")
			return
		}
		ctx := r.Context()
		if !update {
			var quota, bandwidth sql.NullInt64
			err := s.db.QueryRowContext(ctx, read, id).Scan(&quota, &bandwidth)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, 404, "target_not_found", "记录不存在")
			} else if err != nil {
				writeError(w, 500, "traffic_limits_failed", "无法读取限制")
			} else {
				writeJSON(w, 200, map[string]any{"monthlyQuotaBytes": nullableInt64(quota), "bandwidthMbps": nullableInt64(bandwidth)})
			}
			return
		}
		var input struct {
			MonthlyQuotaBytes *int64 `json:"monthlyQuotaBytes"`
			BandwidthMbps     *int   `json:"bandwidthMbps"`
		}
		if decodeJSON(w, r, &input) != nil ||
			input.MonthlyQuotaBytes != nil && (*input.MonthlyQuotaBytes < 1 || *input.MonthlyQuotaBytes > 9_000_000_000_000_000) ||
			input.BandwidthMbps != nil && (*input.BandwidthMbps < 1 || *input.BandwidthMbps > 100_000 || !node && *input.BandwidthMbps > 3) {
			writeError(w, 400, "invalid_limits", "流量额度或带宽超出允许范围")
			return
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, 500, "traffic_limits_failed", "无法保存限制")
			return
		}
		defer tx.Rollback()
		var savedID string
		target := "user"
		if node {
			target = "node"
			err = tx.QueryRowContext(ctx, "UPDATE nodes SET monthly_quota_bytes=$2,bandwidth_mbps=$3 WHERE code=$1 RETURNING id::text", id, input.MonthlyQuotaBytes, input.BandwidthMbps).Scan(&savedID)
		} else {
			if input.BandwidthMbps == nil {
				defaultMbps := 3
				input.BandwidthMbps = &defaultMbps
			}
			err = tx.QueryRowContext(ctx, "UPDATE users SET monthly_quota_bytes=$2,bandwidth_mbps=$3 WHERE id=$1 RETURNING id::text", id, input.MonthlyQuotaBytes, input.BandwidthMbps).Scan(&savedID)
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "target_not_found", "记录不存在")
			return
		}
		if err == nil {
			err = s.auditWithExecutor(ctx, tx, actorFromContext(r), "traffic_limits.updated", target, savedID, map[string]any{"monthlyQuotaBytes": input.MonthlyQuotaBytes, "bandwidthMbps": input.BandwidthMbps})
		}
		if err != nil || tx.Commit() != nil {
			writeError(w, 500, "traffic_limits_failed", "无法保存限制")
			return
		}
		w.WriteHeader(204)
	}
}
