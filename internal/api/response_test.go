package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/observability"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHandleServiceError_StatusMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		want     int
		wantCode string
	}{
		{"not found", controlplane.ErrNotFound, http.StatusNotFound, "not_found"},
		{"audit not found", fmt.Errorf("wrapped: %w", audit.ErrNotFound), http.StatusNotFound, "not_found"},
		{"user not found", identity.ErrUserNotFound, http.StatusNotFound, "not_found"},
		{"validation", fmt.Errorf("%w: bad", controlplane.ErrValidation), http.StatusBadRequest, "validation_failed"},
		{"invalid email", identity.ErrInvalidEmail, http.StatusBadRequest, "validation_failed"},
		{"invalid role", identity.ErrInvalidRole, http.StatusBadRequest, "validation_failed"},
		{"password policy", identity.ErrPasswordTooShort, http.StatusBadRequest, "validation_failed"},
		{"incomplete credentials", schemes.ErrIncompleteCredentials, http.StatusBadRequest, "credentials_incomplete"},
		{"unauthorized", controlplane.ErrUnauthorized, http.StatusUnauthorized, "unauthorized"},
		{"email taken", identity.ErrEmailTaken, http.StatusConflict, "conflict"},
		{"already a member", identity.ErrAlreadyAMember, http.StatusConflict, "conflict"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/x", nil)

			handleServiceError(rec, r, discardLogger(), tc.err)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			var body errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response was not the standard envelope: %s", rec.Body.String())
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.wantCode)
			}
		})
	}
}

func TestHandleServiceError_DoesNotLeakInternalDetail(t *testing.T) {
	internal := errors.New(`pq: relation "connector_credentials" does not exist; ` +
		`dsn=postgres://gridhook:hunter2@db.internal:5432/gridhook`)

	rec := httptest.NewRecorder()
	handleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), discardLogger(), internal)

	body := rec.Body.String()
	for _, secret := range []string{"hunter2", "db.internal", "connector_credentials", "pq:"} {
		if strings.Contains(body, secret) {
			t.Errorf("500 response leaked %q: %s", secret, body)
		}
	}
}

func TestHandleServiceError_KeepsAuthoredValidationMessages(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("%w: SOAP tools need an envelope", controlplane.ErrValidation)

	handleServiceError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), discardLogger(), err)

	if !strings.Contains(rec.Body.String(), "SOAP tools need an envelope") {
		t.Errorf("validation message was dropped: %s", rec.Body.String())
	}
}

func TestApiError_IncludesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(observability.WithRequestID(r.Context(), "req-abc123"))

	apiError(rec, r, http.StatusBadRequest, "invalid_body", "nope")

	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.RequestID != "req-abc123" {
		t.Errorf("requestId = %q, want req-abc123", body.Error.RequestID)
	}
}

func TestWriteJSON_SetsSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"a": "b"})

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestWriteJSON_UnencodableValueYields500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]any{"bad": make(chan int)})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "encoding_failed") {
		t.Errorf("body = %s", rec.Body.String())
	}
}
