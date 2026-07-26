package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrUserNotFound = errors.New("identity: user not found")

// UserService backs Phase 3 (Users & Roles). Ships with the four fixed
// system roles from APIDOC.md — no custom-role CRUD until something
// actually needs it.
type UserService struct {
	pool *pgxpool.Pool
}

func NewUserService(pool *pgxpool.Pool) *UserService {
	return &UserService{pool: pool}
}

func (s *UserService) List(ctx context.Context, orgID int64) ([]*models.User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, name, role, status, last_active_at, created_at
		FROM users WHERE organization_id = $1 ORDER BY created_at
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("identity: list users: %w", err)
	}
	defer rows.Close()

	var out []*models.User
	for rows.Next() {
		u := &models.User{OrganizationID: orgID}
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Status, &u.LastActiveAt, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("identity: scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Invite creates an `invited` user with a random password (never revealed);
// the accept-invite flow sets a real password via a separately issued
// invite token. Token issuance/email delivery is intentionally out of
// scope for this scaffold.
func (s *UserService) Invite(ctx context.Context, orgID int64, email, name string, role models.UserRole) (*models.User, error) {
	placeholder := make([]byte, 32)
	if _, err := rand.Read(placeholder); err != nil {
		return nil, err
	}
	hash, err := HashPassword(base64.RawURLEncoding.EncodeToString(placeholder))
	if err != nil {
		return nil, err
	}

	u := &models.User{OrganizationID: orgID, Email: email, Name: name, Role: role, Status: models.UserStatusInvited}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (organization_id, email, name, password_hash, role, status)
		VALUES ($1,$2,$3,$4,$5,'invited')
		RETURNING id, created_at
	`, orgID, email, name, hash, role).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("identity: invite user: %w", err)
	}
	return u, nil
}

type UpdateUserInput struct {
	Role   *models.UserRole
	Status *models.UserStatus
}

func (s *UserService) Update(ctx context.Context, orgID, id int64, in UpdateUserInput) (*models.User, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET role = coalesce($3, role), status = coalesce($4, status)
		WHERE organization_id = $1 AND id = $2
	`, orgID, id, in.Role, in.Status)
	if err != nil {
		return nil, fmt.Errorf("identity: update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrUserNotFound
	}

	u := &models.User{OrganizationID: orgID, ID: id}
	err = s.pool.QueryRow(ctx, `
		SELECT email, name, role, status, last_active_at, created_at FROM users WHERE id = $1
	`, id).Scan(&u.Email, &u.Name, &u.Role, &u.Status, &u.LastActiveAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

func (s *UserService) Delete(ctx context.Context, orgID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return fmt.Errorf("identity: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

type RoleSummary struct {
	Role        models.UserRole `json:"role"`
	MemberCount int             `json:"memberCount"`
}

func (s *UserService) ListRoles(ctx context.Context, orgID int64) ([]RoleSummary, error) {
	counts := map[models.UserRole]int{}
	rows, err := s.pool.Query(ctx, `SELECT role, count(*) FROM users WHERE organization_id = $1 GROUP BY role`, orgID)
	if err != nil {
		return nil, fmt.Errorf("identity: list roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role models.UserRole
		var count int
		if err := rows.Scan(&role, &count); err != nil {
			return nil, err
		}
		counts[role] = count
	}

	all := []models.UserRole{models.RoleOwner, models.RoleAdmin, models.RoleDeveloper, models.RoleViewer}
	out := make([]RoleSummary, 0, len(all))
	for _, r := range all {
		out = append(out, RoleSummary{Role: r, MemberCount: counts[r]})
	}
	return out, rows.Err()
}
