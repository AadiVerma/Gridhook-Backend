package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
)

func registerMCPServerRoutes(r chi.Router, d Deps) {
	r.Get("/mcp-servers", func(w http.ResponseWriter, r *http.Request) {
		servers, err := d.Servers.List(r.Context(), orgIDFromContext(r), r.URL.Query().Get("status"), r.URL.Query().Get("q"))
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, servers)
	})

	r.Post("/mcp-servers", func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.CreateServerInput
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		srv, err := d.Servers.Create(r.Context(), orgIDFromContext(r), body)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, srv)
	})

	r.Get("/mcp-servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		srv, err := d.Servers.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	})

	r.Patch("/mcp-servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.UpdateServerInput
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		srv, err := d.Servers.Update(r.Context(), orgIDFromContext(r), id, body)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	})

	r.Delete("/mcp-servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Servers.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// "Assign a whole connector" from the UI: expands to every tool_group
	// that has at least one tool under each given connector, then replaces
	// the server's full group assignment — this is the mechanism that
	// makes group-wise LLM invocation transparent to a UI that still just
	// thinks in terms of "assign this connector."
	r.Put("/mcp-servers/{id}/connectors", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ConnectorIDs []int64 `json:"connectorIds"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var groupIDs []int64
		for _, cid := range body.ConnectorIDs {
			ids, err := d.Groups.GroupsForConnector(r.Context(), cid)
			if err != nil {
				handleServiceError(w, err)
				return
			}
			groupIDs = append(groupIDs, ids...)
		}
		if err := d.Groups.SetServerGroups(r.Context(), id, groupIDs); err != nil {
			handleServiceError(w, err)
			return
		}
		srv, err := d.Servers.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	})

	// Finer-grained equivalent for clients that want to assign specific
	// tool_groups directly rather than whole connectors.
	r.Put("/mcp-servers/{id}/tool-groups", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ToolGroupIDs []int64 `json:"toolGroupIds"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Groups.SetServerGroups(r.Context(), id, body.ToolGroupIDs); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Get("/mcp-servers/{id}/tools", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		tools, err := d.Tools.ListForServer(r.Context(), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tools)
	})

	registerAPIKeyRoutes(r, d)
}

func registerAPIKeyRoutes(r chi.Router, d Deps) {
	r.Get("/mcp-servers/{id}/api-keys", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		keys, err := d.Servers.ListAPIKeys(r.Context(), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, keys)
	})

	r.Post("/mcp-servers/{id}/api-keys", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Label string `json:"label"`
			Live  bool   `json:"live"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		fullKey, key, err := d.Servers.CreateAPIKey(r.Context(), id, body.Label, body.Live)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		// The full secret is returned exactly once, here, and never again.
		writeJSON(w, http.StatusCreated, map[string]any{"key": fullKey, "meta": key})
	})

	r.Delete("/mcp-servers/{id}/api-keys/{keyId}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		keyID, ok := intIDParam(w, r, "keyId")
		if !ok {
			return
		}
		if err := d.Servers.RevokeAPIKey(r.Context(), id, keyID); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
