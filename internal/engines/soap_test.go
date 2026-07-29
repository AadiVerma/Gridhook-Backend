package engines

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

func TestSoapEngine_Execute_PlaceholderSyntax(t *testing.T) {
	var capturedBody string
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		capturedBody = string(raw)
		capturedHeaders = r.Header
		w.Write([]byte(`<Envelope><Body><ok/></Body></Envelope>`))
	}))
	defer server.Close()

	api := &models.ConnectorAPI{BaseURL: server.URL}
	tool := &models.MCPTool{
		Name: "test",
		EndpointMapping: map[string]any{
			"envelopeTemplate": `<Envelope><Body><double>{{doubleParam}}</double><single>{singleParam}</single></Body></Envelope>`,
			"soapAction":       "urn:test/action",
			"headers":          map[string]any{"Accept": "text/xml"},
		},
	}

	_, err := NewSoapEngine().Execute(context.Background(), api, tool, schemes.Credentials{Headers: map[string]string{"Authorization": "Bearer tok"}}, map[string]any{
		"doubleParam": "d-value",
		"singleParam": "s-value",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(capturedBody, "<double>d-value</double>") {
		t.Errorf("double-brace placeholder not substituted: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "<single>s-value</single>") {
		t.Errorf("single-brace placeholder not substituted: %s", capturedBody)
	}

	if capturedHeaders.Get("SOAPAction") != "urn:test/action" {
		t.Errorf("SOAPAction = %q", capturedHeaders.Get("SOAPAction"))
	}
	if capturedHeaders.Get("Content-Type") != "text/xml; charset=utf-8" {
		t.Errorf("Content-Type = %q", capturedHeaders.Get("Content-Type"))
	}
	if capturedHeaders.Get("Accept") != "text/xml" {
		t.Errorf("Accept header from endpoint_mapping.headers missing: %q", capturedHeaders.Get("Accept"))
	}
	if capturedHeaders.Get("Authorization") != "Bearer tok" {
		t.Errorf("Authorization header from credentials missing: %q", capturedHeaders.Get("Authorization"))
	}
}

func TestSoapEngine_Execute_NoEnvelopeTemplate(t *testing.T) {
	tool := &models.MCPTool{Name: "broken", EndpointMapping: map[string]any{}}
	_, err := NewSoapEngine().Execute(context.Background(), &models.ConnectorAPI{}, tool, schemes.Credentials{}, nil)
	if err == nil {
		t.Fatal("expected error for missing envelopeTemplate, got nil")
	}
}
