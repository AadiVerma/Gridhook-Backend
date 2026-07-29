package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/observability"
	"gridhook.dev/connector-backend/internal/parsers"
)

type Deps struct {
	Logger *slog.Logger

	MaxRequestBytes int64

	InternalToken  string
	AllowedOrigins []string

	Sessions      *identity.SessionService
	Auth          *identity.AuthService
	Users         *identity.UserService
	Organizations *controlplane.OrganizationService
	Connectors    *controlplane.ConnectorService
	APIs          *controlplane.APIService
	Tools         *controlplane.ToolService
	Groups        *controlplane.GroupService
	Servers       *controlplane.MCPServerService

	AuditReader   *audit.Reader
	AuditRecorder *audit.Recorder
	Dispatcher    *dispatcher.Dispatcher
	Parsers       *parsers.Registry

	Upstream *httpx.Client

	Ready func(ctx context.Context) error
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(observability.RequestID)
	r.Use(observability.AccessLog(d.Logger))
	r.Use(observability.Recoverer(d.Logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.AllowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type", observability.HeaderRequestID},
		ExposedHeaders:   []string{observability.HeaderRequestID},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	registerHealthRoutes(r, d)

	r.Route("/mcp", func(r chi.Router) {
		registerMCPRoutes(r, d)
	})

	r.Post("/internal/audit-logs", handleInternalAuditIngest(d))

	r.Route("/api/v1", func(r chi.Router) {
		registerAuthRoutes(r, d)

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireSession(d.Sessions))
			r.Use(withOrgContext)
			registerOrganizationRoutes(r, d)
			registerUserRoutes(r, d)
			registerConnectorRoutes(r, d)
			registerGroupRoutes(r, d)
			registerMCPServerRoutes(r, d)
			registerAuditLogRoutes(r, d)
		})
	})

	return r
}

func withOrgContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := identity.UserFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(observability.WithOrgID(r.Context(), user.OrganizationID)))
	})
}

const readinessTimeout = 3 * time.Second

func registerHealthRoutes(r chi.Router, d Deps) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if d.Ready == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		if err := d.Ready(ctx); err != nil {
			d.Logger.WarnContext(ctx, "readiness check failed", slog.Any("error", err))
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unready",
				"reason": "database unavailable",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}
