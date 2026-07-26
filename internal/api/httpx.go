// Package api wires every admin-facing route from APIDOC.md plus the
// MCP-client-facing runtime routes onto one chi.Mux. Handlers stay thin:
// decode request -> call one controlplane/dispatcher method -> encode
// response. Query/business logic lives in internal/controlplane and
// internal/dispatcher, not here.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/identity"
)

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError matches APIDOC.md's error envelope: { error: { code, message, field? } }.
func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, controlplane.ErrNotFound), errors.Is(err, identity.ErrUserNotFound):
		apiError(w, http.StatusNotFound, "not_found", "resource not found")
	default:
		apiError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

type page struct {
	Page     int
	PageSize int
}

func pagination(r *http.Request) page {
	p := page{Page: 1, PageSize: 50}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		p.Page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && v > 0 && v <= 200 {
		p.PageSize = v
	}
	return p
}

// paginated matches APIDOC.md's list envelope: { data, page, pageSize, total }.
func paginated(data any, p page, total int) map[string]any {
	return map[string]any{
		"data":     data,
		"page":     p.Page,
		"pageSize": p.PageSize,
		"total":    total,
	}
}

// intIDParam parses a chi URL path parameter as an int64 primary-key id. On
// failure it writes the 400 itself; callers must return immediately when ok
// is false.
func intIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_id", fmt.Sprintf("%s must be a valid id", name))
		return 0, false
	}
	return v, true
}

// intQueryParam parses an optional query-string id. Absent -> (0, true),
// matching this codebase's 0-sentinel convention for "not set".
// Present-but-invalid -> writes 400 and returns ok=false.
func intQueryParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_id", fmt.Sprintf("%s must be a valid id", name))
		return 0, false
	}
	return v, true
}

// formatOptionalID renders a possibly-unset (0-sentinel) id as blank for
// display/export, matching the pre-conversion coalesce(x::text,'') behavior.
func formatOptionalID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
