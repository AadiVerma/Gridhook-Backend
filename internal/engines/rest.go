package engines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

type RestEngine struct {
	Client *http.Client
}

func NewRestEngine() *RestEngine {
	return &RestEngine{Client: &http.Client{Timeout: 30 * time.Second}}
}

func (e *RestEngine) Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error) {
	mapping := parseRestMapping(tool.EndpointMapping)

	path, remaining, err := substitutePath(tool.Path, mapping.pathParams, input)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	body := map[string]any{}
	for key, val := range remaining {
		switch {
		case mapping.queryParams[key]:
			query.Set(key, fmt.Sprint(val))
		case mapping.bodyParams[key]:
			body[key] = val
		case tool.Method == models.MethodGET || tool.Method == models.MethodDELETE:
			query.Set(key, fmt.Sprint(val))
		default:
			body[key] = val
		}
	}

	fullURL := strings.TrimRight(api.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var reqBody io.Reader
	hasBody := tool.Method != models.MethodGET && tool.Method != models.MethodDELETE && len(body) > 0
	if hasBody {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("engines: rest: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, string(tool.Method), fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("engines: rest: build request: %w", err)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range mapping.staticHeaders {
		req.Header.Set(k, v)
	}
	for k, v := range creds.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range creds.QueryParams {
		q := req.URL.Query()
		q.Set(k, v)
		req.URL.RawQuery = q.Encode()
	}

	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engines: rest: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("engines: rest: read response: %w", err)
	}

	result := &Result{StatusCode: resp.StatusCode, Headers: flattenHeaders(resp.Header)}
	var decoded any
	if len(raw) > 0 && json.Unmarshal(raw, &decoded) == nil {
		result.Body = decoded
	} else {
		result.Body = string(raw)
	}
	return result, nil
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
	set := map[string]bool{}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			set[fmt.Sprint(item)] = true
		}
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

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k := range h {
		out[k] = h.Get(k)
	}
	return out
}
