package main

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

const connectionLeaseLifetime = 90 * time.Second

func (s *server) acquireConnectionLease(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("id")
	var input struct {
		Token  string `json:"token"`
		Region string `json:"region"`
	}
	if !uuidPattern.MatchString(device) || decodeJSON(w, r, &input) != nil || len(input.Token) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_connection_lease", "Provide a valid device and connection token")
		return
	}
	input.Region = strings.ToUpper(strings.TrimSpace(input.Region))
	switch input.Region {
	case "", "HK", "JP", "SG", "US":
	default:
		writeError(w, http.StatusBadRequest, "invalid_region", "Select a supported region")
		return
	}
	ctx, user := r.Context(), actorFromContext(r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		connectionLeaseError(w)
		return
	}
	defer tx.Rollback()

	// Serialize admissions and renewals across devices of the same member.
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT status = 'active' FROM users WHERE id = $1 FOR UPDATE`, user.id).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !active) {
		writeError(w, http.StatusForbidden, "account_inactive", "An active account is required")
		return
	}
	if err != nil {
		connectionLeaseError(w)
		return
	}
	var validDevice bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL)`,
		device, user.id).Scan(&validDevice)
	if err != nil {
		connectionLeaseError(w)
		return
	}
	if !validDevice {
		writeError(w, http.StatusNotFound, "device_not_found", "Register this device to connect")
		return
	}

	var limit int
	var subscriptionEnd sql.NullTime
	var trial bool
	err = tx.QueryRowContext(ctx, `SELECT CASE WHEN p.tier = 'premium' THEN 2 ELSE 1 END, sub.ends_at, p.code = 'trial-15m'
		FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.user_id = $1 AND sub.status = 'active' AND sub.starts_at <= now()
			AND (sub.ends_at IS NULL OR sub.ends_at > now())
		ORDER BY sub.ends_at DESC NULLS FIRST LIMIT 1`, user.id).Scan(&limit, &subscriptionEnd, &trial)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "subscription_required", "An active subscription is required")
		return
	}
	if err != nil {
		connectionLeaseError(w)
		return
	}

	if trial && (!s.nodeRequiresLease() || input.Region != "") {
		writeError(w, http.StatusForbidden, "trial_unavailable", "试用线路暂未开放")
		return
	}

	var token = input.Token
	var expires time.Time
	var endpoint sql.NullString
	if token != "" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(260008, hashtext(UPPER(n.region_code)))
			FROM connection_leases l JOIN node_endpoints e ON e.id=l.endpoint_id
			JOIN nodes n ON n.id=e.node_id WHERE l.device_id=$1 AND l.user_id=$2`, device, user.id); err != nil {
			connectionLeaseError(w)
			return
		}
		hash := sha256.Sum256([]byte(token))
		err = tx.QueryRowContext(ctx, `UPDATE connection_leases
			SET expires_at = LEAST(clock_timestamp() + ($4 * interval '1 second'), $6::timestamptz)
			WHERE device_id = $1 AND user_id = $2 AND token_hash = $3 AND expires_at > clock_timestamp()
			AND device_id IN (
				SELECT l.device_id FROM connection_leases l
				JOIN devices d ON d.id = l.device_id AND d.revoked_at IS NULL
				WHERE l.user_id = $2 AND l.expires_at > now()
				ORDER BY l.acquired_at, l.device_id LIMIT $5
			)
			AND (endpoint_id IS NULL OR EXISTS (
				SELECT 1 FROM node_endpoints e JOIN nodes n ON n.id=e.node_id
				WHERE e.id=connection_leases.endpoint_id AND e.enabled AND n.status IN ('online','degraded')
			))
			RETURNING expires_at, endpoint_id::text`, device, user.id, hash[:], int(connectionLeaseLifetime.Seconds()), limit, subscriptionEnd).Scan(&expires, &endpoint)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, "connection_lease_expired", "Reconnect to renew VPN access")
			return
		}
	} else {
		var count int
		var clearingThisDevice bool
		var managedEndpoint any
		if s.nodeRequiresLease() {
			managedEndpoint = s.cfg.managedVLESS.endpoint
		}
		// A released or expired lease may still have an installed key. Keep
		// its slot occupied until the node confirms removal of that key.
		err = tx.QueryRowContext(ctx, `WITH live AS (
			SELECT l.device_id FROM connection_leases l
			JOIN devices d ON d.id = l.device_id AND d.revoked_at IS NULL
			WHERE l.user_id = $1 AND l.expires_at > now()
		), installed AS (
			SELECT c.device_id FROM device_node_credentials c JOIN devices d ON d.id = c.device_id
			WHERE d.user_id = $1 AND c.endpoint_id = $3::uuid AND c.managed
				AND c.status IN ('pending', 'active', 'revoking')
		), occupied AS (
			SELECT device_id FROM live UNION SELECT device_id FROM installed
		)
		SELECT (SELECT count(*) FROM occupied WHERE device_id <> $2),
			EXISTS (SELECT 1 FROM installed WHERE device_id = $2)
			AND NOT EXISTS (SELECT 1 FROM live WHERE device_id = $2)`,
			user.id, device, managedEndpoint).Scan(&count, &clearingThisDevice)
		if err != nil {
			connectionLeaseError(w)
			return
		}
		if count >= limit {
			writeError(w, http.StatusConflict, "connection_limit_reached", "Disconnect another device before connecting")
			return
		}
		if clearingThisDevice {
			writeError(w, http.StatusConflict, "connection_releasing", "The previous connection is still closing")
			return
		}
		if input.Region != "" {
			var trialOnlyEndpoint any
			if s.cfg.managedVLESS != nil && s.cfg.managedVLESS.trialOnly {
				trialOnlyEndpoint = s.cfg.managedVLESS.endpoint
			}
			if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(260008, hashtext($1))`, input.Region); err != nil {
				connectionLeaseError(w)
				return
			}
			err = tx.QueryRowContext(ctx, `SELECT e.id::text
				FROM nodes n JOIN node_endpoints e ON e.node_id=n.id AND e.enabled
				JOIN device_node_credentials c ON c.endpoint_id=e.id AND c.device_id=$1
					AND c.status='active' AND c.revoked_at IS NULL
				CROSS JOIN LATERAL (
					SELECT count(DISTINCT l.user_id) AS seats,
						COALESCE(bool_or(l.user_id = $3), false) AS has_user
					FROM connection_leases l
					JOIN node_endpoints le ON le.id=l.endpoint_id
					WHERE le.node_id=n.id AND l.expires_at > clock_timestamp()
				) load
				WHERE ($4::uuid IS NULL OR e.id<>$4)
					AND UPPER(n.region_code)=$2 AND n.status IN ('online','degraded')
					AND (load.has_user OR n.capacity_limit IS NULL OR load.seats < n.capacity_limit)
				ORDER BY CASE WHEN n.status='online' THEN 0 ELSE 1 END,
					load.has_user DESC, load.seats, n.code, e.id LIMIT 1`, device, input.Region, user.id, trialOnlyEndpoint).Scan(&endpoint)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusConflict, "region_unavailable", "No server in this region has space and an active device credential")
				return
			}
			if err != nil {
				connectionLeaseError(w)
				return
			}
		}
		token, err = randomToken(32)
		if err != nil {
			connectionLeaseError(w)
			return
		}
		hash := sha256.Sum256([]byte(token))
		err = tx.QueryRowContext(ctx, `INSERT INTO connection_leases (device_id,user_id,token_hash,expires_at,endpoint_id)
			VALUES ($1,$2,$3,LEAST(clock_timestamp()+($4 * interval '1 second'), $6::timestamptz),$5)
			ON CONFLICT (device_id) DO UPDATE SET token_hash=EXCLUDED.token_hash,
				expires_at=EXCLUDED.expires_at, acquired_at=now(), endpoint_id=EXCLUDED.endpoint_id
			WHERE connection_leases.expires_at <= clock_timestamp()
			RETURNING expires_at`, device, user.id, hash[:], int(connectionLeaseLifetime.Seconds()), endpoint, subscriptionEnd).Scan(&expires)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, "device_already_connected", "This device already holds a connection")
			return
		}
	}
	if err != nil {
		connectionLeaseError(w)
		return
	}
	if subscriptionEnd.Valid && !expires.Before(subscriptionEnd.Time) {
		if _, err := tx.ExecContext(ctx, `UPDATE device_node_credentials
			SET reconcile_after=LEAST(reconcile_after,$2::timestamptz)
			WHERE device_id=$1 AND managed AND status='active'`, device, expires); err != nil {
			connectionLeaseError(w)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		connectionLeaseError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token, "expiresAt": expires.UTC().Format(time.RFC3339),
		"validForSeconds": max(1, int(time.Until(expires).Seconds())),
		"endpointId":      nullableString(endpoint),
	})
}

func (s *server) releaseConnectionLease(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("id")
	var input struct {
		Token string `json:"token"`
	}
	if !uuidPattern.MatchString(device) || decodeJSON(w, r, &input) != nil || len(input.Token) == 0 || len(input.Token) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_connection_lease", "Provide a valid device and connection token")
		return
	}
	hash := sha256.Sum256([]byte(input.Token))
	if _, err := s.db.ExecContext(r.Context(), `WITH released AS (
		DELETE FROM connection_leases WHERE device_id = $1 AND user_id = $2 AND token_hash = $3
		RETURNING device_id
	)
	UPDATE device_node_credentials c SET reconcile_after = now()
	FROM released WHERE c.device_id = released.device_id AND c.managed`,
		device, actorFromContext(r).id, hash[:]); err != nil {
		connectionLeaseError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func connectionLeaseError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "connection_lease_failed", "Could not update the VPN connection slot")
}
