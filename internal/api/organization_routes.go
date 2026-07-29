package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
)

func registerOrganizationRoutes(r chi.Router, d Deps) {
	r.Get("/organizations/{id}", handleGetOrganization(d))
	r.Patch("/organizations/{id}", handleUpdateOrganization(d))
	r.Delete("/organizations/{id}", handleDeleteOrganization(d))
}

func handleGetOrganization(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := requireOwnOrgParam(w, r)
		if !ok {
			return
		}
		org, err := d.Organizations.Get(r.Context(), id)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, org)
	}
}

func handleUpdateOrganization(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := requireOwnOrgParam(w, r)
		if !ok {
			return
		}
		var body struct {
			Name     *string `json:"name"`
			Timezone *string `json:"timezone"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		org, err := d.Organizations.Update(r.Context(), id, controlplane.UpdateOrganizationInput{
			Name: body.Name, Timezone: body.Timezone,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, org)
	}
}

func handleDeleteOrganization(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := requireOwnOrgParam(w, r)
		if !ok {
			return
		}
		if err := d.Organizations.Delete(r.Context(), id); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func requireOwnOrgParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	requestedID, ok := intIDParam(w, r, "id")
	if !ok {
		return 0, false
	}
	orgID := orgIDFromContext(r)
	if orgID == 0 || requestedID != orgID {
		apiError(w, r, http.StatusForbidden, "forbidden", "cannot access another organization")
		return 0, false
	}
	return orgID, true
}
