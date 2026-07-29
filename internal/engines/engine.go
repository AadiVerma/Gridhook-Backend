package engines

import (
	"context"
	"fmt"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

// staticHeadersFrom reads an EndpointMapping's "headers" entry (a flat
// map[string]any) into request headers every engine applies the same way:
// after its own protocol defaults, before credential headers.
func staticHeadersFrom(mapping map[string]any) map[string]string {
	out := map[string]string{}
	if headers, ok := mapping["headers"].(map[string]any); ok {
		for k, v := range headers {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

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
