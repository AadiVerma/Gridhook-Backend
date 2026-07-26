package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/models"
)

func registerMCPRoutes(r chi.Router, d Deps) {
	r.Use(requireMCPAPIKey(d))

	r.Get("/{slug}/tools", func(w http.ResponseWriter, r *http.Request) {
		srv := mcpServerFromContext(r)
		tools, err := d.Tools.ListForServer(r.Context(), srv.ID)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": toMCPToolSchemas(tools)})
	})

	r.Post("/{slug}/", func(w http.ResponseWriter, r *http.Request) {
		srv := mcpServerFromContext(r)

		var body struct {
			Tool  string         `json:"tool"`
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		outcome, err := d.Dispatcher.Invoke(r.Context(), srv.ID, body.Tool, body.Input, dispatcher.Identity{
			OrganizationID: srv.OrganizationID,
		})
		if err != nil {
			apiError(w, http.StatusBadRequest, "dispatch_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	})
}

type mcpAuthCtxKey string

const mcpServerCtxKey mcpAuthCtxKey = "gridhook.mcpServer"

func requireMCPAPIKey(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := bearerToken(r)
			if key == "" {
				apiError(w, http.StatusUnauthorized, "unauthorized", "missing API key")
				return
			}
			srv, err := d.Servers.VerifyAPIKey(r.Context(), key)
			if err != nil {
				apiError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked API key")
				return
			}
			if slug := chi.URLParam(r, "slug"); slug != "" && slug != srv.Slug {
				apiError(w, http.StatusForbidden, "forbidden", "API key does not belong to this server")
				return
			}
			if srv.Status != models.ServerRunning {
				apiError(w, http.StatusServiceUnavailable, "server_stopped", "this MCP server is stopped")
				return
			}
			ctx := context.WithValue(r.Context(), mcpServerCtxKey, srv)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func mcpServerFromContext(r *http.Request) *models.MCPServer {
	srv, _ := r.Context().Value(mcpServerCtxKey).(*models.MCPServer)
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
