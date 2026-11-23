package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
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

var errLoginDeviceRemoved = errors.New("login device removed")

func enrollDevice(ctx context.Context, tx *sql.Tx, current actor, platform, label, appVersion, coreVersion, installation string) (string, bool, error) {
	var unrestricted bool
	err := tx.QueryRowContext(ctx, `SELECT unrestricted FROM users WHERE id=$1 AND status='active' FOR UPDATE`, current.id).Scan(&unrestricted)
	if err != nil {
		return "", false, err
	}
	signedIn := time.Now().UTC()
	bound := ""
	if current.memberApp {
		err = tx.QueryRowContext(ctx, `SELECT created_at,COALESCE(device_id::text,'') FROM sessions
			WHERE id=$1 AND actor_id=$2 AND revoked_at IS NULL AND member_channel='app'`, current.sessionID, current.id).Scan(&signedIn, &bound)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errLoginDeviceRemoved
		}
		if err != nil {
			return "", false, err
		}
	}
	if current.memberApp && bound == "" && installation != "" {
		var displaced bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE user_id=$1 AND installation_token=$2
			AND revoked_at >= $3)`, current.id, installation, signedIn).Scan(&displaced)
		if err != nil {
			return "", false, err
		}
		if displaced {
			return "", false, errLoginDeviceRemoved
		}
	}
	var id string
	var removed sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id::text,revoked_at FROM devices WHERE user_id=$1 AND platform=$2
		AND (($3<>'' AND installation_token=$3) OR ($3='' AND id=NULLIF($4,'')::uuid))
		ORDER BY revoked_at DESC NULLS FIRST LIMIT 1`, current.id, platform, installation, bound).Scan(&id, &removed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	created := errors.Is(err, sql.ErrNoRows) || removed.Valid
	if (bound != "" && (created || bound != id)) || (removed.Valid && !signedIn.After(removed.Time)) {
		return "", false, errLoginDeviceRemoved
	}
	if created {
		err = tx.QueryRowContext(ctx, `INSERT INTO devices(user_id,platform,label,app_version,core_version,last_seen_at,installation_token,last_login_at)
			VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),now(),NULLIF($6,''),$7) RETURNING id::text`, current.id, platform, label, appVersion, coreVersion, installation, signedIn).Scan(&id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE devices SET label=$2,last_seen_at=now(),last_login_at=GREATEST(last_login_at,$3) WHERE id=$1`, id, label, signedIn)
	}
	if err != nil {
		return "", false, err
	}
	if current.memberApp && bound == "" {
		// Reauthenticating the same installation replaces only that installation's token.
		var newer bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE device_id=$1 AND revoked_at IS NULL AND created_at>$2)`, id, signedIn).Scan(&newer)
		if err != nil {
			return "", false, err
		}
		if newer {
			return "", false, errLoginDeviceRemoved
		}
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE device_id=$1 AND revoked_at IS NULL AND id<>$2`, id, current.sessionID); err != nil {
			return "", false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET device_id=$2 WHERE id=$1`, current.sessionID, id); err != nil {
			return "", false, err
		}
	}
	if !unrestricted {
		rows, err := tx.QueryContext(ctx, `SELECT id::text FROM devices WHERE user_id=$1 AND revoked_at IS NULL
			ORDER BY last_login_at DESC, created_at DESC, id DESC OFFSET COALESCE(
				(SELECT CASE WHEN p.code='trial-15m' THEN 1 WHEN p.tier='premium' THEN 6 ELSE 3 END
				 FROM subscriptions s JOIN plans p ON p.id=s.plan_id WHERE s.user_id=$1 AND s.status='active'
				 AND s.starts_at<=now() AND (s.ends_at IS NULL OR s.ends_at>now())
				 ORDER BY s.ends_at DESC NULLS FIRST LIMIT 1),3)`, current.id)
		if err != nil {
			return "", false, err
		}
		var surplus []string
		for rows.Next() {
			var old string
			if err := rows.Scan(&old); err != nil {
				rows.Close()
				return "", false, err
			}
			surplus = append(surplus, old)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", false, err
		}
		for _, old := range surplus {
			// A delayed request from an older login must never evict a newer device.
			if old == id {
				return "", false, errLoginDeviceRemoved
			}
			if err := removeLoginDevice(ctx, tx, current.id, old); err != nil {
				return "", false, err
			}
		}
	}
	return id, created, nil
}

func removeLoginDevice(ctx context.Context, tx *sql.Tx, user, device string) error {
	var removed string
	return tx.QueryRowContext(ctx, `WITH removed AS (
		UPDATE devices SET revoked_at=clock_timestamp() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL RETURNING id
	), signed_out AS (
		UPDATE sessions SET revoked_at=clock_timestamp() FROM removed WHERE device_id=removed.id AND revoked_at IS NULL
	), released AS (
		DELETE FROM connection_leases l USING removed WHERE l.device_id=removed.id
	), credentials AS (
		UPDATE device_node_credentials c SET status='revoking',reconcile_after=now()
		FROM removed WHERE c.device_id=removed.id AND c.status IN ('pending','active','error')
	) SELECT id::text FROM removed`, device, user).Scan(&removed)
}

func (s *server) signOutDevice(ctx context.Context, current actor) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, current.id); err != nil {
		return err
	}
	// An old sign-out arriving after a new sign-in must not remove its replacement.
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now()
		WHERE id=$1 AND actor_id=$2 AND device_id=$3 AND revoked_at IS NULL`, current.sessionID, current.id, current.deviceID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		var inUse bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE device_id=$1 AND revoked_at IS NULL)`, current.deviceID).Scan(&inUse); err != nil {
			return err
		}
		if !inUse {
			if err = removeLoginDevice(ctx, tx, current.id, current.deviceID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	}
	return tx.Commit()
}
