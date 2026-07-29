package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/parsers"
)

type ToolService struct {
	db *gorm.DB
}

func NewToolService(gdb *gorm.DB) *ToolService {
	return &ToolService{db: gdb}
}

type CreateToolInput struct {
	Name            string
	Method          models.HTTPMethod
	Path            string
	Description     string
	Parameters      map[string]any
	EndpointMapping map[string]any
	ResponseMapping map[string]any
	OutputSchema    map[string]any
	Cached          bool
	CacheTTLSeconds int
	GroupID         *int64
}

func (s *ToolService) Create(ctx context.Context, connectorAPIID int64, engineType models.EngineType, in CreateToolInput) (*models.MCPTool, error) {
	parameters, endpointMapping, err := resolveToolConfig(engineType, orEmpty(in.Parameters), orEmpty(in.EndpointMapping))
	if err != nil {
		return nil, err
	}

	method := in.Method
	if method == "" && engineType == models.EngineSOAP {
		method = models.MethodPOST
	}
	t := &models.MCPTool{
		ConnectorAPIID:    connectorAPIID,
		EngineType:        engineType,
		Name:              in.Name,
		Method:            method,
		Path:              in.Path,
		Description:       in.Description,
		Parameters:        parameters,
		EndpointMapping:   endpointMapping,
		ResponseMapping:   orEmpty(in.ResponseMapping),
		OutputSchema:      orEmpty(in.OutputSchema),
		Cached:            in.Cached,
		CacheTTLSeconds:   in.CacheTTLSeconds,
		Status:            models.ToolActive,
		Version:           "1",
		DisplayOnFrontend: true,
		GroupID:           in.GroupID,
	}
	if err := s.db.WithContext(ctx).Create(t).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create tool: %w", err)
	}
	return t, nil
}

// resolveToolConfig is the one place a tool's parameters/endpointMapping get
// settled before they're persisted. It never rejects a caller for handing
// over native protocol content instead of a pre-shaped JSON Schema — that
// would just push the JSON conversion work back onto the user, which is the
// engine's job. Instead it migrates recognizable native input (a SOAP
// envelope pasted into parameters.body, a GraphQL query pasted into
// parameters.query) into endpointMapping, derives a best-effort JSON Schema
// from whatever placeholders/variables it finds, and only errors when there
// is truly nothing usable to build a request from.
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

// migrateNativeInput relocates a raw protocol payload the caller put under
// "parameters" (because there was nowhere else obvious to put it) into
// endpointMapping, where the engines actually read it from. Existing,
// already-correct endpointMapping content is never overwritten.
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

// flattenHeaderValues coerces whatever shape a caller used to describe
// headers into a flat name->value map, since that's the only shape the
// engines' staticHeadersFrom reads. Plain string values pass through; a
// schema-like descriptor (e.g. {"type":"string","description":"text/xml"})
// contributes whichever of its fields actually looks like the header's value.
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

