package parsers

import (
	"slices"

	"gridhook.dev/connector-backend/internal/models"
)

type Format = string

const (
	FormatOpenAPI    Format = "openapi"
	FormatWSDL       Format = "wsdl"
	FormatPostman    Format = "postman"
	FormatCurl       Format = "curl"
	FormatGraphQLSDL Format = "graphql-sdl"
)

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
	parsers map[Format]Parser
}

func NewRegistry() *Registry {
	return &Registry{
		parsers: map[Format]Parser{
			FormatOpenAPI:    &OpenAPIParser{},
			FormatWSDL:       &WSDLParser{},
			FormatPostman:    &PostmanParser{},
			FormatCurl:       &CurlParser{},
			FormatGraphQLSDL: &GraphQLSDLParser{},
		},
	}
}

func (r *Registry) For(format Format) (Parser, bool) {
	p, ok := r.parsers[format]
	return p, ok
}

func (r *Registry) Formats() []Format {
	out := make([]Format, 0, len(r.parsers))
	for name := range r.parsers {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
