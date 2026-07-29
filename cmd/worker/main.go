package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gridhook.dev/connector-backend/internal/config"
	appdb "gridhook.dev/connector-backend/internal/db"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker: fatal", slog.Any("error", err))
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
	logger = logger.With(slog.String("service", "worker"), slog.String("env", string(cfg.Env)))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := appdb.Connect(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("worker: closing database", slog.Any("error", err))
		}
	}()

	sessions := identity.NewSessionService(database.DB, cfg.Session.TTL)
	sweeper := sessionSweeper{
		sessions:  sessions,
		retention: cfg.Worker.SessionSweepRetention,
		logger:    logger,
	}

	logger.Info("worker: started",
		slog.Duration("sweep_interval", cfg.Worker.SessionSweepInterval),
		slog.Duration("sweep_retention", cfg.Worker.SessionSweepRetention))

	runPeriodically(ctx, cfg.Worker.SessionSweepInterval, sweeper.run)

	logger.Info("worker: stopped")
	return nil
}

func runPeriodically(ctx context.Context, interval time.Duration, job func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	job(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job(ctx)
		}
	}
}

const jobTimeout = 5 * time.Minute

type sessionSweeper struct {
	sessions  *identity.SessionService
	retention time.Duration
	logger    *slog.Logger
}

func (s sessionSweeper) run(ctx context.Context) {
	jobCtx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	swept, err := s.sessions.SweepExpired(jobCtx, s.retention)
	switch {
	case errors.Is(err, context.Canceled):

		return
	case err != nil:
		s.logger.Error("worker: session sweep failed", slog.Any("error", err))
		return
	}
	if swept > 0 {
		s.logger.Info("worker: swept expired sessions", slog.Int64("count", swept))
	}
}
