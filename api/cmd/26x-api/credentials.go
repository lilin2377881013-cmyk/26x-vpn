package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const credentialKeyVersion = "v1"

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type nodeCredentialSecret struct {
	Credential       string   `json:"credential"`
	Fingerprint      string   `json:"fingerprint,omitempty"`
	Flow             string   `json:"flow,omitempty"`
	SpiderX          string   `json:"spiderX,omitempty"`
	ALPN             []string `json:"alpn,omitempty"`
	HysteriaObfsType string   `json:"obfsType,omitempty"`
	HysteriaObfsPass string   `json:"obfsPassword,omitempty"`
}

func (s *server) setDeviceNodeCredential(w http.ResponseWriter, r *http.Request) {
	if len(s.cfg.credentialKey) != 32 {
		writeError(w, http.StatusServiceUnavailable, "credential_encryption_unavailable", "Node credentials are not enabled on this server")
		return
	}
	deviceID := strings.ToLower(r.PathValue("id"))
	var input struct {
		EndpointID   string   `json:"endpointId"`
		Credential   string   `json:"credential"`
		Fingerprint  string   `json:"fingerprint"`
		Flow         string   `json:"flow"`
		SpiderX      string   `json:"spiderX"`
		ALPN         []string `json:"alpn"`
		ObfsType     string   `json:"obfsType"`
		ObfsPassword string   `json:"obfsPassword"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_credential", "A valid node credential request is required")
		return
	}
	input.EndpointID = strings.ToLower(input.EndpointID)
	input.Credential = strings.TrimSpace(input.Credential)
	input.Fingerprint = strings.TrimSpace(input.Fingerprint)
	input.Flow = strings.TrimSpace(input.Flow)
	input.SpiderX = strings.TrimSpace(input.SpiderX)
	input.ObfsType = strings.TrimSpace(input.ObfsType)
	if !uuidPattern.MatchString(deviceID) || !uuidPattern.MatchString(input.EndpointID) || input.Credential == "" || len(input.Credential) > 512 || len(input.Fingerprint) > 32 || len(input.Flow) > 64 || len(input.SpiderX) > 256 || len(input.ObfsType) > 64 || len(input.ObfsPassword) > 256 || len(input.ALPN) > 8 {
		writeError(w, http.StatusBadRequest, "invalid_credential", "The endpoint or credential fields are invalid")
		return
	}
	secret := nodeCredentialSecret{
		Credential: input.Credential, Fingerprint: input.Fingerprint,
		Flow: input.Flow, SpiderX: input.SpiderX, ALPN: input.ALPN,
		HysteriaObfsType: input.ObfsType, HysteriaObfsPass: input.ObfsPassword,
	}

	var platform, protocol, host, serverName string
	var port int
	var publicKey, shortID sql.NullString
	err := s.db.QueryRowContext(r.Context(), `SELECT d.platform, e.protocol, e.host, e.port,
		COALESCE(e.server_name, ''), e.public_key, e.short_id
		FROM devices d JOIN node_endpoints e ON e.id = $2 AND e.enabled = TRUE
		JOIN nodes n ON n.id = e.node_id AND n.status <> 'disabled'
		WHERE d.id = $1 AND d.revoked_at IS NULL`, deviceID, input.EndpointID).
		Scan(&platform, &protocol, &host, &port, &serverName, &publicKey, &shortID)
	if err != nil {
		writeError(w, http.StatusNotFound, "device_or_endpoint_not_found", "The active device or node endpoint does not exist")
		return
	}
	if !validDeviceCredential(protocol, input.Credential, input.Fingerprint, input.Flow, input.ObfsType, input.ObfsPassword) {
		writeError(w, http.StatusBadRequest, "invalid_credential", "The credential does not match the selected protocol")
		return
	}
	if (protocol == "hysteria2" && serverName == "") || (protocol == "vless_reality" && (!publicKey.Valid || !shortID.Valid || serverName == "" || input.Fingerprint == "")) {
		writeError(w, http.StatusBadRequest, "endpoint_incomplete", "The node endpoint is missing required TLS or Reality settings")
		return
	}

	sealed, err := sealNodeCredential(s.cfg.credentialKey, deviceID, input.EndpointID, credentialKeyVersion, secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "credential_store_failed", "The node credential could not be stored")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The node credential could not be stored")
		return
	}
	defer tx.Rollback()
	updated, err := tx.ExecContext(r.Context(), `INSERT INTO device_node_credentials
		(device_id, endpoint_id, credential_ciphertext, key_version, status, revoked_at)
		VALUES ($1, $2, $3, $4, 'active', NULL)
		ON CONFLICT (device_id, endpoint_id) DO UPDATE SET credential_ciphertext = EXCLUDED.credential_ciphertext,
		key_version = EXCLUDED.key_version, status = 'active', revoked_at = NULL
		WHERE NOT device_node_credentials.managed`, deviceID, input.EndpointID, sealed, credentialKeyVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "credential_store_failed", "The node credential could not be stored")
		return
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "credential_store_failed", "The node credential could not be stored")
		return
	}
	if affected == 0 {
		writeError(w, http.StatusConflict, "managed_credential", "This device credential is managed by the node")
		return
	}
	if err := s.auditWithExecutor(r.Context(), tx, actorFromContext(r), "node_credential.assigned", "device", deviceID, map[string]any{"endpointId": input.EndpointID, "platform": platform, "protocol": protocol}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to record the credential update")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "credential_store_failed", "The node credential could not be stored")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deviceId": deviceID, "endpointId": input.EndpointID, "credentialStatus": "active"})
}

func validDeviceCredential(protocol, credential, fingerprint, flow, obfsType, obfsPassword string) bool {
	switch protocol {
	case "hysteria2":
		return credential != "" && fingerprint == "" && flow == "" && obfsType == "" && obfsPassword == ""
	case "vless_reality":
		return uuidPattern.MatchString(credential) && fingerprint != "" && obfsType == "" && obfsPassword == ""
	default:
		return false
	}
}

func sealNodeCredential(key []byte, deviceID, endpointID, version string, secret nodeCredentialSecret) ([]byte, error) {
	plain, err := json.Marshal(secret)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plain, credentialAAD(deviceID, endpointID, version))
	return append(nonce, sealed...), nil
}

func openNodeCredential(key, ciphertext []byte, deviceID, endpointID, version string) (nodeCredentialSecret, error) {
	var secret nodeCredentialSecret
	if len(key) != 32 || version != credentialKeyVersion {
		return secret, errors.New("unsupported node credential key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return secret, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return secret, err
	}
	if len(ciphertext) < gcm.NonceSize()+gcm.Overhead() {
		return secret, errors.New("invalid node credential ciphertext")
	}
	nonce, sealed := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, credentialAAD(deviceID, endpointID, version))
	if err != nil {
		return secret, err
	}
	if err := json.Unmarshal(plain, &secret); err != nil {
		return secret, err
	}
	return secret, nil
}

func credentialAAD(deviceID, endpointID, version string) []byte {
	return []byte(fmt.Sprintf("26x/node-credential/%s/%s/%s", version, deviceID, endpointID))
}
