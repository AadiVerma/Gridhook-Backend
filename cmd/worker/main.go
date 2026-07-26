package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gridhook.dev/connector-backend/internal/config"
	appdb "gridhook.dev/connector-backend/internal/db"
	"gridhook.dev/connector-backend/internal/identity"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("worker: fatal", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	database, err := appdb.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("worker: fatal", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	sessions := identity.NewSessionService(database.DB, cfg.SessionTTL)

	sweepTicker := time.NewTicker(1 * time.Hour)
	defer sweepTicker.Stop()

	slog.Info("worker: started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("worker: shutting down")
			return
		case <-sweepTicker.C:
			n, err := sessions.SweepExpired(ctx, 30*24*time.Hour)
			if err != nil {
				slog.Error("worker: session sweep failed", "error", err)
				continue
			}
			if n > 0 {
				slog.Info("worker: swept expired sessions", "count", n)
			}
		}
	}
}
