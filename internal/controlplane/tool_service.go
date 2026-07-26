package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/dispatcher"
	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/parsers"
)

// ToolService owns mcp_tools CRUD and implements dispatcher.ToolStore, so
// the live dispatch path and the admin CRUD path share one source of truth
// for what a tool is and which connector_api it belongs to.
type ToolService struct {
	pool *pgxpool.Pool
}

func NewToolService(pool *pgxpool.Pool) *ToolService {
	return &ToolService{pool: pool}
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
	var groupID any
	if in.GroupID != 0 {
		groupID = in.GroupID
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO mcp_tools
			(connector_api_id, group_id, engine_type, name, method, path, description,
			 parameters, endpoint_mapping, response_mapping, output_schema, cached, cache_ttl_seconds)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, status, version, display_on_frontend, created_at, updated_at
	`, connectorAPIID, groupID, engineType, in.Name, in.Method, in.Path, in.Description,
		t.Parameters, t.EndpointMapping, t.ResponseMapping, t.OutputSchema, in.Cached, in.CacheTTLSeconds,
	).Scan(&t.ID, &t.Status, &t.Version, &t.DisplayOnFrontend, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("controlplane: create tool: %w", err)
	}
	if in.GroupID != 0 {
		t.GroupID = in.GroupID
	}
	return t, nil
}

// BulkCreate persists every DraftTool a parser produced against one
// connector_api — the "creates connector + tools in one shot" step behind
// POST /connectors/import.
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
	rows, err := s.pool.Query(ctx, `
		SELECT id, connector_api_id, coalesce(group_id,0), engine_type, name, method, path, description,
		       parameters, endpoint_mapping, response_mapping, output_schema, cached, cache_ttl_seconds,
		       status, version, coalesce(display_title,''), display_on_frontend, created_at, updated_at
		FROM mcp_tools WHERE connector_api_id = $1 ORDER BY created_at
	`, connectorAPIID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: list tools: %w", err)
	}
	defer rows.Close()
	return scanTools(rows)
}

func (s *ToolService) Get(ctx context.Context, id int64) (*models.MCPTool, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, connector_api_id, coalesce(group_id,0), engine_type, name, method, path, description,
		       parameters, endpoint_mapping, response_mapping, output_schema, cached, cache_ttl_seconds,
		       status, version, coalesce(display_title,''), display_on_frontend, created_at, updated_at
		FROM mcp_tools WHERE id = $1
	`, id)
	t, err := scanTool(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

type UpdateToolInput struct {
	Name            *string
	Method          *models.HTTPMethod
	Path            *string
	CacheTTLSeconds *int
	GroupID         *int64
}

func (s *ToolService) Update(ctx context.Context, id int64, in UpdateToolInput) (*models.MCPTool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mcp_tools SET
			name = coalesce($2, name),
			method = coalesce($3, method),
			path = coalesce($4, path),
			cache_ttl_seconds = coalesce($5, cache_ttl_seconds),
			group_id = coalesce($6, group_id),
			updated_at = now()
		WHERE id = $1
	`, id, in.Name, in.Method, in.Path, in.CacheTTLSeconds, in.GroupID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: update tool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *ToolService) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mcp_tools WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("controlplane: delete tool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResolveForServer implements dispatcher.ToolStore: find a tool by name,
// scoped to exactly the tool groups assigned to mcpServerID. A tool outside
// every assigned group is invisible here by design — this is the
// group-wise invocation boundary from ARCHITECTURE.md §3.
func (s *ToolService) ResolveForServer(ctx context.Context, mcpServerID int64, toolName string) (*dispatcher.ToolLookup, error) {
	var lookup dispatcher.ToolLookup
	t := &models.MCPTool{}
	a := &models.ConnectorAPI{}
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.connector_api_id, coalesce(t.group_id,0), t.engine_type, t.name, t.method, t.path,
		       t.description, t.parameters, t.endpoint_mapping, t.response_mapping, t.output_schema,
		       t.cached, t.cache_ttl_seconds, t.status, t.version, coalesce(t.display_title,''), t.display_on_frontend,
		       t.created_at, t.updated_at,
		       a.id, a.connector_id, a.name, a.engine_type, a.base_url, a.auth_type, coalesce(a.spec_url,''),
		       a.is_active, a.created_at, a.updated_at
		FROM mcp_tools t
		JOIN connector_apis a ON a.id = t.connector_api_id
		JOIN mcp_server_tool_groups g ON g.tool_group_id = t.group_id AND g.mcp_server_id = $1
		WHERE t.name = $2 AND t.status = 'active'
	`, mcpServerID, toolName).Scan(
		&t.ID, &t.ConnectorAPIID, &t.GroupID, &t.EngineType, &t.Name, &t.Method, &t.Path,
		&t.Description, &t.Parameters, &t.EndpointMapping, &t.ResponseMapping, &t.OutputSchema,
		&t.Cached, &t.CacheTTLSeconds, &t.Status, &t.Version, &t.DisplayTitle, &t.DisplayOnFrontend,
		&t.CreatedAt, &t.UpdatedAt,
		&a.ID, &a.ConnectorID, &a.Name, &a.EngineType, &a.BaseURL, &a.AuthType, &a.SpecURL,
		&a.IsActive, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("controlplane: tool %q not found or not assigned to this server", toolName)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: resolve tool: %w", err)
	}
	lookup.Tool = t
	lookup.API = a
	lookup.ConnectorID = a.ConnectorID
	return &lookup, nil
}

// ListForServer returns every tool reachable from an MCP server — the
// aggregated set GET /mcp/:slug/tools serves to clients (and what
// GET /mcp-servers/:id/tools shows in the admin UI).
func (s *ToolService) ListForServer(ctx context.Context, mcpServerID int64) ([]*models.MCPTool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.connector_api_id, coalesce(t.group_id,0), t.engine_type, t.name, t.method, t.path,
		       t.description, t.parameters, t.endpoint_mapping, t.response_mapping, t.output_schema,
		       t.cached, t.cache_ttl_seconds, t.status, t.version, coalesce(t.display_title,''), t.display_on_frontend,
		       t.created_at, t.updated_at
		FROM mcp_tools t
		JOIN mcp_server_tool_groups g ON g.tool_group_id = t.group_id AND g.mcp_server_id = $1
		WHERE t.status = 'active'
		ORDER BY t.name
	`, mcpServerID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: list tools for server: %w", err)
	}
	defer rows.Close()
	return scanTools(rows)
}

func scanTools(rows pgx.Rows) ([]*models.MCPTool, error) {
	var out []*models.MCPTool
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// row abstracts over pgx.Row and pgx.Rows, both of which implement Scan.
type row interface {
	Scan(dest ...any) error
}

func scanTool(r row) (*models.MCPTool, error) {
	t := &models.MCPTool{}
	err := r.Scan(&t.ID, &t.ConnectorAPIID, &t.GroupID, &t.EngineType, &t.Name, &t.Method, &t.Path,
		&t.Description, &t.Parameters, &t.EndpointMapping, &t.ResponseMapping, &t.OutputSchema,
		&t.Cached, &t.CacheTTLSeconds, &t.Status, &t.Version, &t.DisplayTitle, &t.DisplayOnFrontend,
		&t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
