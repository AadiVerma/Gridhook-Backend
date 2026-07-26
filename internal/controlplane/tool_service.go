package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
	GroupID         int64
}

func (s *ToolService) Create(ctx context.Context, connectorAPIID int64, engineType models.EngineType, in CreateToolInput) (*models.MCPTool, error) {
	t := &models.MCPTool{
		ConnectorAPIID:    connectorAPIID,
		EngineType:        engineType,
		Name:              in.Name,
		Method:            in.Method,
		Path:              in.Path,
		Description:       in.Description,
		Parameters:        orEmpty(in.Parameters),
		EndpointMapping:   orEmpty(in.EndpointMapping),
		ResponseMapping:   orEmpty(in.ResponseMapping),
		OutputSchema:      orEmpty(in.OutputSchema),
		Cached:            in.Cached,
		CacheTTLSeconds:   in.CacheTTLSeconds,
		Status:            models.ToolActive,
		Version:           "1",
		DisplayOnFrontend: true,
	}
	if err := s.db.WithContext(ctx).Omit("GroupID").Create(t).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create tool: %w", err)
	}
	if in.GroupID != 0 {
		if err := s.db.WithContext(ctx).Exec(`UPDATE mcp_tools SET group_id = ? WHERE id = ?`, in.GroupID, t.ID).Error; err != nil {
			return nil, fmt.Errorf("controlplane: create tool: set group: %w", err)
		}
		t.GroupID = in.GroupID
	}
	return t, nil
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

func (s *ToolService) withGroupID(ctx context.Context, tools []*models.MCPTool) error {
	if len(tools) == 0 {
		return nil
	}
	ids := make([]int64, len(tools))
	for i, t := range tools {
		ids[i] = t.ID
	}
	var rows []struct {
		ID      int64
		GroupID int64
	}
	err := s.db.WithContext(ctx).Raw(
		`SELECT id, coalesce(group_id,0) AS group_id FROM mcp_tools WHERE id = ANY(?)`, ids,
	).Scan(&rows).Error
	if err != nil {
		return err
	}
	byID := make(map[int64]int64, len(rows))
	for _, r := range rows {
		byID[r.ID] = r.GroupID
	}
	for _, t := range tools {
		t.GroupID = byID[t.ID]
	}
	return nil
}

func (s *ToolService) ListByConnectorAPI(ctx context.Context, connectorAPIID int64) ([]*models.MCPTool, error) {
	var tools []*models.MCPTool
	if err := s.db.WithContext(ctx).Where("connector_api_id = ?", connectorAPIID).Order("created_at").Find(&tools).Error; err != nil {
		return nil, fmt.Errorf("controlplane: list tools: %w", err)
	}
	if err := s.withGroupID(ctx, tools); err != nil {
		return nil, fmt.Errorf("controlplane: list tools: group ids: %w", err)
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
	if err := s.withGroupID(ctx, []*models.MCPTool{t}); err != nil {
		return nil, err
	}
	return t, nil
}

type UpdateToolInput struct {
	Name            *string
	Method          *models.HTTPMethod
	Path            *string
	CacheTTLSeconds *int
	GroupID         *int64
}

func (s *ToolService) Update(ctx context.Context, id int64, in UpdateToolInput) (*models.MCPTool, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Method != nil {
		updates["method"] = *in.Method
	}
	if in.Path != nil {
		updates["path"] = *in.Path
	}
	if in.CacheTTLSeconds != nil {
		updates["cache_ttl_seconds"] = *in.CacheTTLSeconds
	}
	if len(updates) > 0 {
		updates["updated_at"] = gorm.Expr("now()")
	}

	var affected int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			res := tx.Model(&models.MCPTool{}).Where("id = ?", id).Updates(updates)
			if res.Error != nil {
				return res.Error
			}
			affected = res.RowsAffected
		}
		if in.GroupID != nil {
			res := tx.Exec(`UPDATE mcp_tools SET group_id = ?, updated_at = now() WHERE id = ?`, *in.GroupID, id)
			if res.Error != nil {
				return res.Error
			}
			affected = res.RowsAffected
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("controlplane: update tool: %w", err)
	}
	if affected == 0 {
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
		SELECT t.id, t.connector_api_id, coalesce(t.group_id,0) AS group_id, t.engine_type, t.name, t.method, t.path,
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

	var groupID int64
	err := row.Scan(
		&t.ID, &t.ConnectorAPIID, &groupID, &t.EngineType, &t.Name, &t.Method, &t.Path,
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
	t.GroupID = groupID
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
	if err := s.withGroupID(ctx, tools); err != nil {
		return nil, fmt.Errorf("controlplane: list tools for server: group ids: %w", err)
	}
	return tools, nil
}

func orEmpty(m map[string]any) models.JSONMap {
	if m == nil {
		return models.JSONMap{}
	}
	return m
}
