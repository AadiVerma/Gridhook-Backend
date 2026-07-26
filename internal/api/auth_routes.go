package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gridhook.dev/connector-backend/internal/identity"
)

func registerAuthRoutes(r chi.Router, d Deps) {
	r.Post("/auth/register", handleRegister(d))
	r.Post("/auth/login", handleLogin(d))
	r.Post("/auth/logout", handleLogout(d))
	r.Get("/auth/me", identity.RequireSession(d.Sessions)(http.HandlerFunc(handleMe)).ServeHTTP)
}

func orgMembershipsJSON(memberships []identity.OrgMembership) []map[string]any {
	orgs := make([]map[string]any, len(memberships))
	for i, m := range memberships {
		orgs[i] = map[string]any{
			"id":   m.OrganizationID,
			"name": m.OrganizationName,
			"slug": m.OrganizationSlug,
			"role": m.Role,
		}
	}
	return orgs
}

func handleRegister(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name         string `json:"name"`
			Email        string `json:"email"`
			Password     string `json:"password"`
			Organization string `json:"organization"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		result, err := d.Auth.Register(r.Context(), identity.RegisterInput{
			Name: body.Name, Email: body.Email, Password: body.Password, Organization: body.Organization,
		})
		if errors.Is(err, identity.ErrEmailTaken) {
			apiError(w, http.StatusConflict, "email_taken", "an account with this email already exists — log in instead")
			return
		}
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token": result.Session.AccessToken,
			"user":  result.User,
		})
	}
}

func handleLogin(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email          string `json:"email"`
			Password       string `json:"password"`
			OrganizationID int64  `json:"organizationId"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		result, memberships, err := d.Auth.Login(r.Context(), body.Email, body.Password, body.OrganizationID)
		if errors.Is(err, identity.ErrInvalidCredentials) {
			apiError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		if errors.Is(err, identity.ErrNotAMember) {
			apiError(w, http.StatusForbidden, "not_a_member", "this account is not a member of that organization")
			return
		}
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if result == nil {
			writeJSON(w, http.StatusOK, map[string]any{"organizations": orgMembershipsJSON(memberships)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"token": result.Session.AccessToken,
			"user":  result.User,
		})
	}
}

func handleLogout(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token != "" {
			_ = d.Sessions.Revoke(r.Context(), token)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	user, _ := identity.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, user)
}
