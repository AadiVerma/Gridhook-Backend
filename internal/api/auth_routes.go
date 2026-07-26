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

	r.Post("/auth/forgot-password", notImplemented)
	r.Post("/auth/reset-password", notImplemented)
	r.Post("/auth/verify-email", notImplemented)
	r.Post("/auth/accept-invite", notImplemented)
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	apiError(w, http.StatusNotImplemented, "not_implemented", "this endpoint is not wired to an email/token delivery provider yet")
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
			apiError(w, http.StatusConflict, "email_taken", "an account with this email already exists")
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
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &body); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		result, err := d.Auth.Login(r.Context(), body.Email, body.Password)
		if errors.Is(err, identity.ErrInvalidCredentials) {
			apiError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", err.Error())
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
