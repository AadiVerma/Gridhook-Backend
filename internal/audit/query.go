package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

type ListFilter struct {
	Status      string
	ConnectorID int64
	ServerID    int64
	ToolID      int64
	From        *time.Time
	To          *time.Time
	Page        int
	PageSize    int
}

const invocationSelect = `id, tool_id, connector_id, connector_api_id, coalesce(mcp_server_id,0) AS mcp_server_id, organization_id,
	coalesce(user_id,0) AS user_id, coalesce(user_email,'') AS user_email, status,
	coalesce(http_code,0) AS http_code, duration_ms, input, output, coalesce(error,'') AS error, created_at`

func listInvocations(ctx context.Context, gdb *gorm.DB, orgID int64, f ListFilter) ([]*models.ToolInvocation, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 50
	}

	filter := func(tx *gorm.DB) *gorm.DB {
		tx = tx.Where("organization_id = ?", orgID)
		if f.Status != "" {
			tx = tx.Where("status = ?", f.Status)
		}
		if f.ConnectorID != 0 {
			tx = tx.Where("connector_id = ?", f.ConnectorID)
		}
		if f.ServerID != 0 {
			tx = tx.Where("mcp_server_id = ?", f.ServerID)
		}
		if f.ToolID != 0 {
			tx = tx.Where("tool_id = ?", f.ToolID)
		}
		if f.From != nil {
			tx = tx.Where("created_at >= ?", *f.From)
		}
		if f.To != nil {
			tx = tx.Where("created_at <= ?", *f.To)
		}
		return tx
	}

	var total int64
	if err := filter(gdb.WithContext(ctx).Model(&models.ToolInvocation{})).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("audit: count: %w", err)
	}

	var rows []*models.ToolInvocation
	err := filter(gdb.WithContext(ctx).Model(&models.ToolInvocation{})).
		Select(invocationSelect).
		Order("created_at DESC").
		Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).
		Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("audit: list: %w", err)
	}
	return rows, int(total), nil
}

func getInvocation(ctx context.Context, gdb *gorm.DB, orgID, id int64) (*models.ToolInvocation, error) {
	inv := &models.ToolInvocation{}
	err := gdb.WithContext(ctx).Model(&models.ToolInvocation{}).
		Select(invocationSelect).
		Where("organization_id = ? AND id = ?", orgID, id).
		First(inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {

		return nil, fmt.Errorf("audit: invocation %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("audit: get invocation: %w", err)
	}
	return inv, nil
}
