package main

import (
	"context"
	"errors"
	"fmt"
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
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/idcodec"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/observability"
	"gridhook.dev/connector-backend/internal/parsers"
	"gridhook.dev/connector-backend/internal/secrets"
)

const auditBufferSize = 4096

func main() {
	if err := run(); err != nil {

		slog.Error("server: fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := observability.NewLogger(cfg.Observability)
	if err != nil {
		return err
	}
	logger = logger.With(slog.String("service", "server"), slog.String("env", string(cfg.Env)))

	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := appdb.Connect(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("server: closing database", slog.Any("error", err))
		}
	}()

	sealer, err := newSealer(cfg, logger)
	if err != nil {
		return err
	}

	upstream, err := httpx.New(upstreamConfig(cfg.Upstream))
	if err != nil {
		return err
	}

	publicIDs, err := newIDCodec(cfg, logger)
	if err != nil {
		return err
	}

	sessions := identity.NewSessionService(database.DB, cfg.Session.TTL)
	authSvc := identity.NewAuthService(database.DB, sessions)
	users := identity.NewUserService(database.DB)
	orgs := controlplane.NewOrganizationService(database.DB)
	connectors := controlplane.NewConnectorService(database.DB)
	marketplace := controlplane.NewMarketplaceService(database.DB)
	apis := controlplane.NewAPIService(database.DB, sealer)
	tools := controlplane.NewToolService(database.DB)
	groups := controlplane.NewGroupService(database.DB)
	servers := controlplane.NewMCPServerService(database.DB, cfg.MCP.PublicBaseURL)

	auditRecorder := audit.NewRecorder(database.DB, logger, auditBufferSize)
	auditReader := audit.NewReader(database.DB)
	broker := auth.NewBroker(apis, auth.NewInMemoryTokenCache(), upstream)
	dispatch := dispatcher.New(tools, broker, engines.NewRegistry(upstream), auditRecorder)

	router := api.NewRouter(api.Deps{
		Logger:          logger,
		MaxRequestBytes: cfg.HTTP.MaxRequestBytes,
		InternalToken:   cfg.Security.InternalToken,
		AllowedOrigins:  cfg.HTTP.AllowedOrigins,

		Sessions:      sessions,
		Auth:          authSvc,
		Users:         users,
		Organizations: orgs,
		Connectors:    connectors,
		Marketplace:   marketplace,
		APIs:          apis,
		Tools:         tools,
		Groups:        groups,
		Servers:       servers,

		AuditReader:   auditReader,
		AuditRecorder: auditRecorder,
		Dispatcher:    dispatch,
		Parsers:       parsers.NewRegistry(),
		Upstream:      upstream,
		IDCodec:       publicIDs,
		Ready:         database.Ping,
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server: listening", slog.String("addr", cfg.HTTP.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("server: shutdown signal received")
	}

	return shutdown(httpServer, auditRecorder, logger, cfg.HTTP.ShutdownTimeout)
}

func shutdown(server *http.Server, recorder *audit.Recorder, logger *slog.Logger, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), timeout)
	defer cancel()

	var shutdownErr error
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server: graceful shutdown failed", slog.Any("error", err))
		shutdownErr = err
	}
	if err := recorder.Close(shutdownCtx); err != nil {
		logger.Error("server: audit recorder did not drain", slog.Any("error", err))
		if shutdownErr == nil {
			shutdownErr = err
		}
	}

	logger.Info("server: stopped")
	return shutdownErr
}

func upstreamConfig(cfg config.Upstream) httpx.Config {
	out := httpx.DefaultConfig()
	out.Timeout = cfg.Timeout
	out.MaxResponseBytes = cfg.MaxResponseBytes
	out.MaxRetries = cfg.MaxRetries
	out.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
	out.AllowPrivateNetworks = cfg.AllowPrivateNetworks
	return out
}

func newSealer(cfg config.Config, logger *slog.Logger) (secrets.Sealer, error) {
	if cfg.Security.DataKey != "" {
		if cfg.Security.KMSKeyID != "" {
			logger.Info("server: credential sealer initialised", slog.String("kms_key_id", cfg.Security.KMSKeyID))
		}
		return secrets.NewAESSealer(secrets.DeriveKey(cfg.Security.DataKey))
	}

	if cfg.IsProduction() {
		return nil, fmt.Errorf("server: KMS_DATA_KEY is required in production")
	}
	logger.Warn("server: KMS_DATA_KEY not set — using an insecure development-only key; " +
		"credentials sealed with it are readable by anyone with the source")
	return secrets.NewAESSealer(secrets.DeriveKey("gridhook-dev-only-key-do-not-use-in-prod"))
}

func newIDCodec(cfg config.Config, logger *slog.Logger) (*idcodec.Codec, error) {
	if cfg.Security.PublicIDKey != "" {
		return idcodec.New(cfg.Security.PublicIDKey)
	}
	if cfg.IsProduction() {
		return nil, fmt.Errorf("server: PUBLIC_ID_KEY is required in production")
	}
	logger.Warn("server: PUBLIC_ID_KEY not set — using a development-only key; " +
		"public identifiers are forgeable by anyone with the source")
	return idcodec.New("gridhook-dev-only-public-id-key")
}
