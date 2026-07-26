package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
)

// registerGroupRoutes backs /tool-groups — not in APIDOC.md's original
// contract (that doc predates the multi-API/grouping requirement) but
// additive under the same /api/v1 prefix and auth. This is the admin
// surface for the clustering ARCHITECTURE.md §6 describes: create a group,
// assign tools to it, then assign the group to one or more MCP servers via
// mcpserver_routes.go's PUT /mcp-servers/:id/connectors.
func registerGroupRoutes(r chi.Router, d Deps) {
	r.Get("/tool-groups", func(w http.ResponseWriter, r *http.Request) {
		groups, err := d.Groups.List(r.Context(), orgIDFromContext(r))
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, groups)
	})

	r.Post("/tool-groups", func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.CreateGroupInput
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		g, err := d.Groups.Create(r.Context(), orgIDFromContext(r), body)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, g)
	})

	r.Get("/tool-groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		g, err := d.Groups.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, g)
	})

	r.Delete("/tool-groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Groups.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Body: {"toolIds": [1, 2]} — moves each tool into this group.
	r.Put("/tool-groups/{id}/tools", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ToolIDs []int64 `json:"toolIds"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Groups.AssignTools(r.Context(), id, body.ToolIDs); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
