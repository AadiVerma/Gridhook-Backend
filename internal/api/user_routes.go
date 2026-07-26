package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/models"
)

func registerUserRoutes(r chi.Router, d Deps) {
	r.Get("/users", func(w http.ResponseWriter, r *http.Request) {
		users, err := d.Users.List(r.Context(), orgIDFromContext(r))
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
	})

	r.Post("/users/invite", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string          `json:"email"`
			Name  string          `json:"name"`
			Role  models.UserRole `json:"role"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		u, err := d.Users.Invite(r.Context(), orgIDFromContext(r), body.Email, body.Name, body.Role)
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, u)
	})

	r.Patch("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Role   *models.UserRole   `json:"role"`
			Status *models.UserStatus `json:"status"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		u, err := d.Users.Update(r.Context(), orgIDFromContext(r), id, identity.UpdateUserInput{Role: body.Role, Status: body.Status})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	})

	r.Delete("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Users.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Get("/roles", func(w http.ResponseWriter, r *http.Request) {
		roles, err := d.Users.ListRoles(r.Context(), orgIDFromContext(r))
		if err != nil {
			handleServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, roles)
	})
}
