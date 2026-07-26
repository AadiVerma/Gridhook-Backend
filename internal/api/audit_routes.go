package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/models"
)

func registerAuditLogRoutes(r chi.Router, d Deps) {
	r.Get("/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		p := pagination(r)
		connectorID, ok := intQueryParam(w, r, "connector")
		if !ok {
			return
		}
		serverID, ok := intQueryParam(w, r, "server")
		if !ok {
			return
		}
		toolID, ok := intQueryParam(w, r, "tool")
		if !ok {
			return
		}
		filter := audit.ListFilter{
			Status:      r.URL.Query().Get("status"),
			ConnectorID: connectorID,
			ServerID:    serverID,
			ToolID:      toolID,
			Page:        p.Page,
			PageSize:    p.PageSize,
		}
		if from := parseTime(r.URL.Query().Get("from")); from != nil {
			filter.From = from
		}
		if to := parseTime(r.URL.Query().Get("to")); to != nil {
			filter.To = to
		}

		list, total, err := d.Audit.List(r.Context(), orgIDFromContext(r), filter)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, paginated(list, p, total))
	})

	r.Get("/audit-logs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		inv, err := d.Audit.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, inv)
	})

	r.Get("/audit-logs/export", func(w http.ResponseWriter, r *http.Request) {
		connectorID, ok := intQueryParam(w, r, "connector")
		if !ok {
			return
		}
		serverID, ok := intQueryParam(w, r, "server")
		if !ok {
			return
		}
		toolID, ok := intQueryParam(w, r, "tool")
		if !ok {
			return
		}
		filter := audit.ListFilter{
			Status:      r.URL.Query().Get("status"),
			ConnectorID: connectorID,
			ServerID:    serverID,
			ToolID:      toolID,
			Page:        1,
			PageSize:    10000,
		}
		list, _, err := d.Audit.List(r.Context(), orgIDFromContext(r), filter)
		if err != nil {
			handleServiceError(w, err)
			return
		}

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="audit-logs.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "time", "tool_id", "connector_id", "server_id", "status", "http_code", "duration_ms", "error"})
		for _, inv := range list {
			_ = cw.Write([]string{
				strconv.FormatInt(inv.ID, 10), inv.CreatedAt.Format(time.RFC3339), strconv.FormatInt(inv.ToolID, 10),
				strconv.FormatInt(inv.ConnectorID, 10), formatOptionalID(inv.MCPServerID),
				string(inv.Status), fmt.Sprint(inv.HTTPCode), fmt.Sprint(inv.DurationMs), inv.Error,
			})
		}
		cw.Flush()
	})
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func handleInternalAuditIngest(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.InternalToken == "" || r.Header.Get("X-Internal-Token") != d.InternalToken {
			apiError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing internal token")
			return
		}
		var inv models.ToolInvocation
		if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if inv.CreatedAt.IsZero() {
			inv.CreatedAt = time.Now()
		}
		d.Audit.Write(r.Context(), &inv)
		w.WriteHeader(http.StatusAccepted)
	}
}
