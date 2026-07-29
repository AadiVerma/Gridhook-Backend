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

type GraphQLEngine struct {
	client *httpx.Client
}

func NewGraphQLEngine(client *httpx.Client) *GraphQLEngine {
	return &GraphQLEngine{client: client}
}

var _ Engine = (*GraphQLEngine)(nil)

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

	payload, err := json.Marshal(graphqlRequestBody{
		Query:         query,
		OperationName: operationName,
		Variables:     input,
	})
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: marshal request: %w", err)
	}

	req, err := httpx.NewRequest(ctx, http.MethodPost, api.BaseURL, payload)
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range staticHeadersFrom(tool.EndpointMapping) {
		req.Header.Set(k, v)
	}
	applyCredentials(req, creds)

	resp, err := e.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("engines: graphql: %w", err)
	}
	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
		Body:       decodeBody(resp.Body),
	}, nil
}
