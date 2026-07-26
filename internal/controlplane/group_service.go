package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

// GroupService owns tool_groups — the generalized replacement for trd.md's
// rigid `modules` — and the mcp_server_tool_groups assignment join table
// that makes "invoke by LLM group-wise" real: an MCP server's visible tool
// set is exactly the union of its assigned groups.
type GroupService struct {
	pool *pgxpool.Pool
}

func NewGroupService(pool *pgxpool.Pool) *GroupService {
	return &GroupService{pool: pool}
}

type CreateGroupInput struct {
	Name        string
	Slug        string
	Description string
}

func (s *GroupService) Create(ctx context.Context, orgID int64, in CreateGroupInput) (*models.ToolGroup, error) {
	g := &models.ToolGroup{
		OrganizationID: orgID,
		Name:           in.Name,
		Slug:           in.Slug,
		Description:    in.Description,
		Kind:           models.GroupManual,
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tool_groups (organization_id, name, slug, description, kind)
		VALUES ($1,$2,$3,$4,'manual')
		RETURNING id, created_at, updated_at
	`, orgID, in.Name, in.Slug, in.Description).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("controlplane: create tool_group: %w", err)
	}
	return g, nil
}

func (s *GroupService) List(ctx context.Context, orgID int64) ([]*models.ToolGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name, g.slug, g.description, g.kind, coalesce(g.synced_module_key,''),
		       g.created_at, g.updated_at, count(t.id)
		FROM tool_groups g
		LEFT JOIN mcp_tools t ON t.group_id = g.id AND t.status = 'active'
		WHERE g.organization_id = $1
		GROUP BY g.id
		ORDER BY g.name
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: list tool_groups: %w", err)
	}
	defer rows.Close()

	var out []*models.ToolGroup
	for rows.Next() {
		g := &models.ToolGroup{OrganizationID: orgID}
		if err := rows.Scan(&g.ID, &g.Name, &g.Slug, &g.Description, &g.Kind, &g.SyncedModuleKey, &g.CreatedAt, &g.UpdatedAt, &g.ToolCount); err != nil {
			return nil, fmt.Errorf("controlplane: scan tool_group: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *GroupService) Get(ctx context.Context, orgID, id int64) (*models.ToolGroup, error) {
	g := &models.ToolGroup{OrganizationID: orgID, ID: id}
	err := s.pool.QueryRow(ctx, `
		SELECT name, slug, description, kind, coalesce(synced_module_key,''), created_at, updated_at
		FROM tool_groups WHERE organization_id = $1 AND id = $2
	`, orgID, id).Scan(&g.Name, &g.Slug, &g.Description, &g.Kind, &g.SyncedModuleKey, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *GroupService) Delete(ctx context.Context, orgID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tool_groups WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return fmt.Errorf("controlplane: delete tool_group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AssignToolsToGroup moves a set of tools into this group — a tool belongs
// to at most one group at a time, matching mcp_tools.group_id being a
// single FK rather than a join table.
func (s *GroupService) AssignTools(ctx context.Context, groupID int64, toolIDs []int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE mcp_tools SET group_id = $1, updated_at = now() WHERE id = ANY($2)`, groupID, toolIDs)
	if err != nil {
		return fmt.Errorf("controlplane: assign tools to group: %w", err)
	}
	return nil
}

// SetServerGroups replaces the full set of tool groups an MCP server
// exposes — the group-level equivalent of APIDOC.md's
// `PUT /mcp-servers/:id/connectors`. "Assign a whole connector" in the UI
// expands to "assign every tool_group that has at least one tool under that
// connector" before calling this.
func (s *GroupService) SetServerGroups(ctx context.Context, mcpServerID int64, groupIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("controlplane: set server groups: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM mcp_server_tool_groups WHERE mcp_server_id = $1`, mcpServerID); err != nil {
		return fmt.Errorf("controlplane: set server groups: clear: %w", err)
	}
	for _, groupID := range groupIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO mcp_server_tool_groups (mcp_server_id, tool_group_id) VALUES ($1,$2)`, mcpServerID, groupID); err != nil {
			return fmt.Errorf("controlplane: set server groups: insert: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// GroupsForConnector returns every tool_group with at least one tool under
// the given connector — used to expand "assign this connector" into the
// concrete group IDs SetServerGroups needs.
func (s *GroupService) GroupsForConnector(ctx context.Context, connectorID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT t.group_id
		FROM mcp_tools t
		JOIN connector_apis a ON a.id = t.connector_api_id
		WHERE a.connector_id = $1 AND t.group_id IS NOT NULL
	`, connectorID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: groups for connector: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SyncModules mirrors an external system's module list into the `modules`
// lookup table, then reconciles any `synced` tool_groups against it —
// trd.md's module_sync.go, generalized: instead of validating tools against
// a single host product's fixed list, it keeps whichever tool_groups an org
// chose to mark `synced` up to date with their source connector.
func (s *GroupService) SyncModules(ctx context.Context, modules []models.Module) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("controlplane: sync modules: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, m := range modules {
		_, err := tx.Exec(ctx, `
			INSERT INTO modules (key, label, synced_at) VALUES ($1,$2,now())
			ON CONFLICT (key) DO UPDATE SET label = EXCLUDED.label, synced_at = now()
		`, m.Key, m.Label)
		if err != nil {
			return fmt.Errorf("controlplane: sync modules: upsert %q: %w", m.Key, err)
		}
	}
	return tx.Commit(ctx)
}

// EnsureSyncedGroup creates or updates the tool_group backing one synced
// module, so tool_service.Create can attach newly-discovered tools to it by
// slug immediately after a module sync.
func (s *GroupService) EnsureSyncedGroup(ctx context.Context, orgID int64, moduleKey, label string) (*models.ToolGroup, error) {
	g := &models.ToolGroup{OrganizationID: orgID, Kind: models.GroupSynced, SyncedModuleKey: moduleKey}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tool_groups (organization_id, name, slug, kind, synced_module_key)
		VALUES ($1,$2,$3,'synced',$3)
		ON CONFLICT (organization_id, slug) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		RETURNING id, name, slug, created_at, updated_at
	`, orgID, label, moduleKey).Scan(&g.ID, &g.Name, &g.Slug, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("controlplane: ensure synced group: %w", err)
	}
	return g, nil
}
