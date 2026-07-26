package audit

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

type Logger struct {
	db    *gorm.DB
	queue chan *models.ToolInvocation
}

func NewLogger(gdb *gorm.DB, bufferSize int) *Logger {
	l := &Logger{db: gdb, queue: make(chan *models.ToolInvocation, bufferSize)}
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
	return l.db.WithContext(ctx).Exec(`
		INSERT INTO tool_invocations
			(tool_id, connector_id, connector_api_id, mcp_server_id, organization_id,
			 user_id, user_email, status, http_code, duration_ms, input, output, error, created_at)
		VALUES ($1,$2,$3,NULLIF($4,0),$5,NULLIF($6,0),$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14)
	`,
		inv.ToolID, inv.ConnectorID, inv.ConnectorAPIID, inv.MCPServerID, inv.OrganizationID,
		inv.UserID, inv.UserEmail, inv.Status, inv.HTTPCode, inv.DurationMs, inv.Input, inv.Output, inv.Error, inv.CreatedAt,
	).Error
}

func (l *Logger) List(ctx context.Context, orgID int64, filter ListFilter) ([]*models.ToolInvocation, int, error) {
	return listInvocations(ctx, l.db, orgID, filter)
}

func (l *Logger) Get(ctx context.Context, orgID, id int64) (*models.ToolInvocation, error) {
	return getInvocation(ctx, l.db, orgID, id)
}
