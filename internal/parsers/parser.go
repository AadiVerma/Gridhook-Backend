package parsers

import "gridhook.dev/connector-backend/internal/models"

type DraftTool struct {
	Name            string
	Method          models.HTTPMethod
	Path            string
	Description     string
	Parameters      map[string]any
	EndpointMapping map[string]any
	ResponseMapping map[string]any
	OutputSchema    map[string]any
}

type ParseResult struct {
	EngineType models.EngineType
	BaseURL    string
	Tools      []DraftTool
}

type Parser interface {
	Parse(raw []byte) (*ParseResult, error)
}

type Registry struct {
	parsers map[string]Parser
}

func NewRegistry() *Registry {
	return &Registry{
		parsers: map[string]Parser{
			"openapi":     &OpenAPIParser{},
			"wsdl":        &WSDLParser{},
			"postman":     &PostmanParser{},
			"curl":        &CurlParser{},
			"graphql-sdl": &GraphQLSDLParser{},
		},
	}
}

func (r *Registry) For(format string) (Parser, bool) {
	p, ok := r.parsers[format]
	return p, ok
}
