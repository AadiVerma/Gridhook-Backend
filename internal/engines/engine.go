package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/httpx"
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

func NewRegistry(client *httpx.Client) *Registry {
	return &Registry{
		engines: map[models.EngineType]Engine{
			models.EngineREST:    NewRestEngine(client),
			models.EngineSOAP:    NewSoapEngine(client),
			models.EngineGraphQL: NewGraphQLEngine(client),
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

func staticHeadersFrom(mapping map[string]any) map[string]string {
	out := map[string]string{}
	if headers, ok := mapping["headers"].(map[string]any); ok {
		for k, v := range headers {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

func applyCredentials(req *http.Request, creds schemes.Credentials) {
	if len(creds.Headers) > 0 {
		names := make([]string, 0, len(creds.Headers))
		for name, value := range creds.Headers {
			req.Header.Set(name, value)
			names = append(names, name)
		}
		httpx.MarkSensitive(req.Header, names...)
	}
	if len(creds.QueryParams) > 0 {
		query := req.URL.Query()
		for name, value := range creds.QueryParams {
			query.Set(name, value)
		}
		req.URL.RawQuery = query.Encode()
	}
}

func decodeBody(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return string(raw)
	}
	return decoded
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k := range h {
		out[k] = h.Get(k)
	}
	return out
}
