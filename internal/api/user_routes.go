package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/models"
)

func registerUserRoutes(r chi.Router, d Deps) {
	r.Get("/users", handleListUsers(d))
	r.Post("/users/invite", handleInviteUser(d))
	r.Patch("/users/{id}", handleUpdateUser(d))
	r.Delete("/users/{id}", handleDeleteUser(d))
	r.Get("/roles", handleListRoles(d))
}

func handleListUsers(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := d.Users.List(r.Context(), orgIDFromContext(r))
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
	}
}

func handleInviteUser(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string          `json:"email"`
			Name  string          `json:"name"`
			Role  models.UserRole `json:"role"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		u, err := d.Users.Invite(r.Context(), orgIDFromContext(r), body.Email, body.Name, body.Role)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, u)
	}
}

func handleUpdateUser(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			Role   *models.UserRole   `json:"role"`
			Status *models.UserStatus `json:"status"`
		}
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		u, err := d.Users.Update(r.Context(), orgIDFromContext(r), id, identity.UpdateUserInput{
			Role: body.Role, Status: body.Status,
		})
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	}
}

func handleDeleteUser(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := intIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := d.Users.Delete(r.Context(), orgIDFromContext(r), id); err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListRoles(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roles, err := d.Users.ListRoles(r.Context(), orgIDFromContext(r))
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}
		writeJSON(w, http.StatusOK, roles)
	}
}
