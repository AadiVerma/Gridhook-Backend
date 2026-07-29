package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/observability"
)

func errAttr(err error) slog.Attr {
	return slog.Any("error", err)
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"encoding_failed","message":"failed to encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func apiError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: observability.RequestIDFromContext(r.Context()),
	}})
}

func handleServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, controlplane.ErrNotFound),
		errors.Is(err, audit.ErrNotFound),
		errors.Is(err, identity.ErrUserNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "resource not found")

	case errors.Is(err, controlplane.ErrValidation),
		errors.Is(err, identity.ErrInvalidEmail),
		errors.Is(err, identity.ErrInvalidRole),
		identity.IsPasswordPolicyError(err):

		apiError(w, r, http.StatusBadRequest, "validation_failed", err.Error())

	case errors.Is(err, schemes.ErrIncompleteCredentials):
		apiError(w, r, http.StatusBadRequest, "credentials_incomplete",
			"the connector's stored credentials are missing a required field")

	case errors.Is(err, controlplane.ErrUnauthorized):
		apiError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or revoked credential")

	case errors.Is(err, controlplane.ErrConflict),
		errors.Is(err, identity.ErrAlreadyAMember),
		errors.Is(err, identity.ErrEmailTaken):
		apiError(w, r, http.StatusConflict, "conflict", err.Error())

	default:
		logger.ErrorContext(r.Context(), "unhandled service error",
			slog.Any("error", err),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path))
		apiError(w, r, http.StatusInternalServerError, "internal_error",
			"an internal error occurred; quote the requestId when reporting it")
	}
}
