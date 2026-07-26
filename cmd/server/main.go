package main

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gridhook.dev/connector-backend/internal/api"
	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/auth"
	"gridhook.dev/connector-backend/internal/config"
	"gridhook.dev/connector-backend/internal/controlplane"
	appdb "gridhook.dev/connector-backend/internal/db"
	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/engines"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/parsers"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server: fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	database, err := appdb.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()

	sealer, err := loadSealer()
	if err != nil {
		return err
	}

	sessions := identity.NewSessionService(database.DB, cfg.SessionTTL)
	authSvc := identity.NewAuthService(database.DB, sessions)
	users := identity.NewUserService(database.DB)
	orgs := controlplane.NewOrganizationService(database.DB)
	connectors := controlplane.NewConnectorService(database.DB)
	apis := controlplane.NewAPIService(database.DB, sealer)
	tools := controlplane.NewToolService(database.DB)
	groups := controlplane.NewGroupService(database.DB)
	servers := controlplane.NewMCPServerService(database.DB, cfg.MCPPublicBaseURL)

	auditLogger := audit.NewLogger(database.DB, 4096)
	broker := auth.NewBroker(apis, auth.NewInMemoryTokenCache())
	engineRegistry := engines.NewRegistry()
	dispatch := dispatcher.New(tools, broker, engineRegistry, auditLogger)

	router := api.NewRouter(api.Deps{
		Sessions:      sessions,
		Auth:          authSvc,
		Users:         users,
		Organizations: orgs,
		Connectors:    connectors,
		APIs:          apis,
		Tools:         tools,
		Groups:        groups,
		Servers:       servers,
		Audit:         auditLogger,
		Dispatcher:    dispatch,
		Parsers:       parsers.NewRegistry(),
		InternalToken: os.Getenv("INTERNAL_TOKEN"),
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server: listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("server: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func loadSealer() (appdb.Sealer, error) {
	raw := os.Getenv("KMS_DATA_KEY")
	if raw == "" {
		slog.Warn("server: KMS_DATA_KEY not set, using an insecure development-only key")
		devKey := sha256.Sum256([]byte("gridhook-dev-only-key-do-not-use-in-prod"))
		return appdb.NewAESSealer(devKey[:])
	}
	key := sha256.Sum256([]byte(raw))
	return appdb.NewAESSealer(key[:])
}
