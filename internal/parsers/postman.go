package parsers

import (
	"encoding/json"
	"fmt"
	"strings"

	"gridhook.dev/connector-backend/internal/models"
)

// PostmanParser drafts one tool per request in a Postman v2.1 collection.
// Postman requests are already concrete (a literal method/URL/body), so —
// unlike OpenAPI — there's no parameter/schema metadata to recover; path
// variables ({{var}} or :var) become pathParams and the rest of the query
// string becomes queryParams, matching RestEngine's mapping shape.
type PostmanParser struct{}

type postmanCollection struct {
	Item []postmanItem `json:"item"`
}

type postmanItem struct {
	Name    string        `json:"name"`
	Item    []postmanItem `json:"item,omitempty"` // folders nest more items
	Request *struct {
		Method string `json:"method"`
		URL    struct {
			Raw   string `json:"raw"`
			Query []struct {
				Key string `json:"key"`
			} `json:"query"`
		} `json:"url"`
	} `json:"request"`
}

func (p *PostmanParser) Parse(raw []byte) (*ParseResult, error) {
	var col postmanCollection
	if err := json.Unmarshal(raw, &col); err != nil {
		return nil, fmt.Errorf("parsers: postman: invalid collection: %w", err)
	}

	result := &ParseResult{EngineType: models.EngineREST}
	var walk func(items []postmanItem)
	walk = func(items []postmanItem) {
		for _, item := range items {
			if len(item.Item) > 0 {
				walk(item.Item)
				continue
			}
			if item.Request == nil {
				continue
			}
			var queryParams []any
			for _, q := range item.Request.URL.Query {
				queryParams = append(queryParams, q.Key)
			}
			result.Tools = append(result.Tools, DraftTool{
				Name:   sanitizeName(item.Name),
				Method: models.HTTPMethod(strings.ToUpper(item.Request.Method)),
				Path:   item.Request.URL.Raw,
				EndpointMapping: map[string]any{
					"queryParams": queryParams,
				},
			})
		}
	}
	walk(col.Item)
	return result, nil
}

func sanitizeName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "_"))
}
