package engines

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

type SoapEngine struct {
	Client *http.Client
}

func NewSoapEngine() *SoapEngine {
	return &SoapEngine{Client: &http.Client{Timeout: 30 * time.Second}}
}

func (e *SoapEngine) Execute(ctx context.Context, api *models.ConnectorAPI, tool *models.MCPTool, creds schemes.Credentials, input map[string]any) (*Result, error) {
	template, _ := tool.EndpointMapping["envelopeTemplate"].(string)
	if template == "" {
		return nil, fmt.Errorf("engines: soap: tool %q has no envelopeTemplate", tool.Name)
	}
	soapAction, _ := tool.EndpointMapping["soapAction"].(string)

	envelope := template
	for key, val := range input {
		placeholder := "{{" + key + "}}"
		var escaped strings.Builder
		if err := xml.EscapeText(&escaped, []byte(fmt.Sprint(val))); err != nil {
			return nil, fmt.Errorf("engines: soap: escape param %q: %w", key, err)
		}
		envelope = strings.ReplaceAll(envelope, placeholder, escaped.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api.BaseURL, strings.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("engines: soap: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	if soapAction != "" {
		req.Header.Set("SOAPAction", soapAction)
	}
	for k, v := range creds.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engines: soap: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("engines: soap: read response: %w", err)
	}

	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
		Body:       string(raw),
	}, nil
}
