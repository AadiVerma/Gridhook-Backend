package dispatcher

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"gridhook.dev/connector-backend/internal/engines"
	"gridhook.dev/connector-backend/internal/models"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

func TestNormalize_ClassifiesTimeouts(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want models.InvocationStatus
	}{
		{"deadline exceeded", context.DeadlineExceeded, models.InvocationTimeout},
		{"wrapped deadline", errors.Join(errors.New("engines: rest"), context.DeadlineExceeded), models.InvocationTimeout},
		{"net timeout", timeoutError{}, models.InvocationTimeout},
		{"wrapped net timeout", &url.Error{Op: "Get", URL: "https://x", Err: timeoutError{}}, models.InvocationTimeout},
		{"connection refused", errors.New("connection refused"), models.InvocationError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize(nil, tc.err, time.Second)
			if got.Status != tc.want {
				t.Errorf("Status = %q, want %q", got.Status, tc.want)
			}
		})
	}
}

func TestNormalize_RedactsCredentialsFromErrors(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://api.example.com/v1/orders?api_key=live_LEAKME&page=1",
		Err: errors.New("connection refused"),
	}

	got := normalize(nil, err, time.Second)
	if strings.Contains(got.Error, "live_LEAKME") {
		t.Errorf("Outcome.Error leaked the API key, and would have persisted it: %s", got.Error)
	}
	if !strings.Contains(got.Error, "connection refused") {
		t.Errorf("Outcome.Error dropped the useful cause: %s", got.Error)
	}
}

func TestNormalize_Success(t *testing.T) {
	result := &engines.Result{StatusCode: 200, Body: map[string]any{"id": 1.0}}

	got := normalize(result, nil, 250*time.Millisecond)
	if got.Status != models.InvocationSuccess {
		t.Errorf("Status = %q, want success", got.Status)
	}
	if got.HTTPCode != 200 {
		t.Errorf("HTTPCode = %d, want 200", got.HTTPCode)
	}
	if got.DurationMs != 250 {
		t.Errorf("DurationMs = %d, want 250", got.DurationMs)
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty on success", got.Error)
	}
}

func TestNormalize_HTTPErrorCarriesAnError(t *testing.T) {
	got := normalize(&engines.Result{StatusCode: 503}, nil, time.Second)

	if got.Status != models.InvocationError {
		t.Errorf("Status = %q, want error for a 503", got.Status)
	}
	if got.Error == "" {
		t.Error("Error is empty for a 503; an HTTP failure must be greppable in the audit log")
	}
	if got.HTTPCode != 503 {
		t.Errorf("HTTPCode = %d, want 503", got.HTTPCode)
	}
}

func TestNormalize_HandlesNilResult(t *testing.T) {
	got := normalize(nil, nil, time.Second)
	if got.Status != models.InvocationError {
		t.Errorf("Status = %q, want error", got.Status)
	}
	if got.Error == "" {
		t.Error("Error is empty; a nil result must be explained")
	}
}

func TestAsMap(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want map[string]any
	}{
		{"nil", nil, map[string]any{}},
		{"map passes through", map[string]any{"a": 1}, map[string]any{"a": 1}},
		{"scalar is wrapped", "text", map[string]any{"value": "text"}},
		{"slice is wrapped", []any{1, 2}, map[string]any{"value": []any{1, 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := asMap(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("asMap(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for k := range tc.want {
				if _, ok := got[k]; !ok {
					t.Errorf("asMap(%v) missing key %q", tc.in, k)
				}
			}
		})
	}
}
