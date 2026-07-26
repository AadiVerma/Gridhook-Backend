// Package parsers turns an uploaded spec (OpenAPI, WSDL, Postman, curl) into
// a slice of DraftTool the control plane can persist as mcp_tools in bulk.
// Every parser only ever produces DraftTool — it never touches the
// database — so ToolService.BulkCreate is the single place drafts become
// real rows, regardless of source format.
package parsers

import "gridhook.dev/connector-backend/internal/models"

// DraftTool is a parser's proposed tool, before it has an ID or a
// connector_api to belong to.
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

// ParseResult is everything a spec upload implies: the API's own connection
// details (so /connectors/import can create the connector_api in the same
// shot) plus the tools drafted from it.
type ParseResult struct {
	EngineType models.EngineType
	BaseURL    string
	Tools      []DraftTool
}

type Parser interface {
	// Parse accepts the raw spec bytes (JSON/YAML for OpenAPI, XML for WSDL,
	// JSON for Postman, plain text for curl) and returns the drafted tools.
	Parse(raw []byte) (*ParseResult, error)
}

// Registry resolves a Parser by the spec format the caller identifies
// (openapi/wsdl/postman/curl) — the caller (an import HTTP handler) already
// knows the format from a form field or file extension, so this is a plain
// map lookup, not content sniffing.
type Registry struct {
	parsers map[string]Parser
}

func NewRegistry() *Registry {
	return &Registry{
		parsers: map[string]Parser{
			"openapi": &OpenAPIParser{},
			"wsdl":    &WSDLParser{},
			"postman": &PostmanParser{},
			"curl":    &CurlParser{},
		},
	}
}

func (r *Registry) For(format string) (Parser, bool) {
	p, ok := r.parsers[format]
	return p, ok
}
