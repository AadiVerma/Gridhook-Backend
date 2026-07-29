package observability

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func prettyLogger(color bool) (*slog.Logger, *strings.Builder) {
	buf := &strings.Builder{}
	h := newPrettyHandler(buf, slog.HandlerOptions{Level: slog.LevelDebug}, color)
	return slog.New(&correlationHandler{Handler: h}), buf
}

func TestPrettyHandler_RendersLevelAndMessage(t *testing.T) {
	logger, buf := prettyLogger(false)
	logger.Info("server: listening", slog.String("addr", ":8080"))

	got := buf.String()
	for _, want := range []string{"INF", "server: listening", "addr=:8080"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("colour was emitted with color=false: %q", got)
	}
}

func TestPrettyHandler_EmitsColourWhenEnabled(t *testing.T) {
	logger, buf := prettyLogger(true)
	logger.Error("boom")

	got := buf.String()
	if !strings.Contains(got, ansiRed) {
		t.Errorf("ERROR was not painted red: %q", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), ansiReset) && !strings.Contains(got, ansiReset) {
		t.Errorf("colour was not reset: %q", got)
	}
}

func TestPrettyHandler_LevelLabels(t *testing.T) {
	cases := map[slog.Level]string{
		slog.LevelDebug: "DBG",
		slog.LevelInfo:  "INF",
		slog.LevelWarn:  "WRN",
		slog.LevelError: "ERR",
	}
	for level, want := range cases {
		t.Run(want, func(t *testing.T) {
			logger, buf := prettyLogger(false)
			logger.Log(context.Background(), level, "msg")
			if !strings.Contains(buf.String(), want) {
				t.Errorf("level %v rendered as %q, want %s", level, buf.String(), want)
			}
		})
	}
}

func TestPrettyHandler_RespectsLevelThreshold(t *testing.T) {
	buf := &strings.Builder{}
	logger := slog.New(newPrettyHandler(buf, slog.HandlerOptions{Level: slog.LevelWarn}, false))

	logger.Debug("hidden")
	logger.Info("hidden")
	logger.Warn("shown")

	got := buf.String()
	if strings.Contains(got, "hidden") {
		t.Errorf("records below the threshold were emitted: %s", got)
	}
	if !strings.Contains(got, "shown") {
		t.Errorf("record at the threshold was suppressed: %s", got)
	}
}

func TestPrettyHandler_FormatsDurationsCompactly(t *testing.T) {
	logger, buf := prettyLogger(false)
	logger.Info("http request", slog.Duration("duration", 2232583*time.Nanosecond))

	if !strings.Contains(buf.String(), "duration=2.23ms") {
		t.Errorf("duration = %q, want duration=2.23ms", buf.String())
	}
}

func TestPrettyHandler_QuotesAmbiguousStrings(t *testing.T) {
	logger, buf := prettyLogger(false)
	logger.Info("msg", slog.String("note", "two words"), slog.String("empty", ""))

	got := buf.String()
	if !strings.Contains(got, `note="two words"`) {
		t.Errorf("a value with a space was not quoted: %s", got)
	}
	if !strings.Contains(got, `empty=""`) {
		t.Errorf("an empty value was not made visible: %s", got)
	}
}

func TestPrettyHandler_WithAttrsDoesNotAlias(t *testing.T) {
	buf := &strings.Builder{}
	base := slog.New(newPrettyHandler(buf, slog.HandlerOptions{Level: slog.LevelDebug}, false))

	first := base.With(slog.String("branch", "one"))
	second := base.With(slog.String("branch", "two"))

	first.Info("a")
	second.Info("b")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "branch=one") || strings.Contains(lines[0], "branch=two") {
		t.Errorf("first logger's attrs were corrupted: %s", lines[0])
	}
	if !strings.Contains(lines[1], "branch=two") || strings.Contains(lines[1], "branch=one") {
		t.Errorf("second logger's attrs were corrupted: %s", lines[1])
	}
}

func TestPrettyHandler_NestsGroups(t *testing.T) {
	logger, buf := prettyLogger(false)
	logger.WithGroup("db").With(slog.String("host", "localhost")).
		Info("connected", slog.Int("pool", 25))

	got := buf.String()
	for _, want := range []string{"db.host=localhost", "db.pool=25"} {
		if !strings.Contains(got, want) {
			t.Errorf("group nesting missing %q: %s", want, got)
		}
	}
}

func TestPrettyHandler_InlineGroupAttr(t *testing.T) {
	logger, buf := prettyLogger(false)
	logger.Info("msg", slog.Group("upstream", slog.Int("status", 503), slog.String("host", "api")))

	got := buf.String()
	for _, want := range []string{"upstream.status=503", "upstream.host=api"} {
		if !strings.Contains(got, want) {
			t.Errorf("inline group missing %q: %s", want, got)
		}
	}
}

func TestPrettyHandler_CarriesCorrelationFields(t *testing.T) {
	logger, buf := prettyLogger(false)
	ctx := WithOrgID(WithRequestID(context.Background(), "req-1"), 42)

	logger.InfoContext(ctx, "msg")

	got := buf.String()
	if !strings.Contains(got, "request_id=req-1") || !strings.Contains(got, "organization_id=42") {
		t.Errorf("correlation fields missing: %s", got)
	}
}

func TestPrettyHandler_ConcurrentWritesAreWholeLines(t *testing.T) {
	buf := &strings.Builder{}
	logger := slog.New(newPrettyHandler(buf, slog.HandlerOptions{Level: slog.LevelDebug}, true))

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				logger.Info("concurrent", slog.Int("worker", i))
			}
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 800 {
		t.Fatalf("got %d lines, want 800 — writes interleaved", len(lines))
	}
	for _, line := range lines {
		if !strings.Contains(line, "concurrent") || !strings.Contains(line, "worker=") {
			t.Fatalf("torn line: %q", line)
		}
	}
}
