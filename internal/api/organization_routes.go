package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/identity"
)

func registerOrganizationRoutes(r chi.Router, d Deps) {
	r.Get("/organizations/{id}", func(w http.ResponseWriter, r *http.Request) {
		reqID, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		id, ok := requireOwnOrg(w, r, reqID)
		if !ok {
			return
		}
		org, err := d.Organizations.Get(r.Context(), id)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, org)
	})

	r.Patch("/organizations/{id}", func(w http.ResponseWriter, r *http.Request) {
		reqID, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		id, ok := requireOwnOrg(w, r, reqID)
		if !ok {
			return
		}
		var body struct {
			Name     *string `json:"name"`
			Timezone *string `json:"timezone"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		org, err := d.Organizations.Update(r.Context(), id, controlplane.UpdateOrganizationInput{Name: body.Name, Timezone: body.Timezone})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, org)
	})

	r.Delete("/organizations/{id}", func(w http.ResponseWriter, r *http.Request) {
		reqID, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		id, ok := requireOwnOrg(w, r, reqID)
		if !ok {
			return
		}
		if err := d.Organizations.Delete(r.Context(), id); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// requireOwnOrg is the single enforcement point mentioned in APIDOC.md's
// conventions: every resource is scoped to the caller's organization, and
// that scoping happens here, not per-handler. Every other route file below
// calls orgIDFromContext directly for the same reason — it's one function,
// not a scattered convention.
func requireOwnOrg(w http.ResponseWriter, r *http.Request, requestedID int64) (int64, bool) {
	orgID := orgIDFromContext(r)
	if requestedID != orgID {
		apiError(w, http.StatusForbidden, "forbidden", "cannot access another organization")
		return 0, false
	}
	return orgID, true
}

func orgIDFromContext(r *http.Request) int64 {
	user, ok := identity.UserFromContext(r.Context())
	if !ok {
		return 0
	}
	return user.OrganizationID
}
