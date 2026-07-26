package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

// OrganizationService backs Phase 2 — the org profile itself. Creation
// happens in identity.AuthService.Register (it's part of the signup
// bootstrap); this service only reads/updates/deletes an existing one.
type OrganizationService struct {
	pool *pgxpool.Pool
}

func NewOrganizationService(pool *pgxpool.Pool) *OrganizationService {
	return &OrganizationService{pool: pool}
}

func (s *OrganizationService) Get(ctx context.Context, id int64) (*models.Organization, error) {
	o := &models.Organization{ID: id}
	err := s.pool.QueryRow(ctx, `
		SELECT company_id, tenant_id, name, slug, timezone, created_at
		FROM organizations WHERE id = $1
	`, id).Scan(&o.CompanyID, &o.TenantID, &o.Name, &o.Slug, &o.Timezone, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

type UpdateOrganizationInput struct {
	Name     *string
	Timezone *string
}

func (s *OrganizationService) Update(ctx context.Context, id int64, in UpdateOrganizationInput) (*models.Organization, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE organizations SET name = coalesce($2, name), timezone = coalesce($3, timezone) WHERE id = $1
	`, id, in.Name, in.Timezone)
	if err != nil {
		return nil, fmt.Errorf("controlplane: update organization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

// Delete cascades everything below via the schema's ON DELETE CASCADE
// chain (connectors, mcp_servers, users, sessions, ...). Danger-zone only.
func (s *OrganizationService) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("controlplane: delete organization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
