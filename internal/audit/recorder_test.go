package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"gridhook.dev/connector-backend/internal/models"
)

type recordingSink struct {
	mu       sync.Mutex
	received []*models.ToolInvocation
	block    chan struct{}
	err      error
}

func (s *recordingSink) insert(_ context.Context, inv *models.ToolInvocation) error {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, inv)
	return s.err
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.received)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func invocation(toolID int64) *models.ToolInvocation {
	return &models.ToolInvocation{ToolID: toolID, OrganizationID: 1, CreatedAt: time.Now()}
}

func TestRecorder_WritesAreDrained(t *testing.T) {
	sink := &recordingSink{}
	recorder := newRecorder(sink, discardLogger(), 16)

	for i := range int64(5) {
		recorder.Write(t.Context(), invocation(i))
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := sink.count(); got != 5 {
		t.Errorf("persisted %d invocations, want 5", got)
	}
}

func TestRecorder_CloseFlushesBufferedRecords(t *testing.T) {
	sink := &recordingSink{block: make(chan struct{})}
	recorder := newRecorder(sink, discardLogger(), 64)

	for i := range int64(20) {
		recorder.Write(t.Context(), invocation(i))
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(sink.block)
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := sink.count(); got != 20 {
		t.Errorf("persisted %d invocations, want all 20 flushed by Close", got)
	}
}

func TestRecorder_CloseHonoursDeadline(t *testing.T) {
	sink := &recordingSink{block: make(chan struct{})}
	defer close(sink.block)

	recorder := newRecorder(sink, discardLogger(), 4)
	recorder.Write(t.Context(), invocation(1))
	recorder.Write(t.Context(), invocation(2))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	if err := recorder.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Close = %v, want DeadlineExceeded", err)
	}
}

func TestRecorder_CloseIsIdempotent(t *testing.T) {
	recorder := newRecorder(&recordingSink{}, discardLogger(), 4)

	if err := recorder.Close(t.Context()); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := recorder.Close(t.Context()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRecorder_WriteAfterCloseDoesNotPanic(t *testing.T) {
	recorder := newRecorder(&recordingSink{}, discardLogger(), 4)
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recorder.Write(t.Context(), invocation(1))

	if got := recorder.Dropped(); got != 1 {
		t.Errorf("Dropped = %d, want 1", got)
	}
}

func TestRecorder_WriteNeverBlocksWhenFull(t *testing.T) {
	sink := &recordingSink{block: make(chan struct{})}
	defer close(sink.block)

	recorder := newRecorder(sink, discardLogger(), 2)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range int64(100) {
			recorder.Write(context.Background(), invocation(i))
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked when the buffer was full")
	}

	if recorder.Dropped() == 0 {
		t.Error("Dropped = 0; overflow must be counted, not hidden")
	}
}

func TestRecorder_SurvivesInsertFailures(t *testing.T) {
	sink := &recordingSink{err: errors.New("connection reset")}
	recorder := newRecorder(sink, discardLogger(), 16)

	for i := range int64(5) {
		recorder.Write(t.Context(), invocation(i))
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := sink.count(); got != 5 {
		t.Errorf("sink saw %d invocations, want 5 — the drain must continue past a failure", got)
	}
}

func TestRecorder_ConcurrentWrites(t *testing.T) {
	sink := &recordingSink{}
	recorder := newRecorder(sink, discardLogger(), 1024)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 50 {
				recorder.Write(context.Background(), invocation(int64(i*50+j)))
			}
		}()
	}
	wg.Wait()

	if err := recorder.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if int64(sink.count())+recorder.Dropped() != 800 {
		t.Errorf("persisted %d + dropped %d != 800", sink.count(), recorder.Dropped())
	}
}
