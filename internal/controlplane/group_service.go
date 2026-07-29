package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/slug"
)

func slugify(name string) string { return slug.Make(name) }

type GroupService struct {
	db *gorm.DB
}

func NewGroupService(gdb *gorm.DB) *GroupService {
	return &GroupService{db: gdb}
}

type CreateGroupInput struct {
	Name        string
	Slug        string
	Description string
}

func (s *GroupService) Create(ctx context.Context, orgID int64, in CreateGroupInput) (*models.ToolGroup, error) {
	groupSlug := in.Slug
	if groupSlug == "" {
		var err error
		groupSlug, err = s.uniqueSlug(ctx, orgID, in.Name)
		if err != nil {
			return nil, fmt.Errorf("controlplane: create tool_group: generate slug: %w", err)
		}
	}
	g := &models.ToolGroup{
		OrganizationID: orgID,
		Name:           in.Name,
		Slug:           groupSlug,
		Description:    in.Description,
		Kind:           models.GroupManual,
	}
	if err := s.db.WithContext(ctx).Create(g).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create tool_group: %w", err)
	}
	return g, nil
}

func (s *GroupService) uniqueSlug(ctx context.Context, orgID int64, name string) (string, error) {
	base := slugify(name)
	candidate := base
	for attempt := 2; ; attempt++ {
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.ToolGroup{}).
			Where("organization_id = ? AND slug = ?", orgID, candidate).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, attempt)
	}
}

func (s *GroupService) List(ctx context.Context, orgID int64) ([]*models.ToolGroup, error) {
	var rows []struct {
		ID          int64
		Name        string
		Slug        string
		Description string
		Kind        models.ToolGroupKind
		CreatedAt   time.Time
		UpdatedAt   time.Time
		ToolCount   int64
	}
	err := s.db.WithContext(ctx).Model(&models.ToolGroup{}).
		Select("tool_groups.*, count(t.id) AS tool_count").
		Joins("LEFT JOIN mcp_tools t ON t.group_id = tool_groups.id AND t.status = 'active'").
		Where("tool_groups.organization_id = ?", orgID).
		Group("tool_groups.id").
		Order("tool_groups.name").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list tool_groups: %w", err)
	}
	out := make([]*models.ToolGroup, len(rows))
	for i, r := range rows {
		out[i] = &models.ToolGroup{
			OrganizationID: orgID, ID: r.ID, Name: r.Name, Slug: r.Slug, Description: r.Description,
			Kind: r.Kind, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			ToolCount: int(r.ToolCount),
		}
	}
	return out, nil
}

func (s *GroupService) Get(ctx context.Context, orgID, id int64) (*models.ToolGroup, error) {
	g := &models.ToolGroup{}
	err := s.db.WithContext(ctx).Where("organization_id = ? AND id = ?", orgID, id).First(g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *GroupService) Delete(ctx context.Context, orgID, id int64) error {
	tx := s.db.WithContext(ctx).Where("organization_id = ? AND id = ?", orgID, id).Delete(&models.ToolGroup{})
	if tx.Error != nil {
		return fmt.Errorf("controlplane: delete tool_group: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupService) AssignTools(ctx context.Context, orgID, groupID int64, toolIDs []int64) error {
	if len(toolIDs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups int64
		if err := tx.Model(&models.ToolGroup{}).
			Where("id = ? AND organization_id = ?", groupID, orgID).Count(&groups).Error; err != nil {
			return fmt.Errorf("controlplane: assign tools: verify group: %w", err)
		}
		if groups == 0 {
			return ErrNotFound
		}

		res := tx.Model(&models.MCPTool{}).
			Where("id IN ?", toolIDs).
			Where(`connector_api_id IN (
				SELECT a.id FROM connector_apis a
				JOIN connectors c ON c.id = a.connector_id
				WHERE c.organization_id = ?)`, orgID).
			Updates(map[string]any{"group_id": groupID, "updated_at": gorm.Expr("now()")})
		if res.Error != nil {
			return fmt.Errorf("controlplane: assign tools to group: %w", res.Error)
		}
		if res.RowsAffected != int64(len(toolIDs)) {
			return fmt.Errorf("%w: one or more tools do not exist in this organization", ErrNotFound)
		}
		return nil
	})
}

func (s *GroupService) SetServerGroups(ctx context.Context, orgID, mcpServerID int64, groupIDs []int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var servers int64
		if err := tx.Model(&models.MCPServer{}).
			Where("id = ? AND organization_id = ?", mcpServerID, orgID).Count(&servers).Error; err != nil {
			return fmt.Errorf("controlplane: set server groups: verify server: %w", err)
		}
		if servers == 0 {
			return ErrNotFound
		}

		if len(groupIDs) > 0 {
			var owned int64
			if err := tx.Model(&models.ToolGroup{}).
				Where("id IN ? AND organization_id = ?", groupIDs, orgID).Count(&owned).Error; err != nil {
				return fmt.Errorf("controlplane: set server groups: verify groups: %w", err)
			}
			if owned != int64(len(uniqueIDs(groupIDs))) {
				return fmt.Errorf("%w: one or more tool groups do not exist in this organization", ErrNotFound)
			}
		}

		if err := tx.Where("mcp_server_id = ?", mcpServerID).Delete(&models.MCPServerToolGroup{}).Error; err != nil {
			return fmt.Errorf("controlplane: set server groups: clear: %w", err)
		}
		if len(groupIDs) == 0 {
			return nil
		}

		rows := make([]models.MCPServerToolGroup, 0, len(groupIDs))
		for _, groupID := range uniqueIDs(groupIDs) {
			rows = append(rows, models.MCPServerToolGroup{MCPServerID: mcpServerID, ToolGroupID: groupID})
		}

		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("controlplane: set server groups: insert: %w", err)
		}
		return nil
	})
}

func (s *GroupService) GroupsForConnector(ctx context.Context, orgID, connectorID int64) ([]int64, error) {
	var ids []int64
	err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT group_id FROM (
			SELECT a.group_id
			FROM connector_apis a
			JOIN connectors c ON c.id = a.connector_id
			WHERE a.connector_id = ? AND c.organization_id = ? AND a.group_id IS NOT NULL
			UNION
			SELECT t.group_id
			FROM mcp_tools t
			JOIN connector_apis a2 ON a2.id = t.connector_api_id
			JOIN connectors c2 ON c2.id = a2.connector_id
			WHERE a2.connector_id = ? AND c2.organization_id = ? AND t.group_id IS NOT NULL
		) modules
	`, connectorID, orgID, connectorID, orgID).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: groups for connector: %w", err)
	}
	return ids, nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
