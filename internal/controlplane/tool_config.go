package controlplane

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"gridhook.dev/connector-backend/internal/models"
)

func resolveToolConfig(engineType models.EngineType, parameters, endpointMapping map[string]any) (map[string]any, map[string]any, error) {
	parameters, endpointMapping = migrateNativeInput(engineType, parameters, endpointMapping)

	if _, hasType := parameters["type"]; !hasType {
		parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}

	switch engineType {
	case models.EngineSOAP:
		template, _ := endpointMapping["envelopeTemplate"].(string)
		if strings.TrimSpace(template) == "" {
			return nil, nil, fmt.Errorf("%w: SOAP tools need a SOAP envelope to call — set endpointMapping.envelopeTemplate, or put it in parameters.body and it will be migrated automatically", ErrValidation)
		}
	case models.EngineGraphQL:
		query, _ := endpointMapping["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, nil, fmt.Errorf("%w: GraphQL tools need a query to call — set endpointMapping.query, or put it in parameters.query and it will be migrated automatically", ErrValidation)
		}
	}
	return parameters, endpointMapping, nil
}

var placeholderPattern = regexp.MustCompile(`\{\{?([A-Za-z_][A-Za-z0-9_]*)\}\}?`)

func migrateNativeInput(engineType models.EngineType, parameters, endpointMapping map[string]any) (map[string]any, map[string]any) {
	original := parameters
	endpointMapping = cloneMap(endpointMapping)

	switch engineType {
	case models.EngineSOAP:
		if _, ok := endpointMapping["headers"]; !ok {
			if headers, ok := original["headers"].(map[string]any); ok {
				endpointMapping["headers"] = flattenHeaderValues(headers)
			}
		}
		if template, _ := endpointMapping["envelopeTemplate"].(string); strings.TrimSpace(template) == "" {
			if body, ok := original["body"].(string); ok && strings.TrimSpace(body) != "" {
				endpointMapping["envelopeTemplate"] = body
				parameters = schemaFromPlaceholders(extractPlaceholders(body))
			}
		}
	case models.EngineGraphQL:
		if query, _ := endpointMapping["query"].(string); strings.TrimSpace(query) == "" {
			if q, ok := original["query"].(string); ok && strings.TrimSpace(q) != "" {
				endpointMapping["query"] = q
				if opName, ok := original["operationName"].(string); ok {
					endpointMapping["operationName"] = opName
				}
				if vars, ok := original["variables"].(map[string]any); ok {
					parameters = schemaFromExampleValues(vars)
				} else {
					parameters = map[string]any{"type": "object", "properties": map[string]any{}}
				}
			}
		}
	}
	return parameters, endpointMapping
}

func extractPlaceholders(body string) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range placeholderPattern.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	return names
}

func schemaFromPlaceholders(names []string) map[string]any {
	properties := map[string]any{}
	required := make([]any, 0, len(names))
	for _, name := range names {
		properties[name] = map[string]any{"type": "string"}
		required = append(required, name)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func schemaFromExampleValues(vars map[string]any) map[string]any {
	properties := map[string]any{}
	for k, v := range vars {
		properties[k] = map[string]any{"type": jsonTypeOfValue(v)}
	}
	return map[string]any{"type": "object", "properties": properties}
}

func jsonTypeOfValue(v any) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case float64:
		return "number"
	default:
		return "string"
	}
}

func flattenHeaderValues(headers map[string]any) map[string]any {
	out := make(map[string]any, len(headers))
	for name, v := range headers {
		switch val := v.(type) {
		case string:
			out[name] = val
		case map[string]any:
			for _, key := range []string{"value", "default", "example", "description"} {
				if s, ok := val[key].(string); ok {
					out[name] = s
					break
				}
			}
			if _, ok := out[name]; !ok {
				out[name] = fmt.Sprint(v)
			}
		default:
			out[name] = fmt.Sprint(v)
		}
	}
	return out
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}
