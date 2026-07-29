package audit

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrNotFound = errors.New("audit: invocation not found")

const insertTimeout = 5 * time.Second

type invocationSink interface {
	insert(ctx context.Context, inv *models.ToolInvocation) error
}

const insertInvocationSQL = `
	INSERT INTO tool_invocations
		(tool_id, connector_id, connector_api_id, mcp_server_id, organization_id,
		 user_id, user_email, status, http_code, duration_ms, input, output, error, created_at)
	VALUES ($1,$2,$3,NULLIF($4,0),$5,NULLIF($6,0),$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14)`

type gormSink struct {
	db *gorm.DB
}

func (g gormSink) insert(ctx context.Context, inv *models.ToolInvocation) error {
	return g.db.WithContext(ctx).Exec(insertInvocationSQL,
		inv.ToolID, inv.ConnectorID, inv.ConnectorAPIID, inv.MCPServerID, inv.OrganizationID,
		inv.UserID, inv.UserEmail, inv.Status, inv.HTTPCode, inv.DurationMs,
		inv.Input, inv.Output, inv.Error, inv.CreatedAt,
	).Error
}

type Recorder struct {
	sink   invocationSink
	logger *slog.Logger
	queue  chan *models.ToolInvocation
	done   chan struct{}

	mu     sync.RWMutex
	closed bool

	dropped atomic.Int64
}

func NewRecorder(gdb *gorm.DB, logger *slog.Logger, bufferSize int) *Recorder {
	return newRecorder(gormSink{db: gdb}, logger, bufferSize)
}

func newRecorder(sink invocationSink, logger *slog.Logger, bufferSize int) *Recorder {
	r := &Recorder{
		sink:   sink,
		logger: logger,
		queue:  make(chan *models.ToolInvocation, bufferSize),
		done:   make(chan struct{}),
	}
	go r.drain()
	return r
}

func (r *Recorder) Write(ctx context.Context, inv *models.ToolInvocation) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		r.dropped.Add(1)
		r.logger.WarnContext(ctx, "audit: recorder closed, dropping invocation",
			slog.Int64("tool_id", inv.ToolID))
		return
	}

	select {
	case r.queue <- inv:
	default:

		r.dropped.Add(1)
		r.logger.WarnContext(ctx, "audit: queue full, dropping invocation",
			slog.Int64("tool_id", inv.ToolID),
			slog.Int("queue_capacity", cap(r.queue)))
	}
}

func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

func (r *Recorder) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.queue)
	r.mu.Unlock()

	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Recorder) drain() {
	defer close(r.done)
	for inv := range r.queue {

		ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), insertTimeout)
		if err := r.sink.insert(ctx, inv); err != nil {
			r.logger.Error("audit: insert failed",
				slog.Any("error", err),
				slog.Int64("tool_id", inv.ToolID))
		}
		cancel()
	}
}
