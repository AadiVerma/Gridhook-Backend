package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/parsers"
)

type ToolService struct {
	db *gorm.DB
}

func NewToolService(gdb *gorm.DB) *ToolService {
	return &ToolService{db: gdb}
}

func orgScopedTools(tx *gorm.DB, orgID int64) *gorm.DB {
	return tx.Where(`mcp_tools.connector_api_id IN (
		SELECT a.id FROM connector_apis a
		JOIN connectors c ON c.id = a.connector_id
		WHERE c.organization_id = ?)`, orgID)
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

func (s *ToolService) Create(ctx context.Context, orgID, connectorAPIID int64, engineType models.EngineType, in CreateToolInput) (*models.MCPTool, error) {
	if err := s.assertAPIInOrg(ctx, orgID, connectorAPIID); err != nil {
		return nil, err
	}
	return s.createTool(ctx, s.db.WithContext(ctx), connectorAPIID, engineType, in)
}

func (s *ToolService) assertAPIInOrg(ctx context.Context, orgID, connectorAPIID int64) error {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.ConnectorAPI{}).
		Where("connector_apis.id = ?", connectorAPIID).
		Where("connector_apis.connector_id IN (SELECT id FROM connectors WHERE organization_id = ?)", orgID).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("controlplane: verify connector_api ownership: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ToolService) createTool(ctx context.Context, tx *gorm.DB, connectorAPIID int64, engineType models.EngineType, in CreateToolInput) (*models.MCPTool, error) {
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
	if err := tx.WithContext(ctx).Create(t).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create tool: %w", err)
	}
	return t, nil
}

func (s *ToolService) BulkCreate(ctx context.Context, orgID, connectorAPIID int64, engineType models.EngineType, drafts []parsers.DraftTool) ([]*models.MCPTool, error) {
	if err := s.assertAPIInOrg(ctx, orgID, connectorAPIID); err != nil {
		return nil, err
	}

	out := make([]*models.MCPTool, 0, len(drafts))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, d := range drafts {
			t, err := s.createTool(ctx, tx, connectorAPIID, engineType, CreateToolInput{
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
				return fmt.Errorf("tool %q: %w", d.Name, err)
			}
			out = append(out, t)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("controlplane: bulk create tools: %w", err)
	}
	return out, nil
}

func (s *ToolService) ListByConnectorAPI(ctx context.Context, orgID, connectorAPIID int64) ([]*models.MCPTool, error) {
	var tools []*models.MCPTool
	err := orgScopedTools(s.db.WithContext(ctx).Model(&models.MCPTool{}), orgID).
		Where("connector_api_id = ?", connectorAPIID).
		Order("created_at").
		Find(&tools).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list tools: %w", err)
	}
	return tools, nil
}

func (s *ToolService) Get(ctx context.Context, orgID, id int64) (*models.MCPTool, error) {
	t := &models.MCPTool{}
	err := orgScopedTools(s.db.WithContext(ctx).Model(&models.MCPTool{}), orgID).
		Where("mcp_tools.id = ?", id).
		First(t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: get tool: %w", err)
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

func (s *ToolService) Update(ctx context.Context, orgID, id int64, in UpdateToolInput) (*models.MCPTool, error) {
	existing, err := s.Get(ctx, orgID, id)
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
		return existing, nil
	}
	updates["updated_at"] = gorm.Expr("now()")

	res := orgScopedTools(s.db.WithContext(ctx).Model(&models.MCPTool{}), orgID).
		Where("mcp_tools.id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return nil, fmt.Errorf("controlplane: update tool: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, orgID, id)
}

func (s *ToolService) Delete(ctx context.Context, orgID, id int64) error {
	tx := orgScopedTools(s.db.WithContext(ctx).Model(&models.MCPTool{}), orgID).
		Where("mcp_tools.id = ?", id).
		Delete(&models.MCPTool{})
	if tx.Error != nil {
		return fmt.Errorf("controlplane: delete tool: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

const resolveForServerSQL = `
	SELECT t.id, t.connector_api_id, t.group_id, t.engine_type, t.name, t.method, t.path,
	       t.description, t.parameters, t.endpoint_mapping, t.response_mapping, t.output_schema,
	       t.cached, t.cache_ttl_seconds, t.status, t.version, coalesce(t.display_title,'') AS display_title,
	       t.display_on_frontend, t.created_at, t.updated_at,
	       a.id AS api_id, a.connector_id AS api_connector_id, a.name AS api_name, a.engine_type AS api_engine_type,
	       a.base_url AS api_base_url, a.auth_type AS api_auth_type, coalesce(a.spec_url,'') AS api_spec_url,
	       a.is_active AS api_is_active, a.created_at AS api_created_at, a.updated_at AS api_updated_at
	FROM mcp_tools t
	JOIN connector_apis a ON a.id = t.connector_api_id
	JOIN mcp_server_tool_groups g ON g.tool_group_id = t.group_id AND g.mcp_server_id = ?
	JOIN mcp_servers s ON s.id = g.mcp_server_id AND s.organization_id = ?
	WHERE t.name = ? AND t.status = 'active'`

func (s *ToolService) ResolveForServer(ctx context.Context, orgID, mcpServerID int64, toolName string) (*models.ResolvedTool, error) {
	t := &models.MCPTool{}
	a := &models.ConnectorAPI{}

	row := s.db.WithContext(ctx).Raw(resolveForServerSQL, mcpServerID, orgID, toolName).Row()
	err := row.Scan(
		&t.ID, &t.ConnectorAPIID, &t.GroupID, &t.EngineType, &t.Name, &t.Method, &t.Path,
		&t.Description, &t.Parameters, &t.EndpointMapping, &t.ResponseMapping, &t.OutputSchema,
		&t.Cached, &t.CacheTTLSeconds, &t.Status, &t.Version, &t.DisplayTitle, &t.DisplayOnFrontend,
		&t.CreatedAt, &t.UpdatedAt,
		&a.ID, &a.ConnectorID, &a.Name, &a.EngineType, &a.BaseURL, &a.AuthType, &a.SpecURL,
		&a.IsActive, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("controlplane: tool %q is not exposed by this server: %w", toolName, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: resolve tool: %w", err)
	}
	return &models.ResolvedTool{Tool: t, API: a}, nil
}

func (s *ToolService) ListForServer(ctx context.Context, orgID, mcpServerID int64) ([]*models.MCPTool, error) {
	var tools []*models.MCPTool
	err := s.db.WithContext(ctx).Model(&models.MCPTool{}).
		Joins("JOIN mcp_server_tool_groups g ON g.tool_group_id = mcp_tools.group_id AND g.mcp_server_id = ?", mcpServerID).
		Joins("JOIN mcp_servers s ON s.id = g.mcp_server_id AND s.organization_id = ?", orgID).
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
