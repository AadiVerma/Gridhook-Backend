package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/parsers"
)

type Deps struct {
	Sessions       *identity.SessionService
	Auth           *identity.AuthService
	Users          *identity.UserService
	Organizations  *controlplane.OrganizationService
	Connectors     *controlplane.ConnectorService
	APIs           *controlplane.APIService
	Tools          *controlplane.ToolService
	Groups         *controlplane.GroupService
	Servers        *controlplane.MCPServerService
	Audit          *audit.Logger
	Dispatcher     *dispatcher.Dispatcher
	Parsers        *parsers.Registry
	InternalToken  string
	AllowedOrigins []string
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/mcp", func(r chi.Router) {
		registerMCPRoutes(r, d)
	})

	r.Post("/internal/audit-logs", handleInternalAuditIngest(d))

	r.Route("/api/v1", func(r chi.Router) {
		registerAuthRoutes(r, d)

		r.Group(func(r chi.Router) {
			r.Use(identity.RequireSession(d.Sessions))
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
