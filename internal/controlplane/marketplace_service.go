package controlplane

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

type MarketplaceService struct {
	db *gorm.DB
}

func NewMarketplaceService(gdb *gorm.DB) *MarketplaceService {
	return &MarketplaceService{db: gdb}
}

func (s *MarketplaceService) List(ctx context.Context, category, query string) ([]*models.AdapterTemplate, error) {
	tx := s.db.WithContext(ctx).Model(&models.AdapterTemplate{}).Order("name")
	if category != "" {
		tx = tx.Where("category = ?", category)
	}
	if query != "" {
		tx = tx.Where("name ILIKE ?", "%"+query+"%")
	}

	var out []*models.AdapterTemplate
	if err := tx.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("controlplane: list marketplace templates: %w", err)
	}
	return out, nil
}

func (s *MarketplaceService) Get(ctx context.Context, key string) (*models.AdapterTemplate, error) {
	var t models.AdapterTemplate
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("controlplane: get marketplace template: %w", err)
	}
	return &t, nil
}

func (s *MarketplaceService) IncrementInstallCount(ctx context.Context, id int64) error {
	err := s.db.WithContext(ctx).Model(&models.AdapterTemplate{}).Where("id = ?", id).
		Updates(map[string]any{"install_count": gorm.Expr("install_count + 1"), "updated_at": gorm.Expr("now()")}).Error
	if err != nil {
		return fmt.Errorf("controlplane: increment install count: %w", err)
	}
	return nil
}
