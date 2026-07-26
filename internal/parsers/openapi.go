package parsers

import (
	"encoding/json"
	"fmt"
	"strings"

	"gridhook.dev/connector-backend/internal/models"
)

// OpenAPIParser handles OpenAPI 3.x JSON documents (YAML specs are expected
// to be converted to JSON by the upload handler before reaching here, to
// keep this package dependency-free). It walks every path/method/operation
// and drafts one tool per operation, classifying each parameter's `in`
// (path/query/header) directly into the endpoint_mapping shape RestEngine
// expects.
type OpenAPIParser struct{}

func (p *OpenAPIParser) Parse(raw []byte) (*ParseResult, error) {
	var doc struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Summary     string `json:"summary"`
			Description string `json:"description"`
			Parameters  []struct {
				Name     string         `json:"name"`
				In       string         `json:"in"`
				Required bool           `json:"required"`
				Schema   map[string]any `json:"schema"`
			} `json:"parameters"`
			RequestBody map[string]any `json:"requestBody"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsers: openapi: invalid document: %w", err)
	}

	result := &ParseResult{EngineType: models.EngineREST}
	if len(doc.Servers) > 0 {
		result.BaseURL = doc.Servers[0].URL
	}

	for path, methods := range doc.Paths {
		for method, op := range methods {
			httpMethod := models.HTTPMethod(strings.ToUpper(method))
			switch httpMethod {
			case models.MethodGET, models.MethodPOST, models.MethodPUT, models.MethodPATCH, models.MethodDELETE:
			default:
				continue // skip HEAD/OPTIONS/TRACE — not tool-worthy
			}

			name := op.OperationID
			if name == "" {
				name = strings.ToLower(method) + strings.ReplaceAll(path, "/", "_")
			}

			var pathParams, queryParams []string
			properties := map[string]any{}
			var required []string
			for _, param := range op.Parameters {
				switch param.In {
				case "path":
					pathParams = append(pathParams, param.Name)
				case "query":
					queryParams = append(queryParams, param.Name)
				}
				properties[param.Name] = param.Schema
				if param.Required {
					required = append(required, param.Name)
				}
			}

			var bodyParams []string
			if op.RequestBody != nil {
				bodyParams = []string{"body"} // whole-body pass-through; refined later by the visual tool-mapping editor
			}

			result.Tools = append(result.Tools, DraftTool{
				Name:        name,
				Method:      httpMethod,
				Path:        path,
				Description: firstNonEmpty(op.Description, op.Summary),
				Parameters: map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
				EndpointMapping: map[string]any{
					"pathParams":  toAnySlice(pathParams),
					"queryParams": toAnySlice(queryParams),
					"bodyParams":  toAnySlice(bodyParams),
				},
			})
		}
	}

	return result, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
