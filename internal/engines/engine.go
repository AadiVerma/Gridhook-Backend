// Package engines makes the real outbound call for one tool invocation. Each
// engine is keyed by models.EngineType and knows nothing about auth schemes
// (it just applies whatever headers/query params the auth broker resolved)
// or about sessions/audit — it is the single-purpose "actually call the
// upstream" step in the dispatcher's pipeline.
package engines

import (
	"context"
	"fmt"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

// Result is the engine's normalized output, before the dispatcher applies
// the tool's response_mapping.
type Result struct {
	StatusCode int
	Headers    map[string]string
	Body       any // decoded JSON (map[string]any / []any) when possible, else raw string
}

type Engine interface {
	Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error)
}

// Registry dispatches to the right Engine by models.EngineType — this is
// EngineRegistry from trd.md's component diagram.
type Registry struct {
	engines map[models.EngineType]Engine
}

func NewRegistry() *Registry {
	return &Registry{
		engines: map[models.EngineType]Engine{
			models.EngineREST:    NewRestEngine(),
			models.EngineSOAP:    NewSoapEngine(),
			models.EngineGraphQL: NewGraphQLEngine(),
		},
	}
}

func (r *Registry) For(engineType models.EngineType) (Engine, error) {
	e, ok := r.engines[engineType]
	if !ok {
		return nil, fmt.Errorf("engines: no engine registered for type %q", engineType)
	}
	return e, nil
}
