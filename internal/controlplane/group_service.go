package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gridhook.dev/connector-backend/internal/models"
)

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
	g := &models.ToolGroup{
		OrganizationID: orgID,
		Name:           in.Name,
		Slug:           in.Slug,
		Description:    in.Description,
		Kind:           models.GroupManual,
	}
	if err := s.db.WithContext(ctx).Omit("SyncedModuleKey").Create(g).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create tool_group: %w", err)
	}
	return g, nil
}

func (s *GroupService) List(ctx context.Context, orgID int64) ([]*models.ToolGroup, error) {
	var rows []struct {
		ID              int64
		Name            string
		Slug            string
		Description     string
		Kind            models.ToolGroupKind
		SyncedModuleKey string
		CreatedAt       time.Time
		UpdatedAt       time.Time
		ToolCount       int64
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
			Kind: r.Kind, SyncedModuleKey: r.SyncedModuleKey, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
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

func (s *GroupService) AssignTools(ctx context.Context, groupID int64, toolIDs []int64) error {
	err := s.db.WithContext(ctx).Model(&models.MCPTool{}).Where("id IN ?", toolIDs).
		Updates(map[string]any{"group_id": groupID, "updated_at": gorm.Expr("now()")}).Error
	if err != nil {
		return fmt.Errorf("controlplane: assign tools to group: %w", err)
	}
	return nil
}

func (s *GroupService) SetServerGroups(ctx context.Context, mcpServerID int64, groupIDs []int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("mcp_server_id = ?", mcpServerID).Delete(&models.MCPServerToolGroup{}).Error; err != nil {
			return fmt.Errorf("controlplane: set server groups: clear: %w", err)
		}
		for _, groupID := range groupIDs {
			row := models.MCPServerToolGroup{MCPServerID: mcpServerID, ToolGroupID: groupID}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("controlplane: set server groups: insert: %w", err)
			}
		}
		return nil
	})
}

func (s *GroupService) GroupsForConnector(ctx context.Context, connectorID int64) ([]int64, error) {
	var ids []int64
	err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT t.group_id
		FROM mcp_tools t
		JOIN connector_apis a ON a.id = t.connector_api_id
		WHERE a.connector_id = ? AND t.group_id IS NOT NULL
	`, connectorID).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: groups for connector: %w", err)
	}
	return ids, nil
}

func (s *GroupService) SyncModules(ctx context.Context, modules []models.Module) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range modules {
			row := models.Module{Key: m.Key, Label: m.Label, SyncedAt: time.Now()}
			err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"label", "synced_at"}),
			}).Create(&row).Error
			if err != nil {
				return fmt.Errorf("controlplane: sync modules: upsert %q: %w", m.Key, err)
			}
		}
		return nil
	})
}

func (s *GroupService) EnsureSyncedGroup(ctx context.Context, orgID int64, moduleKey, label string) (*models.ToolGroup, error) {
	g := &models.ToolGroup{
		OrganizationID: orgID, Name: label, Slug: moduleKey, Kind: models.GroupSynced, SyncedModuleKey: moduleKey,
	}
	err := s.db.WithContext(ctx).Clauses(
		clause.OnConflict{
			Columns:   []clause.Column{{Name: "organization_id"}, {Name: "slug"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at"}),
		},
		clause.Returning{},
	).Create(g).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: ensure synced group: %w", err)
	}
	return g, nil
}
