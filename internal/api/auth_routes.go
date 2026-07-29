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

	r.With(identity.RequireSession(d.Sessions)).Get("/auth/me", handleMe(d))
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
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		result, err := d.Auth.Register(r.Context(), identity.RegisterInput{
			Name: body.Name, Email: body.Email, Password: body.Password, Organization: body.Organization,
		})
		if errors.Is(err, identity.ErrEmailTaken) {
			apiError(w, r, http.StatusConflict, "email_taken",
				"an account with this email already exists — log in instead")
			return
		}
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
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
		if err := decodeJSON(w, r, d.MaxRequestBytes, &body); err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		result, memberships, err := d.Auth.Login(r.Context(), body.Email, body.Password, body.OrganizationID)
		switch {
		case errors.Is(err, identity.ErrInvalidCredentials):

			apiError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		case errors.Is(err, identity.ErrNotAMember):
			apiError(w, r, http.StatusForbidden, "not_a_member",
				"this account is not a member of that organization")
			return
		case err != nil:
			handleServiceError(w, r, d.Logger, err)
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

		if token := bearerToken(r); token != "" {
			if err := d.Sessions.Revoke(r.Context(), token); err != nil {
				d.Logger.WarnContext(r.Context(), "logout: revoke session failed", errAttr(err))
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMe(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := identity.UserFromContext(r.Context())
		if !ok {
			apiError(w, r, http.StatusUnauthorized, "unauthorized", "no active session")
			return
		}

		org, err := d.Organizations.Get(r.Context(), user.OrganizationID)
		if err != nil {
			handleServiceError(w, r, d.Logger, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user": user,
			"org":  org,
		})
	}
}
