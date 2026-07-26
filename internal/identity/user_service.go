package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrUserNotFound = errors.New("identity: user not found")

type UserService struct {
	db *gorm.DB
}

func NewUserService(gdb *gorm.DB) *UserService {
	return &UserService{db: gdb}
}

func (s *UserService) List(ctx context.Context, orgID int64) ([]*models.User, error) {
	var users []*models.User
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Order("created_at").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("identity: list users: %w", err)
	}
	return users, nil
}

func (s *UserService) Invite(ctx context.Context, orgID int64, email, name string, role models.UserRole) (*models.User, error) {
	placeholder := make([]byte, 32)
	if _, err := rand.Read(placeholder); err != nil {
		return nil, err
	}
	hash, err := HashPassword(base64.RawURLEncoding.EncodeToString(placeholder))
	if err != nil {
		return nil, err
	}

	u := &models.User{OrganizationID: orgID, Email: email, Name: name, PasswordHash: hash, Role: role, Status: models.UserStatusInvited}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		return nil, fmt.Errorf("identity: invite user: %w", err)
	}
	return u, nil
}

type UpdateUserInput struct {
	Role   *models.UserRole
	Status *models.UserStatus
}

func (s *UserService) Update(ctx context.Context, orgID, id int64, in UpdateUserInput) (*models.User, error) {
	updates := map[string]any{}
	if in.Role != nil {
		updates["role"] = *in.Role
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}

	tx := s.db.WithContext(ctx).Model(&models.User{}).Where("organization_id = ? AND id = ?", orgID, id).Updates(updates)
	if tx.Error != nil {
		return nil, fmt.Errorf("identity: update user: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return nil, ErrUserNotFound
	}

	u := &models.User{}
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

func (s *UserService) Delete(ctx context.Context, orgID, id int64) error {
	tx := s.db.WithContext(ctx).Where("organization_id = ? AND id = ?", orgID, id).Delete(&models.User{})
	if tx.Error != nil {
		return fmt.Errorf("identity: delete user: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

type RoleSummary struct {
	Role        models.UserRole `json:"role"`
	MemberCount int             `json:"memberCount"`
}

func (s *UserService) ListRoles(ctx context.Context, orgID int64) ([]RoleSummary, error) {
	var rows []struct {
		Role  models.UserRole
		Count int
	}
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Select("role, count(*) as count").
		Where("organization_id = ?", orgID).
		Group("role").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("identity: list roles: %w", err)
	}
	counts := map[models.UserRole]int{}
	for _, r := range rows {
		counts[r.Role] = r.Count
	}

	all := []models.UserRole{models.RoleOwner, models.RoleAdmin, models.RoleDeveloper, models.RoleViewer}
	out := make([]RoleSummary, 0, len(all))
	for _, r := range all {
		out = append(out, RoleSummary{Role: r, MemberCount: counts[r]})
	}
	return out, nil
}
