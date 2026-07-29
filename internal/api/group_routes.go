package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
)

func registerGroupRoutes(r chi.Router, d Deps) {
	r.Get("/tool-groups", handleListGroups(d))
	r.Post("/tool-groups", handleCreateGroup(d))
	r.Get("/tool-groups/{id}", handleGetGroup(d))
	r.Delete("/tool-groups/{id}", handleDeleteGroup(d))
	r.Put("/tool-groups/{id}/tools", handleAssignGroupTools(d))
}

func handleListGroups(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := d.Groups.List(r.Context(), orgIDFromContext(r))
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, groups)
	}
}

func handleCreateGroup(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.CreateGroupInput
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		g, err := d.Groups.Create(r.Context(), orgIDFromContext(r), body)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, g)
	}
}

func handleGetGroup(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		g, err := d.Groups.Get(r.Context(), orgIDFromContext(r), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, g)
	}
}

func handleDeleteGroup(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Groups.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAssignGroupTools(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			ToolIDs []int64 `json:"toolIds"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		if err := d.Groups.AssignTools(r.Context(), orgIDFromContext(r), id, body.ToolIDs); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
