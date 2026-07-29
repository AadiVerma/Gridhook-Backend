package observability

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gridhook.dev/connector-backend/internal/config"
)

func jsonLogger() (*slog.Logger, *strings.Builder) {
	buf := &strings.Builder{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(&correlationHandler{Handler: handler}), buf
}

func lastRecord(t *testing.T, buf *strings.Builder) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("no log records were emitted")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &record); err != nil {
		t.Fatalf("log line was not JSON: %s", lines[len(lines)-1])
	}
	return record
}

func TestRequestID_GeneratesAndEchoes(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Error("no request ID was published on the context")
	}
	if rec.Header().Get(HeaderRequestID) != seen {
		t.Errorf("echoed header %q != context value %q", rec.Header().Get(HeaderRequestID), seen)
	}
}

func TestRequestID_HonoursInboundValue(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderRequestID, "upstream-trace-42")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if seen != "upstream-trace-42" {
		t.Errorf("request ID = %q, want the inbound value preserved for tracing", seen)
	}
}

func TestRequestID_RejectsHostileInboundValues(t *testing.T) {
	cases := map[string]string{
		"overlong":          strings.Repeat("x", 200),
		"newline injection": "abc\nlevel=ERROR msg=\"fake entry\"",
		"control chars":     "abc\x00\x07def",
	}

	for name, hostile := range cases {
		t.Run(name, func(t *testing.T) {
			var seen string
			handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = RequestIDFromContext(r.Context())
			}))

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(HeaderRequestID, hostile)
			handler.ServeHTTP(httptest.NewRecorder(), r)

			if seen == hostile {
				t.Errorf("hostile request ID %q was accepted verbatim", name)
			}
			if seen == "" {
				t.Error("no replacement request ID was generated")
			}
		})
	}
}

func TestAccessLog_RecordsRequestOutcome(t *testing.T) {
	logger, buf := jsonLogger()
	handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/connectors", nil))

	record := lastRecord(t, buf)
	if record["method"] != http.MethodPost {
		t.Errorf("method = %v", record["method"])
	}
	if record["status"] != float64(http.StatusCreated) {
		t.Errorf("status = %v, want 201", record["status"])
	}
	if record["bytes"] != float64(5) {
		t.Errorf("bytes = %v, want 5", record["bytes"])
	}
}

func TestAccessLog_LevelsBySeverity(t *testing.T) {
	cases := map[int]string{
		http.StatusOK:                  "INFO",
		http.StatusBadRequest:          "WARN",
		http.StatusInternalServerError: "ERROR",
	}

	for status, wantLevel := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			logger, buf := jsonLogger()
			handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

			if got := lastRecord(t, buf)["level"]; got != wantLevel {
				t.Errorf("level for %d = %v, want %s", status, got, wantLevel)
			}
		})
	}
}

func TestCorrelationHandler_AttachesContextFields(t *testing.T) {
	logger, buf := jsonLogger()

	ctx := WithOrgID(WithRequestID(t.Context(), "req-xyz"), 4242)
	logger.InfoContext(ctx, "something happened")

	record := lastRecord(t, buf)
	if record["request_id"] != "req-xyz" {
		t.Errorf("request_id = %v, want req-xyz", record["request_id"])
	}
	if record["organization_id"] != float64(4242) {
		t.Errorf("organization_id = %v, want 4242", record["organization_id"])
	}
}

func TestCorrelationHandler_SurvivesWith(t *testing.T) {
	logger, buf := jsonLogger()

	logger.With(slog.String("service", "server")).
		InfoContext(WithRequestID(t.Context(), "req-1"), "msg")

	record := lastRecord(t, buf)
	if record["request_id"] != "req-1" {
		t.Errorf("request_id lost after With(): %v", record)
	}
	if record["service"] != "server" {
		t.Errorf("service attr lost: %v", record)
	}
}

func TestRecoverer_ConvertsPanicTo500(t *testing.T) {
	logger, buf := jsonLogger()
	handler := Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("body = %s", rec.Body.String())
	}

	record := lastRecord(t, buf)
	if record["level"] != "ERROR" {
		t.Errorf("panic was logged at %v, want ERROR", record["level"])
	}
	if record["stack"] == nil {
		t.Error("panic log has no stack trace")
	}
}

func TestRecoverer_DoesNotLeakPanicValue(t *testing.T) {
	logger, _ := jsonLogger()
	handler := Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("dsn=postgres://user:hunter2@db.internal/gridhook")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("panic value leaked into the response: %s", rec.Body.String())
	}
}

func TestRecoverer_RepanicsOnErrAbortHandler(t *testing.T) {
	logger, _ := jsonLogger()
	handler := Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("Recoverer swallowed http.ErrAbortHandler")
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRecoverer_PassesThroughNormalRequests(t *testing.T) {
	logger, _ := jsonLogger()
	handler := Recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the handler's own 418", rec.Code)
	}
}

func TestStatusRecorder_FirstWriteHeaderWins(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}

	rec.WriteHeader(http.StatusNotFound)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.status != http.StatusNotFound {
		t.Errorf("status = %d, want the first value written", rec.status)
	}
}

func TestNewLogger_RejectsBadLevel(t *testing.T) {
	if _, err := NewLogger(config.Observability{LogLevel: "verbose"}); err == nil {
		t.Error("NewLogger accepted an invalid level")
	}
}

func TestLogger_ConcurrentUse(t *testing.T) {
	if _, err := NewLogger(config.Observability{LogLevel: "info", LogFormat: "json"}); err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	logger := slog.New(&correlationHandler{Handler: slog.NewJSONHandler(io.Discard, nil)})

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := WithOrgID(WithRequestID(t.Context(), "req"), int64(i))
			for range 50 {
				logger.InfoContext(ctx, "concurrent")
			}
		}()
	}
	wg.Wait()
}
