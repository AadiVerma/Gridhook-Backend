package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
)

func registerMCPServerRoutes(r chi.Router, d Deps) {
	r.Get("/mcp-servers", handleListServers(d))
	r.Post("/mcp-servers", handleCreateServer(d))
	r.Get("/mcp-servers/{id}", handleGetServer(d))
	r.Patch("/mcp-servers/{id}", handleUpdateServer(d))
	r.Delete("/mcp-servers/{id}", handleDeleteServer(d))

	r.Put("/mcp-servers/{id}/connectors", handleSetServerConnectors(d))
	r.Put("/mcp-servers/{id}/tool-groups", handleSetServerToolGroups(d))
	r.Get("/mcp-servers/{id}/tools", handleListServerTools(d))

	registerAPIKeyRoutes(r, d)
}

func handleListServers(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		servers, err := d.Servers.List(r.Context(), orgIDFromContext(r),
			r.URL.Query().Get("status"), r.URL.Query().Get("q"))
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, servers)
	}
}

func handleCreateServer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.CreateServerInput
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		srv, err := d.Servers.Create(r.Context(), orgIDFromContext(r), body)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, srv)
	}
}

func handleGetServer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		srv, err := d.Servers.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	}
}

func handleUpdateServer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body controlplane.UpdateServerInput
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		srv, err := d.Servers.Update(r.Context(), orgIDFromContext(r), id, body)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	}
}

func handleDeleteServer(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Servers.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSetServerConnectors(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			ConnectorIDs []int64 `json:"connectorIds"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		orgID := orgIDFromContext(r)
		var groupIDs []int64
		for _, connectorID := range body.ConnectorIDs {
			ids, err := d.Groups.GroupsForConnector(r.Context(), orgID, connectorID)
			if err != nil {
				handleServiceError(w, r, d.Logger, err)
				return
			}
			groupIDs = append(groupIDs, ids...)
		}
		if err := d.Groups.SetServerGroups(r.Context(), orgID, id, groupIDs); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		srv, err := d.Servers.Get(r.Context(), orgID, id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	}
}

func handleSetServerToolGroups(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			ToolGroupIDs []int64 `json:"toolGroupIds"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if err := d.Groups.SetServerGroups(r.Context(), orgIDFromContext(r), id, body.ToolGroupIDs); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListServerTools(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		tools, err := d.Tools.ListForServer(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, tools)
	}
}

func registerAPIKeyRoutes(r chi.Router, d Deps) {
	r.Get("/mcp-servers/{id}/api-keys", handleListAPIKeys(d))
	r.Post("/mcp-servers/{id}/api-keys", handleCreateAPIKey(d))
	r.Delete("/mcp-servers/{id}/api-keys/{keyId}", handleRevokeAPIKey(d))
}

func handleListAPIKeys(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		keys, err := d.Servers.ListAPIKeys(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, keys)
	}
}

func handleCreateAPIKey(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			Label string `json:"label"`
			Live  bool   `json:"live"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		fullKey, key, err := d.Servers.CreateAPIKey(r.Context(), orgIDFromContext(r), id, body.Label, body.Live)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"key": fullKey, "meta": key})
	}
}

func handleRevokeAPIKey(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		keyID, ok := intIDParam(w, r, "keyId")
		if !ok {
			return
		}
		if err := d.Servers.RevokeAPIKey(r.Context(), orgIDFromContext(r), id, keyID); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
