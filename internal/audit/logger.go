// Package audit writes the append-only tool_invocations log. Writes never
// block the caller's response — Write enqueues onto a buffered channel and a
// background goroutine drains it, so a slow/contended audit insert can never
// add latency to a live tool call.
package audit

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

type Logger struct {
	pool  *pgxpool.Pool
	queue chan *models.ToolInvocation
}

// NewLogger starts the drain goroutine. bufferSize bounds how many
// invocations can be queued before Write starts dropping the oldest —
// audit-log durability under overload is a secondary concern to keeping the
// live dispatch path fast.
func NewLogger(pool *pgxpool.Pool, bufferSize int) *Logger {
	l := &Logger{pool: pool, queue: make(chan *models.ToolInvocation, bufferSize)}
	go l.drain()
	return l
}

func (l *Logger) Write(ctx context.Context, inv *models.ToolInvocation) {
	select {
	case l.queue <- inv:
	default:
		slog.Warn("audit: queue full, dropping invocation", "tool_id", inv.ToolID)
	}
}

func (l *Logger) drain() {
	for inv := range l.queue {
		if err := l.insert(context.Background(), inv); err != nil {
			slog.Error("audit: insert failed", "error", err, "tool_id", inv.ToolID)
		}
	}
}

func (l *Logger) insert(ctx context.Context, inv *models.ToolInvocation) error {
	_, err := l.pool.Exec(ctx, `
		INSERT INTO tool_invocations
			(tool_id, connector_id, connector_api_id, mcp_server_id, organization_id,
			 user_id, user_email, status, http_code, duration_ms, input, output, error, created_at)
		VALUES ($1,$2,$3,NULLIF($4,0),$5,NULLIF($6,0),$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14)
	`,
		inv.ToolID, inv.ConnectorID, inv.ConnectorAPIID, inv.MCPServerID, inv.OrganizationID,
		inv.UserID, inv.UserEmail, inv.Status, inv.HTTPCode, inv.DurationMs, inv.Input, inv.Output, inv.Error, inv.CreatedAt,
	)
	return err
}

// List and Get back the admin API's /audit-logs endpoints. They read
// directly — no cache — since audit is inherently a write-once/read-rarely
// path and correctness (seeing the very latest invocation) matters more
// than shaving a query.
func (l *Logger) List(ctx context.Context, orgID int64, filter ListFilter) ([]*models.ToolInvocation, int, error) {
	return listInvocations(ctx, l.pool, orgID, filter)
}

func (l *Logger) Get(ctx context.Context, orgID, id int64) (*models.ToolInvocation, error) {
	return getInvocation(ctx, l.pool, orgID, id)
}
