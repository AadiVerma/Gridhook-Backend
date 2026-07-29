package engines

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

type SoapEngine struct {
	client *httpx.Client
}

func NewSoapEngine(client *httpx.Client) *SoapEngine {
	return &SoapEngine{client: client}
}

var _ Engine = (*SoapEngine)(nil)

func (e *SoapEngine) Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error) {
	template, _ := tool.EndpointMapping["envelopeTemplate"].(string)
	if template == "" {
		return nil, fmt.Errorf("engines: soap: tool %q has no envelopeTemplate", tool.Name)
	}
	soapAction, _ := tool.EndpointMapping["soapAction"].(string)

	envelope, err := renderEnvelope(template, input)
	if err != nil {
		return nil, err
	}

	req, err := httpx.NewRequest(ctx, http.MethodPost, api.BaseURL, []byte(envelope))
	if err != nil {
		return nil, fmt.Errorf("engines: soap: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	if soapAction != "" {
		req.Header.Set("SOAPAction", soapAction)
	}
	for k, v := range staticHeadersFrom(tool.EndpointMapping) {
		req.Header.Set(k, v)
	}
	applyCredentials(req, creds)

	resp, err := e.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("engines: soap: %w", err)
	}
	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),

		Body: string(resp.Body),
	}, nil
}

func renderEnvelope(template string, input map[string]any) (string, error) {
	envelope := template
	for key, val := range input {
		var escaped strings.Builder
		if err := xml.EscapeText(&escaped, []byte(fmt.Sprint(val))); err != nil {
			return "", fmt.Errorf("engines: soap: escape param %q: %w", key, err)
		}
		value := escaped.String()

		envelope = strings.ReplaceAll(envelope, "{{"+key+"}}", value)
		envelope = strings.ReplaceAll(envelope, "{"+key+"}", value)
	}
	return envelope, nil
}
