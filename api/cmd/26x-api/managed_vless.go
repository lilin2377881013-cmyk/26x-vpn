package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

var validInboundTag = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type managedVLESS struct {
	endpoint     string
	socket       string
	tag          string
	helper       string
	xray         string
	requireLease bool
	trialOnly    bool
}

func (s *server) nodeRequiresLease() bool {
	return s.cfg.managedVLESS != nil && s.cfg.managedVLESS.requireLease
}

func (s *server) managedEndpointID() any {
	if s.cfg.managedVLESS == nil {
		return nil
	}
	return s.cfg.managedVLESS.endpoint
}

func (s *server) syncManagedCredentials(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.queueManagedCredentials(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("queue managed credentials", "error", err)
		}
		for i := 0; i < 16 && ctx.Err() == nil; i++ {
			worked, err := s.reconcileManagedCredential(ctx)
			if err != nil {
				s.logger.Warn("reconcile managed credential", "error", err)
				break
			}
			if !worked {
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

const eligibleDevice = `d.revoked_at IS NULL AND u.status = 'active'
	AND e.enabled AND n.status IN ('online', 'degraded')
	AND EXISTS (
		SELECT 1 FROM subscriptions s WHERE s.user_id = u.id AND s.status = 'active'
		AND s.starts_at <= now() AND (s.ends_at IS NULL OR s.ends_at > now())
	)`

func (s *server) queueManagedCredentials(ctx context.Context) error {
	endpoint := s.cfg.managedVLESS.endpoint
	rows, err := s.db.QueryContext(ctx, `SELECT d.id::text FROM devices d JOIN users u ON u.id = d.user_id
		JOIN node_endpoints e ON e.id = $1 AND e.protocol = 'vless_reality'
		JOIN nodes n ON n.id = e.node_id
		LEFT JOIN device_node_credentials c ON c.device_id = d.id AND c.endpoint_id = e.id
		WHERE (`+eligibleDevice+`) AND (c.id IS NULL OR c.managed AND c.status = 'revoked')
		AND (NOT $2 OR EXISTS (SELECT 1 FROM connection_leases l WHERE l.device_id = d.id AND l.expires_at > now()))
		AND (NOT $3 OR EXISTS(SELECT 1 FROM trial_accounts WHERE user_id=d.user_id))
		ORDER BY d.created_at, d.id LIMIT 32`, endpoint, s.nodeRequiresLease(), s.cfg.managedVLESS.trialOnly)
	if err != nil {
		return err
	}
	var devices []string
	for rows.Next() {
		var device string
		if err := rows.Scan(&device); err != nil {
			rows.Close()
			return err
		}
		devices = append(devices, device)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, device := range devices {
		credential, err := newVLESSCredential()
		if err != nil {
			return err
		}
		sealed, err := sealNodeCredential(s.cfg.credentialKey, device, endpoint, credentialKeyVersion, credential)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO device_node_credentials
			(device_id, endpoint_id, credential_ciphertext, key_version, status, managed)
			SELECT $1, $2, $3, $4, 'pending', TRUE
			WHERE NOT $5 OR EXISTS (
				SELECT 1 FROM connection_leases WHERE device_id = $1 AND expires_at > now()
				FOR UPDATE
			)
			ON CONFLICT (device_id, endpoint_id) DO UPDATE SET credential_ciphertext = EXCLUDED.credential_ciphertext,
				key_version = EXCLUDED.key_version, status = 'pending', revoked_at = NULL, reconcile_after = now()
			WHERE device_node_credentials.managed AND device_node_credentials.status = 'revoked'`,
			device, endpoint, sealed, credentialKeyVersion, s.nodeRequiresLease())
		if err != nil {
			return err
		}
	}
	return nil
}

func newVLESSCredential() (nodeCredentialSecret, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nodeCredentialSecret{}, err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return nodeCredentialSecret{
		Credential: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]),
		Flow:       "xtls-rprx-vision", Fingerprint: "chrome",
	}, nil
}

func (s *server) reconcileManagedCredential(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, device, status, version string
	var eligible bool
	var sealed []byte
	var leaseExpires sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT c.id::text, d.id::text, c.status, c.key_version, c.credential_ciphertext,
		COALESCE((`+eligibleDevice+`), FALSE),
		l.expires_at
		FROM device_node_credentials c JOIN devices d ON d.id = c.device_id JOIN users u ON u.id = d.user_id
		JOIN node_endpoints e ON e.id = c.endpoint_id JOIN nodes n ON n.id = e.node_id
		LEFT JOIN connection_leases l ON l.device_id=d.id AND l.expires_at > now()
		WHERE c.managed AND c.endpoint_id = $1 AND c.status IN ('pending','active','revoking')
			AND c.reconcile_after <= now()
		ORDER BY c.reconcile_after, c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED`, s.cfg.managedVLESS.endpoint).
		Scan(&id, &device, &status, &version, &sealed, &eligible, &leaseExpires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	next := "active"
	requiresLease := s.nodeRequiresLease()
	eligible = eligible && (!requiresLease || leaseExpires.Valid && time.Now().Before(leaseExpires.Time))
	if !requiresLease {
		leaseExpires = sql.NullTime{}
	}
	if !eligible || status == "revoking" {
		next = "revoked"
		err = s.cfg.managedVLESS.apply(ctx, "revoke", device, nodeCredentialSecret{})
	} else {
		var secret nodeCredentialSecret
		secret, err = openNodeCredential(s.cfg.credentialKey, sealed, device, s.cfg.managedVLESS.endpoint, version)
		if err == nil {
			err = s.cfg.managedVLESS.apply(ctx, "ensure", device, secret)
		}
	}
	if err != nil {
		// Persist the same encrypted credential across retries. An unavailable
		// node does not confirm a pending credential or a revocation.
		if _, retryErr := tx.ExecContext(ctx, `UPDATE device_node_credentials
			SET reconcile_after = now() + interval '15 seconds' WHERE id = $1`, id); retryErr != nil {
			return true, retryErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return true, commitErr
		}
		return true, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE device_node_credentials SET status = $2,
		revoked_at = CASE WHEN $2 = 'revoked' THEN now() ELSE NULL END,
		reconcile_after = LEAST(now() + interval '2 minutes', $3::timestamptz) WHERE id = $1`,
		id, next, leaseExpires)
	if err != nil {
		return true, err
	}
	return true, tx.Commit()
}

func (node *managedVLESS) apply(ctx context.Context, operation, device string, secret nodeCredentialSecret) error {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node.helper, "--xray", node.xray, "--socket", node.socket, "--tag", node.tag, operation, device)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	if operation == "ensure" {
		payload, err := json.Marshal(secret)
		if err != nil {
			return err
		}
		command.Stdin = bytes.NewReader(payload)
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("managed VLESS %s: %w", operation, err)
	}
	return nil
}
