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

func paginated(data any, p page, total int) map[string]any {
	return map[string]any{
		"data":     data,
		"page":     p.Page,
		"pageSize": p.PageSize,
		"total":    total,
	}
}

func intIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_id", fmt.Sprintf("%s must be a valid id", name))
		return 0, false
	}
	return v, true
}

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

func formatOptionalID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