func (s *ToolService) BulkCreate(ctx context.Context, connectorAPIID int64, engineType models.EngineType, drafts []parsers.DraftTool) ([]*models.MCPTool, error) {
	out := make([]*models.MCPTool, 0, len(drafts))
	for _, d := range drafts {
		t, err := s.Create(ctx, connectorAPIID, engineType, CreateToolInput{
			Name:            d.Name,
			Method:          d.Method,
			Path:            d.Path,
			Description:     d.Description,
			Parameters:      d.Parameters,
			EndpointMapping: d.EndpointMapping,
			ResponseMapping: d.ResponseMapping,
			OutputSchema:    d.OutputSchema,
		})
		if err != nil {
			return out, fmt.Errorf("controlplane: bulk create tool %q: %w", d.Name, err)
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *ToolService) ListByConnectorAPI(ctx context.Context, connectorAPIID int64) ([]*models.MCPTool, error) {
	var tools []*models.MCPTool
	if err := s.db.WithContext(ctx).Where("connector_api_id = ?", connectorAPIID).Order("created_at").Find(&tools).Error; err != nil {
		return nil, fmt.Errorf("controlplane: list tools: %w", err)
	}
	return tools, nil
}

func (s *ToolService) Get(ctx context.Context, id int64) (*models.MCPTool, error) {
	t := &models.MCPTool{}
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

type UpdateToolInput struct {
	Name            *string
	Method          *models.HTTPMethod
	Path            *string
	Description     *string
	Parameters      map[string]any
	EndpointMapping map[string]any
	ResponseMapping map[string]any
	OutputSchema    map[string]any
	Cached          *bool
	CacheTTLSeconds *int
	GroupID         **int64
}

func (s *ToolService) Update(ctx context.Context, id int64, in UpdateToolInput) (*models.MCPTool, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	effectiveParameters := map[string]any(existing.Parameters)
	if in.Parameters != nil {
		effectiveParameters = orEmpty(in.Parameters)
	}
	effectiveEndpointMapping := map[string]any(existing.EndpointMapping)
	if in.EndpointMapping != nil {
		effectiveEndpointMapping = orEmpty(in.EndpointMapping)
	}

	updates := map[string]any{}
	if in.Parameters != nil || in.EndpointMapping != nil {
		resolvedParameters, resolvedEndpointMapping, err := resolveToolConfig(existing.EngineType, effectiveParameters, effectiveEndpointMapping)
		if err != nil {
			return nil, err
		}
		updates["parameters"] = resolvedParameters
		updates["endpoint_mapping"] = resolvedEndpointMapping
	}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Method != nil {
		updates["method"] = *in.Method
	}
	if in.Path != nil {
		updates["path"] = *in.Path
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if in.ResponseMapping != nil {
		updates["response_mapping"] = orEmpty(in.ResponseMapping)
	}
	if in.OutputSchema != nil {
		updates["output_schema"] = orEmpty(in.OutputSchema)
	}
	if in.Cached != nil {
		updates["cached"] = *in.Cached
	}
	if in.CacheTTLSeconds != nil {
		updates["cache_ttl_seconds"] = *in.CacheTTLSeconds
	}
	if in.GroupID != nil {
		updates["group_id"] = *in.GroupID
	}
	if len(updates) == 0 {
		return s.Get(ctx, id)
	}
	updates["updated_at"] = gorm.Expr("now()")

	res := s.db.WithContext(ctx).Model(&models.MCPTool{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, fmt.Errorf("controlplane: update tool: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *ToolService) Delete(ctx context.Context, id int64) error {
	tx := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.MCPTool{})
	if tx.Error != nil {
		return fmt.Errorf("controlplane: delete tool: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ToolService) ResolveForServer(ctx context.Context, mcpServerID int64, toolName string) (*dispatcher.ToolLookup, error) {
	t := &models.MCPTool{}
	a := &models.ConnectorAPI{}
	row := s.db.WithContext(ctx).Raw(`
		SELECT t.id, t.connector_api_id, t.group_id, t.engine_type, t.name, t.method, t.path,
		       t.description, t.parameters, t.endpoint_mapping, t.response_mapping, t.output_schema,
		       t.cached, t.cache_ttl_seconds, t.status, t.version, coalesce(t.display_title,'') AS display_title, t.display_on_frontend,
		       t.created_at, t.updated_at,
		       a.id AS api_id, a.connector_id AS api_connector_id, a.name AS api_name, a.engine_type AS api_engine_type,
		       a.base_url AS api_base_url, a.auth_type AS api_auth_type, coalesce(a.spec_url,'') AS api_spec_url,
		       a.is_active AS api_is_active, a.created_at AS api_created_at, a.updated_at AS api_updated_at
		FROM mcp_tools t
		JOIN connector_apis a ON a.id = t.connector_api_id
		JOIN mcp_server_tool_groups g ON g.tool_group_id = t.group_id AND g.mcp_server_id = ?
		WHERE t.name = ? AND t.status = 'active'
	`, mcpServerID, toolName).Row()

	err := row.Scan(
		&t.ID, &t.ConnectorAPIID, &t.GroupID, &t.EngineType, &t.Name, &t.Method, &t.Path,
		&t.Description, &t.Parameters, &t.EndpointMapping, &t.ResponseMapping, &t.OutputSchema,
		&t.Cached, &t.CacheTTLSeconds, &t.Status, &t.Version, &t.DisplayTitle, &t.DisplayOnFrontend,
		&t.CreatedAt, &t.UpdatedAt,
		&a.ID, &a.ConnectorID, &a.Name, &a.EngineType, &a.BaseURL, &a.AuthType, &a.SpecURL,
		&a.IsActive, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("controlplane: tool %q not found or not assigned to this server", toolName)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: resolve tool: %w", err)
	}
	return &dispatcher.ToolLookup{Tool: t, API: a, ConnectorID: a.ConnectorID}, nil
}

func (s *ToolService) ListForServer(ctx context.Context, mcpServerID int64) ([]*models.MCPTool, error) {
	var tools []*models.MCPTool
	err := s.db.WithContext(ctx).
		Joins("JOIN mcp_server_tool_groups g ON g.tool_group_id = mcp_tools.group_id AND g.mcp_server_id = ?", mcpServerID).
		Where("mcp_tools.status = ?", models.ToolActive).
		Order("mcp_tools.name").
		Find(&tools).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list tools for server: %w", err)
	}
	return tools, nil
}

func orEmpty(m map[string]any) models.JSONMap {
	if m == nil {
		return models.JSONMap{}
	}
	return m
}
