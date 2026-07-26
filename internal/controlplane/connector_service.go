// Package controlplane owns create/read/update/delete for every configured
// entity (connectors, their APIs, tools, groups) — the "control plane" from
// ARCHITECTURE.md's component diagram. It's the only package that writes to
// those tables; the dispatcher and MCP routes only ever read through
// ToolService/GroupService's resolve methods.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrNotFound = errors.New("controlplane: not found")

type ConnectorService struct {
	pool *pgxpool.Pool
}

func NewConnectorService(pool *pgxpool.Pool) *ConnectorService {
	return &ConnectorService{pool: pool}
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
	err := s.pool.QueryRow(ctx, `
		INSERT INTO connectors (organization_id, name, glyph, description, status)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at, updated_at
	`, orgID, in.Name, in.Glyph, in.Description, c.Status).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("controlplane: create connector: %w", err)
	}
	return c, nil
}

type ListConnectorsFilter struct {
	Type     string // filters by any of its APIs' engine_type
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

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, coalesce(c.glyph,''), c.description, c.status, c.last_sync_at, c.created_at, c.updated_at,
		       (array_agg(a.engine_type ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1] AS primary_type,
		       (array_agg(a.base_url ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1] AS primary_base_url,
		       (array_agg(a.auth_type ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1] AS primary_auth_type
		FROM connectors c
		LEFT JOIN connector_apis a ON a.connector_id = c.id
		WHERE c.organization_id = $1
		  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR c.status = $3::connector_status)
		  AND ($4 = '' OR EXISTS (SELECT 1 FROM connector_apis a2 WHERE a2.connector_id = c.id AND a2.engine_type = $4::engine_type))
		GROUP BY c.id
		ORDER BY c.created_at DESC
		LIMIT $5 OFFSET $6
	`, orgID, f.Query, f.Status, f.Type, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("controlplane: list connectors: %w", err)
	}
	defer rows.Close()

	var out []*models.Connector
	for rows.Next() {
		c := &models.Connector{}
		var primaryType, primaryAuth *string
		if err := rows.Scan(&c.ID, &c.Name, &c.Glyph, &c.Description, &c.Status, &c.LastSyncAt, &c.CreatedAt, &c.UpdatedAt,
			&primaryType, &c.PrimaryBaseURL, &primaryAuth); err != nil {
			return nil, 0, fmt.Errorf("controlplane: scan connector: %w", err)
		}
		c.OrganizationID = orgID
		if primaryType != nil {
			c.PrimaryType = models.EngineType(*primaryType)
		}
		if primaryAuth != nil {
			c.PrimaryAuthType = models.AuthType(*primaryAuth)
		}
		out = append(out, c)
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM connectors WHERE organization_id = $1`, orgID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("controlplane: count connectors: %w", err)
	}
	return out, total, rows.Err()
}

func (s *ConnectorService) Get(ctx context.Context, orgID, id int64) (*models.Connector, error) {
	c := &models.Connector{}
	var primaryType, primaryAuth *string
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.name, coalesce(c.glyph,''), c.description, c.status, c.last_sync_at, c.created_at, c.updated_at,
		       (array_agg(a.engine_type ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1],
		       (array_agg(a.base_url ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1],
		       (array_agg(a.auth_type ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL))[1]
		FROM connectors c
		LEFT JOIN connector_apis a ON a.connector_id = c.id
		WHERE c.organization_id = $1 AND c.id = $2
		GROUP BY c.id
	`, orgID, id).Scan(&c.ID, &c.Name, &c.Glyph, &c.Description, &c.Status, &c.LastSyncAt, &c.CreatedAt, &c.UpdatedAt,
		&primaryType, &c.PrimaryBaseURL, &primaryAuth)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: get connector: %w", err)
	}
	c.OrganizationID = orgID
	if primaryType != nil {
		c.PrimaryType = models.EngineType(*primaryType)
	}
	if primaryAuth != nil {
		c.PrimaryAuthType = models.AuthType(*primaryAuth)
	}
	return c, nil
}

type UpdateConnectorInput struct {
	Name        *string
	Description *string
	Status      *models.ConnectorStatus
}

func (s *ConnectorService) Update(ctx context.Context, orgID, id int64, in UpdateConnectorInput) (*models.Connector, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE connectors SET
			name = coalesce($3, name),
			description = coalesce($4, description),
			status = coalesce($5, status),
			updated_at = now()
		WHERE organization_id = $1 AND id = $2
	`, orgID, id, in.Name, in.Description, in.Status)
	if err != nil {
		return nil, fmt.Errorf("controlplane: update connector: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, orgID, id)
}

func (s *ConnectorService) Delete(ctx context.Context, orgID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM connectors WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return fmt.Errorf("controlplane: delete connector: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// HealthCheck pings the connector's primary API and updates status/last_sync.
// A connector with multiple APIs is healthy only if all of them respond.
func (s *ConnectorService) HealthCheck(ctx context.Context, orgID, id int64, ping func(baseURL string) error) (*models.Connector, error) {
	rows, err := s.pool.Query(ctx, `SELECT base_url FROM connector_apis WHERE connector_id = $1 AND is_active`, id)
	if err != nil {
		return nil, fmt.Errorf("controlplane: health check: list apis: %w", err)
	}
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return nil, err
		}
		urls = append(urls, u)
	}
	rows.Close()

	status := models.ConnectorActive
	for _, u := range urls {
		if err := ping(u); err != nil {
			status = models.ConnectorError
			break
		}
	}

	now := time.Now()
	_, err = s.pool.Exec(ctx, `
		UPDATE connectors SET status = $3, last_sync_at = $4, updated_at = now()
		WHERE organization_id = $1 AND id = $2
	`, orgID, id, status, now)
	if err != nil {
		return nil, fmt.Errorf("controlplane: health check: update: %w", err)
	}
	return s.Get(ctx, orgID, id)
}
