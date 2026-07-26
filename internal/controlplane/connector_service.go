package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrNotFound = errors.New("controlplane: not found")

type ConnectorService struct {
	db *gorm.DB
}

func NewConnectorService(gdb *gorm.DB) *ConnectorService {
	return &ConnectorService{db: gdb}
}

type CreateConnectorInput struct {
	Name        string
	Glyph       string
	Description string
}

func (s *ConnectorService) Create(ctx context.Context, orgID int64, in CreateConnectorInput) (*models.Connector, error) {
	c := &models.Connector{
		OrganizationID: orgID,
		Name:           in.Name,
		Glyph:          in.Glyph,
		Description:    in.Description,
		Status:         models.ConnectorInactive,
	}
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create connector: %w", err)
	}
	return c, nil
}

type connectorRow struct {
	ID              int64                  `gorm:"column:id"`
	Name            string                 `gorm:"column:name"`
	Glyph           string                 `gorm:"column:glyph"`
	Description     string                 `gorm:"column:description"`
	Status          models.ConnectorStatus `gorm:"column:status"`
	LastSyncAt      *time.Time             `gorm:"column:last_sync_at"`
	CreatedAt       time.Time              `gorm:"column:created_at"`
	UpdatedAt       time.Time              `gorm:"column:updated_at"`
	PrimaryType     *string                `gorm:"column:primary_type"`
	PrimaryBaseURL  *string                `gorm:"column:primary_base_url"`
	PrimaryAuthType *string                `gorm:"column:primary_auth_type"`
}

const connectorSelectSQL = `
	SELECT c.id, c.name, coalesce(c.glyph,'') AS glyph, c.description, c.status, c.last_sync_at, c.created_at, c.updated_at,
	       (array_agg(a.engine_type ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1] AS primary_type,
	       (array_agg(a.base_url ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1] AS primary_base_url,
	       (array_agg(a.auth_type ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1] AS primary_auth_type
	FROM connectors c
	LEFT JOIN connector_apis a ON a.connector_id = c.id
`

func (r connectorRow) toModel(orgID int64) *models.Connector {
	c := &models.Connector{
		ID: r.ID, OrganizationID: orgID, Name: r.Name, Glyph: r.Glyph, Description: r.Description,
		Status: r.Status, LastSyncAt: r.LastSyncAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.PrimaryType != nil {
		c.PrimaryType = models.EngineType(*r.PrimaryType)
	}
	if r.PrimaryBaseURL != nil {
		c.PrimaryBaseURL = *r.PrimaryBaseURL
	}
	if r.PrimaryAuthType != nil {
		c.PrimaryAuthType = models.AuthType(*r.PrimaryAuthType)
	}
	return c
}

type ListConnectorsFilter struct {
	Type     string
	Status   string
	Query    string
	Page     int
	PageSize int
}

func (s *ConnectorService) List(ctx context.Context, orgID int64, f ListConnectorsFilter) ([]*models.Connector, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 50
	}

	var rows []connectorRow
	err := s.db.WithContext(ctx).Raw(connectorSelectSQL+`
		WHERE c.organization_id = ?
		  AND (? = '' OR c.name ILIKE '%' || ? || '%')
		  AND (? = '' OR c.status = ?::connector_status)
		  AND (? = '' OR EXISTS (SELECT 1 FROM connector_apis a2 WHERE a2.connector_id = c.id AND a2.engine_type = ?::engine_type))
		GROUP BY c.id
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?
	`, orgID, f.Query, f.Query, f.Status, f.Status, f.Type, f.Type, f.PageSize, (f.Page-1)*f.PageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("controlplane: list connectors: %w", err)
	}

	out := make([]*models.Connector, len(rows))
	for i, r := range rows {
		out[i] = r.toModel(orgID)
	}

	var total int64
	if err := s.db.WithContext(ctx).Model(&models.Connector{}).Where("organization_id = ?", orgID).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("controlplane: count connectors: %w", err)
	}
	return out, int(total), nil
}

func (s *ConnectorService) Get(ctx context.Context, orgID, id int64) (*models.Connector, error) {
	var row connectorRow
	err := s.db.WithContext(ctx).Raw(connectorSelectSQL+`
		WHERE c.organization_id = ? AND c.id = ?
		GROUP BY c.id
	`, orgID, id).Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: get connector: %w", err)
	}
	if row.ID == 0 {
		return nil, ErrNotFound
	}
	return row.toModel(orgID), nil
}

type UpdateConnectorInput struct {
	Name        *string
	Description *string
	Status      *models.ConnectorStatus
}

func (s *ConnectorService) Update(ctx context.Context, orgID, id int64, in UpdateConnectorInput) (*models.Connector, error) {
	updates := map[string]any{"updated_at": gorm.Expr("now()")}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}

	tx := s.db.WithContext(ctx).Model(&models.Connector{}).Where("organization_id = ? AND id = ?", orgID, id).Updates(updates)
	if tx.Error != nil {
		return nil, fmt.Errorf("controlplane: update connector: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, orgID, id)
}

func (s *ConnectorService) Delete(ctx context.Context, orgID, id int64) error {
	tx := s.db.WithContext(ctx).Where("organization_id = ? AND id = ?", orgID, id).Delete(&models.Connector{})
	if tx.Error != nil {
		return fmt.Errorf("controlplane: delete connector: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ConnectorService) HealthCheck(ctx context.Context, orgID, id int64, ping func(baseURL string) error) (*models.Connector, error) {
	var urls []string
	if err := s.db.WithContext(ctx).Model(&models.ConnectorAPI{}).
		Where("connector_id = ? AND is_active", id).
		Pluck("base_url", &urls).Error; err != nil {
		return nil, fmt.Errorf("controlplane: health check: list apis: %w", err)
	}

	status := models.ConnectorActive
	for _, u := range urls {
		if err := ping(u); err != nil {
			status = models.ConnectorError
			break
		}
	}

	now := time.Now()
	err := s.db.WithContext(ctx).Model(&models.Connector{}).Where("organization_id = ? AND id = ?", orgID, id).
		Updates(map[string]any{"status": status, "last_sync_at": now, "updated_at": gorm.Expr("now()")}).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: health check: update: %w", err)
	}
	return s.Get(ctx, orgID, id)
}
