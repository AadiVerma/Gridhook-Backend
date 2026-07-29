package api

import (
	"crypto/subtle"
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/idcodec"
	"gridhook.dev/connector-backend/internal/models"
)

const exportPageSize = 1000

func registerAuditLogRoutes(r chi.Router, d Deps) {
	r.Get("/audit-logs", handleListAuditLogs(d))
	r.Get("/audit-logs/export", handleExportAuditLogs(d))

	r.Get("/audit-logs/{id}", handleGetAuditLog(d))
}

func auditFilterFromRequest(w http.ResponseWriter, r *http.Request) (audit.ListFilter, bool) {
	connectorID, ok := intQueryParam(w, r, "connector")
	if !ok {
		return audit.ListFilter{}, false
	}
	serverID, ok := intQueryParam(w, r, "server")
	if !ok {
		return audit.ListFilter{}, false
	}
	toolID, ok := intQueryParam(w, r, "tool")
	if !ok {
		return audit.ListFilter{}, false
	}

	filter := audit.ListFilter{
		Status:      r.URL.Query().Get("status"),
		ConnectorID: connectorID,
		ServerID:    serverID,
		ToolID:      toolID,
	}
	if from := parseTime(r.URL.Query().Get("from")); from != nil {
		filter.From = from
	}
	if to := parseTime(r.URL.Query().Get("to")); to != nil {
		filter.To = to
	}
	return filter, true
}

func handleListAuditLogs(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, ok := auditFilterFromRequest(w, r)
		if !ok {
			return
		}
		p := pagination(r)
		filter.Page, filter.PageSize = p.Page, p.PageSize

		list, total, err := d.AuditReader.List(r.Context(), orgIDFromContext(r), filter)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, paginated(list, p, total))
	}
}

func handleGetAuditLog(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		inv, err := d.AuditReader.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, inv)
	}
}

func handleExportAuditLogs(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, ok := auditFilterFromRequest(w, r)
		if !ok {
			return
		}
		orgID := orgIDFromContext(r)
		filter.PageSize = exportPageSize

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="audit-logs.csv"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		cw := csv.NewWriter(w)
		if err := cw.Write([]string{
			"id", "time", "tool_id", "connector_id", "server_id",
			"status", "http_code", "duration_ms", "error",
		}); err != nil {
			d.Logger.ErrorContext(r.Context(), "audit export: write header", errAttr(err))
			return
		}

		for page := 1; ; page++ {
			filter.Page = page
			batch, total, err := d.AuditReader.List(r.Context(), orgID, filter)
			if err != nil {

				d.Logger.ErrorContext(r.Context(), "audit export: list page", errAttr(err))
				return
			}
			for _, inv := range batch {
				if err := cw.Write(invocationCSVRow(d.IDCodec, inv)); err != nil {
					d.Logger.ErrorContext(r.Context(), "audit export: write row", errAttr(err))
					return
				}
			}
			cw.Flush()
			if err := cw.Error(); err != nil {
				d.Logger.ErrorContext(r.Context(), "audit export: flush", errAttr(err))
				return
			}
			if len(batch) < filter.PageSize || page*filter.PageSize >= total {
				return
			}
		}
	}
}

func invocationCSVRow(codec *idcodec.Codec, inv *models.ToolInvocation) []string {
	return []string{
		codec.Encode(inv.ID),
		inv.CreatedAt.Format(time.RFC3339),
		codec.Encode(inv.ToolID),
		codec.Encode(inv.ConnectorID),
		codec.Encode(inv.MCPServerID),
		string(inv.Status),
		strconv.Itoa(inv.HTTPCode),
		strconv.Itoa(inv.DurationMs),
		inv.Error,
	}
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
		if d.InternalToken == "" {
			apiError(w, r, http.StatusNotFound, "not_found", "internal ingest is not enabled")
			return
		}
		presented := r.Header.Get("X-Internal-Token")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(d.InternalToken)) != 1 {
			apiError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or missing internal token")
			return
		}

		var inv models.ToolInvocation
		if err := decodeJSON(w, r, d.MaxRequestBytes, &inv); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if inv.OrganizationID == 0 || inv.ToolID == 0 {
			apiError(w, r, http.StatusBadRequest, "invalid_body",
				"organizationId and toolId are required")
			return
		}
		if inv.CreatedAt.IsZero() {
			inv.CreatedAt = time.Now()
		}

		d.AuditRecorder.Write(r.Context(), &inv)
		w.WriteHeader(http.StatusAccepted)
	}
}
