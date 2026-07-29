package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.AllowPrivateNetworks = true
	cfg.RetryBaseDelay = time.Millisecond
	cfg.RetryMaxDelay = 5 * time.Millisecond
	return cfg
}

func newClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	cfg := testConfig()
	cfg.MaxResponseBytes = 0
	if _, err := New(cfg); err == nil {
		t.Error("New accepted MaxResponseBytes=0")
	}

	cfg = testConfig()
	cfg.MaxRetries = -1
	if _, err := New(cfg); err == nil {
		t.Error("New accepted a negative MaxRetries")
	}
}

func TestClient_Do_ReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := newClient(t, testConfig()).Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", resp.StatusCode)
	}
	if got := string(resp.Body); got != `{"ok":true}` {
		t.Errorf("Body = %q", got)
	}
	if got := resp.Header.Get("X-Custom"); got != "value" {
		t.Errorf("Header X-Custom = %q", got)
	}
}

func TestClient_Do_RetriesRetryableStatus(t *testing.T) {
	for _, status := range []int{
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.WriteHeader(status)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			resp, err := newClient(t, testConfig()).Do(t.Context(), req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want 200 after retry", resp.StatusCode)
			}
			if got := attempts.Load(); got != 2 {
				t.Errorf("attempts = %d, want 2", got)
			}
		})
	}
}

func TestClient_Do_DoesNotRetryClientErrors(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := newClient(t, testConfig()).Do(t.Context(), req); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (400 must not be retried)", got)
	}
}

func TestClient_Do_DoesNotRetryNonIdempotentMethods(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	req, err := NewRequest(t.Context(), http.MethodPost, server.URL, []byte(`{"amount":100}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := newClient(t, testConfig()).Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want the 503 surfaced to the caller", resp.StatusCode)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 — a POST must never be replayed", got)
	}
}

func TestClient_Do_ReplaysBodyOnRetry(t *testing.T) {
	var attempts atomic.Int32
	bodies := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies <- string(raw)
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := NewRequest(t.Context(), http.MethodPut, server.URL, []byte(`{"name":"x"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := newClient(t, testConfig()).Do(t.Context(), req); err != nil {
		t.Fatalf("Do: %v", err)
	}
	close(bodies)

	var seen int
	for body := range bodies {
		seen++
		if body != `{"name":"x"}` {
			t.Errorf("attempt %d body = %q, want the original payload", seen, body)
		}
	}
	if seen != 2 {
		t.Errorf("attempts = %d, want 2", seen)
	}
}

func TestClient_Do_HonoursMaxRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.MaxRetries = 3
	req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := newClient(t, cfg).Do(t.Context(), req); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := attempts.Load(); got != 4 {
		t.Errorf("attempts = %d, want 4 (1 initial + 3 retries)", got)
	}
}

func TestClient_Do_EnforcesResponseSizeCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.MaxResponseBytes = 1024

	req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, err = newClient(t, cfg).Do(t.Context(), req)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("Do = %v, want ErrResponseTooLarge", err)
	}
}

func TestClient_Do_AllowsResponseAtExactLimit(t *testing.T) {
	const size = 1024
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", size)))
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.MaxResponseBytes = size

	req, err := NewRequest(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := newClient(t, cfg).Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(resp.Body) != size {
		t.Errorf("len(Body) = %d, want %d", len(resp.Body), size)
	}
}

func TestClient_Do_RespectsContextCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	req, err := NewRequest(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := newClient(t, testConfig()).Do(ctx, req); err == nil {
		t.Error("Do returned nil, want a deadline error")
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 0},
		{"seconds", "5", 5 * time.Second},
		{"zero", "0", 0},
		{"negative is ignored", "-3", 0},
		{"garbage is ignored", "soon", 0},
		{"past date is ignored", "Mon, 02 Jan 2006 15:04:05 GMT", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRetryAfter(tc.value); got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestClient_Backoff_StaysWithinBounds(t *testing.T) {
	cfg := testConfig()
	cfg.RetryBaseDelay = 100 * time.Millisecond
	cfg.RetryMaxDelay = time.Second
	client := newClient(t, cfg)

	for attempt := range 40 {
		for range 20 {
			got := client.backoff(attempt, 0)
			if got < 0 {
				t.Fatalf("attempt %d: backoff = %v, want non-negative", attempt, got)
			}
			if got > cfg.RetryMaxDelay {
				t.Fatalf("attempt %d: backoff = %v, exceeds RetryMaxDelay %v", attempt, got, cfg.RetryMaxDelay)
			}
		}
	}
}

func TestClient_Backoff_HandlesShiftOverflow(t *testing.T) {
	client := newClient(t, testConfig())
	if got := client.backoff(62, 0); got < 0 {
		t.Errorf("backoff(62) = %v, want non-negative", got)
	}
}

func TestClient_Backoff_PrefersRetryAfter(t *testing.T) {
	cfg := testConfig()
	cfg.RetryMaxDelay = time.Second
	client := newClient(t, cfg)

	if got := client.backoff(0, 200*time.Millisecond); got != 200*time.Millisecond {
		t.Errorf("backoff with Retry-After = %v, want 200ms", got)
	}

	if got := client.backoff(0, time.Hour); got != cfg.RetryMaxDelay {
		t.Errorf("backoff with an excessive Retry-After = %v, want the clamp %v", got, cfg.RetryMaxDelay)
	}
}

func TestCanRetry(t *testing.T) {
	cases := []struct {
		method string
		body   []byte
		want   bool
	}{
		{http.MethodGet, nil, true},
		{http.MethodHead, nil, true},
		{http.MethodOptions, nil, true},
		{http.MethodPut, []byte(`{}`), true},
		{http.MethodDelete, nil, true},
		{http.MethodPost, nil, false},
		{http.MethodPost, []byte(`{}`), false},
		{http.MethodPatch, []byte(`{}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			req, err := NewRequest(t.Context(), tc.method, "https://example.com", tc.body)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if got := canRetry(req); got != tc.want {
				t.Errorf("canRetry(%s) = %v, want %v", tc.method, got, tc.want)
			}
		})
	}
}

func TestRetryableTransportError(t *testing.T) {
	if retryableTransportError(context.Canceled) {
		t.Error("a cancelled context must not be retried")
	}
	if retryableTransportError(context.DeadlineExceeded) {
		t.Error("an exceeded deadline must not be retried")
	}
	if retryableTransportError(ErrBlockedAddress) {
		t.Error("a blocked address must not be retried — it will never succeed")
	}
}
