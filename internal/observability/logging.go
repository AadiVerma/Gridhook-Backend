package observability

import (
	"context"
	"log/slog"
	"os"

	"gridhook.dev/connector-backend/internal/config"
)

func NewLogger(cfg config.Observability) (*slog.Logger, error) {
	level, err := config.ParseLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	useColor, err := config.ResolveLogColor(cfg.LogColor, isTerminal(os.Stdout))
	if err != nil {
		return nil, err
	}

	opts := slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.LogFormat == config.LogFormatJSON {
		handler = slog.NewJSONHandler(os.Stdout, &opts)
	} else {
		handler = newPrettyHandler(os.Stdout, opts, useColor)
	}
	return slog.New(&correlationHandler{Handler: handler}), nil
}

type correlationHandler struct {
	slog.Handler
}

func (h *correlationHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		record.AddAttrs(slog.String("request_id", id))
	}
	if orgID := OrgIDFromContext(ctx); orgID != 0 {
		record.AddAttrs(slog.Int64("organization_id", orgID))
	}
	return h.Handler.Handle(ctx, record)
}

func (h *correlationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &correlationHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *correlationHandler) WithGroup(name string) slog.Handler {
	return &correlationHandler{Handler: h.Handler.WithGroup(name)}
}
