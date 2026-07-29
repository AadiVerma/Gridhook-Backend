package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/models"
)

func registerMCPRoutes(r chi.Router, d Deps) {
	r.Route("/{slug}", func(r chi.Router) {
		r.Use(requireMCPAPIKey(d))

		r.Get("/tools", handleMCPListTools(d))
		r.Post("/", handleMCPInvoke(d))
	})
}

func handleMCPListTools(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := mcpServerFromContext(r)
		tools, err := d.Tools.ListForServer(r.Context(), srv.OrganizationID, srv.ID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": toMCPToolSchemas(tools)})
	}
}

func handleMCPInvoke(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srv := mcpServerFromContext(r)

		var body struct {
			Tool  string         `json:"tool"`
			Input map[string]any `json:"input"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if body.Tool == "" {
			apiError(w, r, http.StatusBadRequest, "invalid_body", "tool is required")
			return
		}

		outcome, err := d.Dispatcher.Invoke(r.Context(), srv.ID, body.Tool, body.Input, dispatcher.Identity{
			OrganizationID: srv.OrganizationID,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}

type mcpAuthCtxKey struct{}

func requireMCPAPIKey(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := bearerToken(r)
			if key == "" {
				apiError(w, r, http.StatusUnauthorized, "unauthorized", "missing API key")
				return
			}
			srv, err := d.Servers.VerifyAPIKey(r.Context(), key)
			if err != nil {
				apiError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or revoked API key")
				return
			}

			if slug := chi.URLParam(r, "slug"); slug != srv.Slug {
				apiError(w, r, http.StatusForbidden, "forbidden", "API key does not belong to this server")
				return
			}
			if srv.Status != models.ServerRunning {
				apiError(w, r, http.StatusServiceUnavailable, "server_stopped", "this MCP server is stopped")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), mcpAuthCtxKey{}, srv)))
		})
	}
}

func mcpServerFromContext(r *http.Request) *models.MCPServer {
	srv, _ := r.Context().Value(mcpAuthCtxKey{}).(*models.MCPServer)
	return srv
}

func toMCPToolSchemas(tools []*models.MCPTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.Parameters,
		})
	}
	return out
}
