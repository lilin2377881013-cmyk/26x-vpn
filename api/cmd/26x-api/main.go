package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/argon2"
)

type config struct {
	addr           string
	databaseURL    string
	migrationDir   string
	cookieSecure   bool
	trustedOrigins map[string]bool
	trustedProxies []*net.IPNet
	bootstrapEmail string
	bootstrapPass  string
	credentialKey  []byte
	managedVLESS   *managedVLESS
	nodeCommand    string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := loadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", cfg.databaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	if err := applyMigrations(ctx, db, cfg.migrationDir); err != nil {
		logger.Error("apply database migration", "error", err)
		os.Exit(1)
	}
	if err := bootstrapOwner(ctx, db, cfg.bootstrapEmail, cfg.bootstrapPass); err != nil {
		logger.Error("bootstrap owner", "error", err)
		os.Exit(1)
	}

	app := newServer(db, cfg, logger)
	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("control plane listening", "addr", cfg.addr)
		serverErr <- httpServer.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if cfg.managedVLESS != nil {
		go app.syncManagedCredentials(signalCtx)
	}
	select {
	case <-signalCtx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown", "error", err)
		}
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped", "error", err)
			os.Exit(1)
		}
	}
}

func loadConfig() (config, error) {
	cfg := config{
		addr:           env("HTTP_ADDR", "127.0.0.1:8080"),
		databaseURL:    strings.TrimSpace(os.Getenv("DATABASE_URL")),
		migrationDir:   env("MIGRATION_DIR", "./migrations"),
		trustedOrigins: make(map[string]bool),
		bootstrapEmail: strings.TrimSpace(strings.ToLower(os.Getenv("INITIAL_ADMIN_EMAIL"))),
		bootstrapPass:  os.Getenv("INITIAL_ADMIN_PASSWORD"),
		nodeCommand:    env("NODE_COMMAND", "/opt/26x-node/bin/26x-node"),
	}
	if !filepath.IsAbs(cfg.nodeCommand) {
		return cfg, errors.New("NODE_COMMAND must be an absolute path")
	}
	if cfg.databaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	var err error
	cfg.cookieSecure, err = strconv.ParseBool(env("COOKIE_SECURE", "true"))
	if err != nil {
		return cfg, errors.New("COOKIE_SECURE must be true or false")
	}
	cfg.trustedProxies, err = parseCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return cfg, fmt.Errorf("TRUSTED_PROXY_CIDRS: %w", err)
	}
	for _, origin := range strings.Split(os.Getenv("TRUSTED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			cfg.trustedOrigins[origin] = true
		}
	}
	if cfg.bootstrapEmail != "" && len(cfg.bootstrapPass) < 16 {
		return cfg, errors.New("INITIAL_ADMIN_PASSWORD must be at least 16 characters")
	}
	if encoded := strings.TrimSpace(os.Getenv("CREDENTIAL_ENCRYPTION_KEY")); encoded != "" {
		cfg.credentialKey, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(cfg.credentialKey) != 32 {
			return cfg, errors.New("CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
		}
	}
	if endpoint := strings.ToLower(strings.TrimSpace(os.Getenv("MANAGED_VLESS_ENDPOINT"))); endpoint != "" {
		socket := os.Getenv("MANAGED_VLESS_SOCKET")
		tag := os.Getenv("MANAGED_VLESS_TAG")
		if len(cfg.credentialKey) != 32 || !uuidPattern.MatchString(endpoint) ||
			!filepath.IsAbs(socket) || !validInboundTag.MatchString(tag) {
			return cfg, errors.New("managed VLESS requires an endpoint UUID, an absolute Unix socket path, an inbound tag and a credential key")
		}
		cfg.managedVLESS = &managedVLESS{
			endpoint: endpoint, socket: socket, tag: tag,
			helper: env("MANAGED_VLESS_HELPER", "/opt/26x/api/device-user.py"),
			xray:   env("MANAGED_VLESS_XRAY", "/usr/local/bin/xray"),
		}
		cfg.managedVLESS.requireLease, err = strconv.ParseBool(env("MANAGED_VLESS_REQUIRE_LEASE", "false"))
		if err != nil {
			return cfg, errors.New("MANAGED_VLESS_REQUIRE_LEASE must be true or false")
		}
	}
	return cfg, nil
}

func applyMigrations(ctx context.Context, db *sql.DB, migrationDir string) error {
	paths, err := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no SQL migrations found in %s", migrationDir)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(2600001)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, path := range paths {
		version := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		var applied bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		statement, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, part := range strings.Split(string(statement), ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, err = tx.ExecContext(ctx, part); err != nil {
				return fmt.Errorf("apply migration %s: %w", version, err)
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func bootstrapOwner(ctx context.Context, db *sql.DB, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM admin_accounts").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO admin_accounts(email, password_hash, role, mfa_required)
		VALUES ($1, $2, 'owner', FALSE) ON CONFLICT (email) DO NOTHING`, email, hash)
	return err
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$19$65536$3$2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derived)), nil
}
