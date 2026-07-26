package engines

import (
	"context"
	"fmt"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

type Result struct {
	StatusCode int
	Headers    map[string]string
	Body       any
}

type Engine interface {
	Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error)
}

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
