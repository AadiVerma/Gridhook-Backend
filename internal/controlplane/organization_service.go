package controlplane

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

type OrganizationService struct {
	db *gorm.DB
}

func NewOrganizationService(gdb *gorm.DB) *OrganizationService {
	return &OrganizationService{db: gdb}
}

func (s *OrganizationService) Get(ctx context.Context, id int64) (*models.Organization, error) {
	o := &models.Organization{}
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return o, nil
}

type UpdateOrganizationInput struct {
	Name     *string
	Timezone *string
}

func (s *OrganizationService) Update(ctx context.Context, id int64, in UpdateOrganizationInput) (*models.Organization, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Timezone != nil {
		updates["timezone"] = *in.Timezone
	}

	tx := s.db.WithContext(ctx).Model(&models.Organization{}).Where("id = ?", id).Updates(updates)
	if tx.Error != nil {
		return nil, fmt.Errorf("controlplane: update organization: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *OrganizationService) Delete(ctx context.Context, id int64) error {
	tx := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Organization{})
	if tx.Error != nil {
		return fmt.Errorf("controlplane: delete organization: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
