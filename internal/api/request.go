package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/identity"
)

type nullableID struct {
	set   bool
	value *int64
}

func (n *nullableID) UnmarshalJSON(data []byte) error {
	n.set = true
	if string(data) == "null" {
		n.value = nil
		return nil
	}
	return json.Unmarshal(data, &n.value)
}

func (n nullableID) ptr() **int64 {
	if !n.set {
		return nil
	}
	v := n.value
	return &v
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, v any) error {
	defer func() { _ = r.Body.Close() }()

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	if err := decoder.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("request body exceeds %d bytes", maxBytes)
		}
		if errors.Is(err, io.EOF) {
			return errors.New("request body is empty")
		}
		return err
	}
	return nil
}

func readBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, fmt.Errorf("request body exceeds %d bytes", maxBytes)
		}
		return nil, err
	}
	return raw, nil
}

type page struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

func pagination(r *http.Request) page {
	p := page{Page: 1, PageSize: defaultPageSize}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		p.Page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && v > 0 && v <= maxPageSize {
		p.PageSize = v
	}
	return p
}

type paginatedResponse struct {
	Data     any `json:"data"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

func paginated(data any, p page, total int) paginatedResponse {
	return paginatedResponse{Data: data, Page: p.Page, PageSize: p.PageSize, Total: total}
}

func intIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || v <= 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_id", fmt.Sprintf("%s must be a positive integer id", name))
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
	if err != nil || v <= 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_id", fmt.Sprintf("%s must be a positive integer id", name))
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

func orgIDFromContext(r *http.Request) int64 {
	user, ok := identity.UserFromContext(r.Context())
	if !ok {
		return 0
	}
	return user.OrganizationID
}
