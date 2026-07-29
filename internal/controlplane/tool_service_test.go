package controlplane

import (
	"errors"
	"testing"

	"gridhook.dev/connector-backend/internal/models"
)

func TestResolveToolConfig_RestDefaultsEmptyParameters(t *testing.T) {
	params, _, err := resolveToolConfig(models.EngineREST, map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params["type"] != "object" {
		t.Errorf("expected default JSON Schema object, got %#v", params)
	}
}

func TestResolveToolConfig_SoapMissingEnvelopeErrors(t *testing.T) {
	_, _, err := resolveToolConfig(models.EngineSOAP, map[string]any{}, map[string]any{})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestResolveToolConfig_SoapWithExistingEnvelopeIsUntouched(t *testing.T) {
	mapping := map[string]any{"envelopeTemplate": "<Envelope><x>{{foo}}</x></Envelope>"}
	params, resolved, err := resolveToolConfig(models.EngineSOAP, map[string]any{
		"type":       "object",
		"properties": map[string]any{"foo": map[string]any{"type": "string"}},
	}, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["envelopeTemplate"] != mapping["envelopeTemplate"] {
		t.Errorf("envelopeTemplate should be left untouched: %#v", resolved)
	}
	props, _ := params["properties"].(map[string]any)
	if _, ok := props["foo"]; !ok {
		t.Errorf("existing hand-authored parameters should not be discarded: %#v", params)
	}
}

// TestResolveToolConfig_MigratesRawSoapPayload mirrors the real broken tool
// row this was built for: a client PATCHed parameters={body, headers} with
// nothing in endpointMapping. The fix must migrate this instead of rejecting
// it — the caller shouldn't have to hand-author a JSON Schema themselves.
func TestResolveToolConfig_MigratesRawSoapPayload(t *testing.T) {
	body := `<soapenv:Envelope><urn:OnBehalfUserId>{USER_ID}</urn:OnBehalfUserId><urn:TemplateId>{{TEMPLATE_ID}}</urn:TemplateId></soapenv:Envelope>`
	parameters := map[string]any{
		"body": body,
		"headers": map[string]any{
			"Accept":       map[string]any{"type": "string", "required": true, "description": "text/xml"},
			"Content-Type": "text/xml; charset=utf-8",
		},
	}

	resolvedParams, resolvedMapping, err := resolveToolConfig(models.EngineSOAP, parameters, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolvedMapping["envelopeTemplate"] != body {
		t.Errorf("envelopeTemplate not migrated from parameters.body: %#v", resolvedMapping)
	}

	headers, _ := resolvedMapping["headers"].(map[string]any)
	if headers["Accept"] != "text/xml" {
		t.Errorf("Accept header value should be pulled from the description field, got %#v", headers["Accept"])
	}
	if headers["Content-Type"] != "text/xml; charset=utf-8" {
		t.Errorf("Content-Type header not migrated: %#v", headers)
	}

	if resolvedParams["type"] != "object" {
		t.Fatalf("derived parameters should be a JSON Schema object: %#v", resolvedParams)
	}
	props, _ := resolvedParams["properties"].(map[string]any)
	if _, ok := props["USER_ID"]; !ok {
		t.Errorf("USER_ID placeholder not captured in derived schema: %#v", props)
	}
	if _, ok := props["TEMPLATE_ID"]; !ok {
		t.Errorf("TEMPLATE_ID placeholder not captured in derived schema: %#v", props)
	}
}

func TestResolveToolConfig_GraphqlMissingQueryErrors(t *testing.T) {
	_, _, err := resolveToolConfig(models.EngineGraphQL, map[string]any{}, map[string]any{})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestResolveToolConfig_MigratesRawGraphqlPayload(t *testing.T) {
	parameters := map[string]any{
		"query":     "query getUser($id: ID!) { getUser(id: $id) { id name } }",
		"variables": map[string]any{"id": "abc-123"},
	}

	resolvedParams, resolvedMapping, err := resolveToolConfig(models.EngineGraphQL, parameters, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedMapping["query"] != parameters["query"] {
		t.Errorf("query not migrated from parameters.query: %#v", resolvedMapping)
	}
	props, _ := resolvedParams["properties"].(map[string]any)
	idProp, _ := props["id"].(map[string]any)
	if idProp["type"] != "string" {
		t.Errorf("id variable should be inferred as string: %#v", props)
	}
}
