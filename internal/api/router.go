package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"gridhook.dev/connector-backend/internal/audit"
	"gridhook.dev/connector-backend/internal/controlplane"
	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/identity"
	"gridhook.dev/connector-backend/internal/parsers"
)

// Deps is every service a route handler needs. Built once in cmd/server and
// threaded through here — no globals, no service locator.
type Deps struct {
	Sessions      *identity.SessionService
	Auth          *identity.AuthService
	Users         *identity.UserService
	Organizations *controlplane.OrganizationService
	Connectors    *controlplane.ConnectorService
	APIs          *controlplane.APIService
	Tools         *controlplane.ToolService
	Groups        *controlplane.GroupService
	Servers       *controlplane.MCPServerService
	Audit         *audit.Logger
	Dispatcher    *dispatcher.Dispatcher
	Parsers       *parsers.Registry
	// InternalToken gates POST /internal/audit-logs — a shared secret the
	// MCP runtime presents via X-Internal-Token, since that endpoint has no
	// user session to verify.
	InternalToken string
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// MCP-client-facing runtime: its own API-key auth, no admin session.
	r.Route("/mcp", func(r chi.Router) {
		registerMCPRoutes(r, d)
	})

	// Internal, server-to-server ingestion (called by the MCP runtime, not
	// the frontend) — authenticated by a shared internal token, not a user
	// session or an MCP server API key.
	r.Post("/internal/audit-logs", handleInternalAuditIngest(d))

	r.Route("/api/v1", func(r chi.Router) {
		// Auth/session endpoints issue the session, so they run before the
		// RequireSession gate.
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
