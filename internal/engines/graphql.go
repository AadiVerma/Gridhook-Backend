package engines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

type GraphQLEngine struct {
	Client *http.Client
}

func NewGraphQLEngine() *GraphQLEngine {
	return &GraphQLEngine{Client: &http.Client{Timeout: 30 * time.Second}}
}

type graphqlRequestBody struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName,omitempty"`
	Variables     map[string]any `json:"variables,omitempty"`
}

func (e *GraphQLEngine) Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error) {
	query, _ := tool.EndpointMapping["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("engines: graphql: tool %q has no query", tool.Name)
	}
	operationName, _ := tool.EndpointMapping["operationName"].(string)

	payload, err := json.Marshal(graphqlRequestBody{Query: query, OperationName: operationName, Variables: input})
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range staticHeadersFrom(tool.EndpointMapping) {
		req.Header.Set(k, v)
	}
	for k, v := range creds.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: read response: %w", err)
	}

	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		decoded = string(raw)
	}

	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
		Body:       decoded,
	}, nil
}
