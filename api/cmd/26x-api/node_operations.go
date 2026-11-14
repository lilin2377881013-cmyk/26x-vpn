package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var nodeCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)
var sshUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var nodeHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)

type nodeChange struct {
	Name     string `json:"name"`
	Region   string `json:"region_code"`
	Capacity *int   `json:"capacity_limit"`
}

func (input *nodeChange) valid() bool {
	input.Name = strings.TrimSpace(input.Name)
	input.Region = strings.ToUpper(strings.TrimSpace(input.Region))
	switch input.Region {
	case "HK", "JP", "SG", "US":
		return len(input.Name) > 0 && len(input.Name) <= 80 && (input.Capacity == nil || *input.Capacity > 0)
	}
	return false
}

func (s *server) nodeCommand(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, s.cfg.nodeCommand, args...).Output()
}

func (s *server) deployNode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		nodeChange
		Code    string `json:"code"`
		Host    string `json:"host"`
		Port    int    `json:"ssh_port"`
		User    string `json:"ssh_user"`
		Pass    string `json:"ssh_password"`
		VPNPort int    `json:"port"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node", "Invalid deployment request")
		return
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Host = strings.TrimSpace(input.Host)
	if input.Port == 0 {
		input.Port = 22
	}
	if input.VPNPort == 0 {
		input.VPNPort = 8443
	}
	if input.User == "" {
		input.User = "root"
	}
	if !input.valid() || !nodeCodePattern.MatchString(input.Code) || input.Code == "new" ||
		(net.ParseIP(input.Host) == nil && !nodeHostnamePattern.MatchString(input.Host)) ||
		input.Port < 1 || input.Port > 65535 || input.VPNPort < 1 || input.VPNPort > 65535 || !sshUserPattern.MatchString(input.User) ||
		len(input.Pass) == 0 || len(input.Pass) > 1024 || strings.ContainsRune(input.Pass, 0) {
		writeError(w, http.StatusBadRequest, "invalid_node", "Node or SSH fields are invalid")
		return
	}
	ctx, cancel := nodeOperationContext(w, r, 180*time.Second)
	defer cancel()
	lock, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "node_lock_failed", "Unable to reserve deployment")
		return
	}
	defer lock.Rollback()
	var acquired bool
	if err := lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(26008443)`).Scan(&acquired); err != nil {
		writeError(w, http.StatusServiceUnavailable, "node_lock_failed", "Unable to reserve deployment")
		return
	}
	if !acquired {
		writeError(w, http.StatusConflict, "deployment_in_progress", "Another node is being deployed")
		return
	}
	var exists bool
	if err := lock.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE code=$1)`, input.Code).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to check node code")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "node_exists", "A node with this code already exists")
		return
	}
	args := []string{"add", "--code", input.Code, "--name", input.Name, "--region", input.Region, "--host", input.Host}
	if input.Port != 22 {
		args = append(args, "--ssh-port", strconv.Itoa(input.Port))
	}
	if input.User != "root" {
		args = append(args, "--ssh-user", input.User)
	}
	if input.VPNPort != 8443 {
		args = append(args, "--port", strconv.Itoa(input.VPNPort))
	}
	args = append(args, "--ssh-pass", input.Pass)
	if input.Capacity != nil {
		args = append(args, "--capacity", strconv.Itoa(*input.Capacity))
	}
	args = append(args, "--json")
	_, ok := s.runNodeJSON(w, ctx, "add", args)
	if !ok {
		return
	}
	if err := lock.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "node_lock_failed", "Unable to finish deployment")
		return
	}
	nodes, err := s.readNodes(ctx, input.Code)
	if err != nil || len(nodes) != 1 {
		writeError(w, http.StatusBadGateway, "node_not_registered", "Deployment command did not register the node")
		return
	}
	s.recordNodeOperation(r, "node.deployed", nodes[0]["id"].(string), input.Code)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "node": nodes[0]})
}

func nodeOperationContext(w http.ResponseWriter, r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + 10*time.Second))
	return context.WithTimeout(r.Context(), timeout)
}

func (s *server) runNodeJSON(w http.ResponseWriter, ctx context.Context, operation string, args []string) (map[string]json.RawMessage, bool) {
	raw, err := s.nodeCommand(ctx, args...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "node_command_timeout", "Node command timed out")
		return nil, false
	}
	var data map[string]json.RawMessage
	var success bool
	parsed := json.Unmarshal(raw, &data) == nil && json.Unmarshal(data["ok"], &success) == nil
	if !parsed || !success || err != nil {
		// Script stderr and error details may contain credentials.
		writeError(w, http.StatusBadGateway, "node_command_failed", "Node command failed: "+operation)
		return nil, false
	}
	return data, true
}

func (s *server) scriptNodeList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := nodeOperationContext(w, r, 30*time.Second)
	defer cancel()
	data, ok := s.runNodeJSON(w, ctx, "list", []string{"list", "--json"})
	if ok {
		writeJSON(w, http.StatusOK, data)
	}
}

func (s *server) scriptNodeAction(operation string, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		if !nodeCodePattern.MatchString(code) {
			writeError(w, http.StatusBadRequest, "invalid_node", "Invalid node code")
			return
		}
		ctx, cancel := nodeOperationContext(w, r, timeout)
		defer cancel()
		args := []string{operation, code, "--json"}
		if operation == "health" || operation == "traffic" {
			args = []string{operation, "--node", code, "--json"}
		}
		data, ok := s.runNodeJSON(w, ctx, operation, args)
		if ok {
			if r.Method != http.MethodGet {
				s.recordNodeOperation(r, "node."+operation, code, code)
			}
			writeJSON(w, http.StatusOK, data)
		}
	}
}

func (s *server) findNode(w http.ResponseWriter, r *http.Request) map[string]any {
	nodes, err := s.readNodes(r.Context(), r.PathValue("code"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read node")
		return nil
	}
	if len(nodes) == 0 {
		writeError(w, http.StatusNotFound, "node_not_found", "Node does not exist")
		return nil
	}
	return nodes[0]
}

func (s *server) getNode(w http.ResponseWriter, r *http.Request) {
	if node := s.findNode(w, r); node != nil {
		writeJSON(w, http.StatusOK, map[string]any{"node": node})
	}
}

func (s *server) editNode(w http.ResponseWriter, r *http.Request) {
	var input nodeChange
	if err := decodeJSON(w, r, &input); err != nil || !input.valid() {
		writeError(w, http.StatusBadRequest, "invalid_node", "Name, region or capacity is invalid")
		return
	}
	code := r.PathValue("code")
	var id string
	err := s.db.QueryRowContext(r.Context(), `UPDATE nodes SET name=$2, region_code=$3, capacity_limit=$4
		WHERE code=$1 RETURNING id::text`, code, input.Name, input.Region, input.Capacity).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "node_not_found", "Node does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to update node")
		return
	}
	s.recordNodeOperation(r, "node.updated", id, code)
	if node := s.findNode(w, r); node != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": node})
	}
}

func (s *server) removeNode(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !nodeCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, "invalid_node", "Invalid node code")
		return
	}
	node := s.findNode(w, r)
	if node == nil {
		return
	}
	ctx, cancel := nodeOperationContext(w, r, 60*time.Second)
	defer cancel()
	data, ok := s.runNodeJSON(w, ctx, "remove", []string{"remove", code, "--json"})
	if ok {
		s.recordNodeOperation(r, "node.removed", node["id"].(string), code)
		writeJSON(w, http.StatusOK, data)
	}
}

func (s *server) recordNodeOperation(r *http.Request, action, id, code string) {
	if err := s.auditWithExecutor(r.Context(), s.db, actorFromContext(r), action, "node", id, map[string]any{"code": code}); err != nil {
		s.logger.Error("record node operation audit", "action", action, "node_id", id)
	}
}

type dailyUsage struct {
	Day       string `json:"day"`
	BytesUp   int64  `json:"bytes_up"`
	BytesDown int64  `json:"bytes_down"`
}

func readDailyUsage(ctx context.Context, db *sql.DB, query, id string, days int) ([]dailyUsage, error) {
	rows, err := db.QueryContext(ctx, query, id, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]dailyUsage, 0)
	for rows.Next() {
		var item dailyUsage
		if err := rows.Scan(&item.Day, &item.BytesUp, &item.BytesDown); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *server) nodeStats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !nodeCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, "invalid_node", "Invalid node code")
		return
	}
	node := s.findNode(w, r)
	if node == nil {
		return
	}
	usage, err := readDailyUsage(r.Context(), s.db, `SELECT to_char(usage_day,'YYYY-MM-DD'), bytes_up, bytes_down
		FROM node_usage_daily WHERE node_id=$1 AND usage_day BETWEEN CURRENT_DATE - ($2::int - 1) AND CURRENT_DATE
		ORDER BY usage_day`, node["id"].(string), 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read node usage")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	data, ok := s.runNodeJSON(w, ctx, "traffic", []string{"traffic", "--node", code, "--json"})
	if !ok {
		return
	}
	var stats struct {
		Node  string
		Users *int64
		Used  *float64
		Limit *float64
	}
	if json.Unmarshal(data["node"], &stats.Node) != nil || stats.Node != code ||
		json.Unmarshal(data["users"], &stats.Users) != nil || stats.Users == nil || *stats.Users < 0 ||
		json.Unmarshal(data["traffic_used_gb"], &stats.Used) != nil || stats.Used == nil || *stats.Used < 0 ||
		json.Unmarshal(data["traffic_limit_gb"], &stats.Limit) != nil || (stats.Limit != nil && *stats.Limit < 0) {
		writeError(w, http.StatusBadGateway, "node_stats_failed", "The traffic command did not return valid node statistics")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node": stats.Node, "users": stats.Users, "traffic_used_gb": stats.Used, "traffic_limit_gb": stats.Limit, "daily": usage,
	})
}

func (s *server) userUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid_user", "User ID must be a UUID")
		return
	}
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 90 {
			writeError(w, http.StatusBadRequest, "invalid_days", "Days must be between 1 and 90")
			return
		}
		days = parsed
	}
	var email, status string
	err := s.db.QueryRowContext(r.Context(), `SELECT email::text,status FROM users WHERE id=$1`, id).Scan(&email, &status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user_not_found", "User does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read user")
		return
	}
	items, err := readDailyUsage(r.Context(), s.db, `SELECT to_char(usage_day,'YYYY-MM-DD'), sum(bytes_up)::bigint, sum(bytes_down)::bigint
		FROM usage_daily WHERE user_id=$1 AND usage_day BETWEEN CURRENT_DATE - ($2::int - 1) AND CURRENT_DATE
		GROUP BY usage_day ORDER BY usage_day`, id, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read user usage")
		return
	}
	var up, down int64
	for _, item := range items {
		up += item.BytesUp
		down += item.BytesDown
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"id": id, "email": email, "status": status},
		"days": days, "bytes_up": up, "bytes_down": down, "daily": items})
}
