package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

type RestEngine struct {
	client *httpx.Client
}

func NewRestEngine(client *httpx.Client) *RestEngine {
	return &RestEngine{client: client}
}

var _ Engine = (*RestEngine)(nil)

func (e *RestEngine) Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error) {
	mapping := parseRestMapping(tool.EndpointMapping)

	path, remaining, err := substitutePath(tool.Path, mapping.pathParams, input)
	if err != nil {
		return nil, err
	}

	query, body := partitionParams(tool.Method, mapping, remaining)

	endpoint, err := buildURL(api.BaseURL, path, query)
	if err != nil {
		return nil, err
	}

	var payload []byte
	hasBody := methodAcceptsBody(tool.Method) && len(body) > 0
	if hasBody {
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("engines: rest: marshal body: %w", err)
		}
	}

	req, err := httpx.NewRequest(ctx, string(tool.Method), endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("engines: rest: build request: %w", err)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range mapping.staticHeaders {
		req.Header.Set(k, v)
	}
	applyCredentials(req, creds)

	resp, err := e.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("engines: rest: %w", err)
	}
	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
		Body:       decodeBody(resp.Body),
	}, nil
}

func buildURL(baseURL, path string, query url.Values) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("engines: rest: invalid base URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", fmt.Errorf("engines: rest: base URL scheme must be http or https, got %q", base.Scheme)
	}

	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")

	if len(query) > 0 {
		existing := base.Query()
		for key, values := range query {
			for _, v := range values {
				existing.Set(key, v)
			}
		}
		base.RawQuery = existing.Encode()
	}
	return base.String(), nil
}

func partitionParams(method models.HTTPMethod, mapping restMapping, remaining map[string]any) (url.Values, map[string]any) {
	query := url.Values{}
	body := make(map[string]any, len(remaining))
	for key, val := range remaining {
		switch {
		case mapping.queryParams[key]:
			query.Set(key, fmt.Sprint(val))
		case mapping.bodyParams[key]:
			body[key] = val
		case !methodAcceptsBody(method):
			query.Set(key, fmt.Sprint(val))
		default:
			body[key] = val
		}
	}
	return query, body
}

func methodAcceptsBody(method models.HTTPMethod) bool {
	switch method {
	case models.MethodGET, models.MethodDELETE:
		return false
	default:
		return true
	}
}

type restMapping struct {
	pathParams    map[string]bool
	queryParams   map[string]bool
	bodyParams    map[string]bool
	staticHeaders map[string]string
}

func parseRestMapping(raw map[string]any) restMapping {
	return restMapping{
		pathParams:    toSet(raw["pathParams"]),
		queryParams:   toSet(raw["queryParams"]),
		bodyParams:    toSet(raw["bodyParams"]),
		staticHeaders: staticHeadersFrom(raw),
	}
}

func toSet(v any) map[string]bool {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	set := make(map[string]bool, len(list))
	for _, item := range list {
		set[fmt.Sprint(item)] = true
	}
	return set
}

func substitutePath(pathTemplate string, pathParams map[string]bool, input map[string]any) (string, map[string]any, error) {
	remaining := make(map[string]any, len(input))
	for k, v := range input {
		remaining[k] = v
	}

	path := pathTemplate
	for name := range pathParams {
		placeholder := "{" + name + "}"
		if !strings.Contains(path, placeholder) {
			continue
		}
		val, ok := remaining[name]
		if !ok {
			return "", nil, fmt.Errorf("engines: rest: missing required path parameter %q", name)
		}
		path = strings.ReplaceAll(path, placeholder, url.PathEscape(fmt.Sprint(val)))
		delete(remaining, name)
	}
	return path, remaining, nil
}
