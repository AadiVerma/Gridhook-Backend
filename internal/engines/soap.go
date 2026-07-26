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

// SoapEngine posts an XML envelope built from the tool's endpoint_mapping:
//
//	{
//	  "soapAction": "http://example.com/GetOrder",
//	  "envelopeTemplate": "<soap:Envelope ...>...<id>{{id}}</id>...</soap:Envelope>"
//	}
//
// `envelopeTemplate` is a literal string with {{paramName}} placeholders —
// deliberately not a full templating engine: the recipe is authored once at
// tool-mapping time (by the WSDL parser or by hand) and never needs
// conditionals/loops, just substitution.
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
		Body:       string(raw), // XML is normalized further up in ResponseShaper, not here
	}, nil
}
