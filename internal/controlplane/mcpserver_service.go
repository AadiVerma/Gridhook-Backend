package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/models"
)

// MCPServerService owns mcp_servers CRUD, its tool-group assignment, and its
// API keys. This is the layer whose GET /mcp-servers/:id/tools view (backed
// by ToolService.ListForServer) is what the separate MCP runtime actually
// serves to AI clients.
type MCPServerService struct {
	pool       *pgxpool.Pool
	publicBase string // e.g. https://gw.gridhook.dev/mcp
}

func NewMCPServerService(pool *pgxpool.Pool, publicBaseURL string) *MCPServerService {
	return &MCPServerService{pool: pool, publicBase: publicBaseURL}
}

type CreateServerInput struct {
	Name        string
	Slug        string
	Description string
}

func (s *MCPServerService) Create(ctx context.Context, orgID int64, in CreateServerInput) (*models.MCPServer, error) {
	srv := &models.MCPServer{
		OrganizationID: orgID,
		Name:           in.Name,
		Slug:           in.Slug,
		Description:    in.Description,
		Status:         models.ServerStopped,
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO mcp_servers (organization_id, name, slug, description, status)
		VALUES ($1,$2,$3,$4,'stopped')
		RETURNING id, created_at, updated_at
	`, orgID, in.Name, in.Slug, in.Description).Scan(&srv.ID, &srv.CreatedAt, &srv.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("controlplane: create mcp_server: %w", err)
	}
	srv.Endpoint = s.publicBase + "/" + srv.Slug
	return srv, nil
}

func (s *MCPServerService) List(ctx context.Context, orgID int64, statusFilter, query string) ([]*models.MCPServer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, slug, description, custom_instructions, status, created_at, updated_at
		FROM mcp_servers
		WHERE organization_id = $1
		  AND ($2 = '' OR status = $2::mcp_server_status)
		  AND ($3 = '' OR name ILIKE '%' || $3 || '%')
		ORDER BY created_at DESC
	`, orgID, statusFilter, query)
	if err != nil {
		return nil, fmt.Errorf("controlplane: list mcp_servers: %w", err)
	}
	defer rows.Close()

	var out []*models.MCPServer
	for rows.Next() {
		srv := &models.MCPServer{OrganizationID: orgID}
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Slug, &srv.Description, &srv.CustomInstructions, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt); err != nil {
			return nil, fmt.Errorf("controlplane: scan mcp_server: %w", err)
		}
		srv.Endpoint = s.publicBase + "/" + srv.Slug
		out = append(out, srv)
	}
	if err := s.hydrate(ctx, out); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func (s *MCPServerService) Get(ctx context.Context, orgID, id int64) (*models.MCPServer, error) {
	srv := &models.MCPServer{OrganizationID: orgID, ID: id}
	err := s.pool.QueryRow(ctx, `
		SELECT name, slug, description, custom_instructions, status, created_at, updated_at
		FROM mcp_servers WHERE organization_id = $1 AND id = $2
	`, orgID, id).Scan(&srv.Name, &srv.Slug, &srv.Description, &srv.CustomInstructions, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: get mcp_server: %w", err)
	}
	srv.Endpoint = s.publicBase + "/" + srv.Slug
	if err := s.hydrate(ctx, []*models.MCPServer{srv}); err != nil {
		return nil, err
	}
	return srv, nil
}

// hydrate fills the read-time aggregates (connectorIds, apiKeyCount) that
// APIDOC.md's Server shape expects but that aren't columns on mcp_servers.
func (s *MCPServerService) hydrate(ctx context.Context, servers []*models.MCPServer) error {
	for _, srv := range servers {
		rows, err := s.pool.Query(ctx, `
			SELECT DISTINCT a.connector_id
			FROM mcp_server_tool_groups g
			JOIN mcp_tools t ON t.group_id = g.tool_group_id
			JOIN connector_apis a ON a.id = t.connector_api_id
			WHERE g.mcp_server_id = $1
		`, srv.ID)
		if err != nil {
			return fmt.Errorf("controlplane: hydrate connector ids: %w", err)
		}
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return err
			}
			srv.ConnectorIDs = append(srv.ConnectorIDs, cid)
		}
		rows.Close()

		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_server_api_keys WHERE mcp_server_id = $1 AND revoked_at IS NULL`, srv.ID).Scan(&srv.APIKeyCount); err != nil {
			return fmt.Errorf("controlplane: hydrate api key count: %w", err)
		}
	}
	return nil
}

type UpdateServerInput struct {
	Name               *string
	Description        *string
	CustomInstructions *string
	Status             *models.MCPServerStatus
}

func (s *MCPServerService) Update(ctx context.Context, orgID, id int64, in UpdateServerInput) (*models.MCPServer, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mcp_servers SET
			name = coalesce($3, name),
			description = coalesce($4, description),
			custom_instructions = coalesce($5, custom_instructions),
			status = coalesce($6, status),
			updated_at = now()
		WHERE organization_id = $1 AND id = $2
	`, orgID, id, in.Name, in.Description, in.CustomInstructions, in.Status)
	if err != nil {
		return nil, fmt.Errorf("controlplane: update mcp_server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, orgID, id)
}

func (s *MCPServerService) Delete(ctx context.Context, orgID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mcp_servers WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return fmt.Errorf("controlplane: delete mcp_server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetBySlug is what the MCP runtime routes use to resolve the public
// `/mcp/:slug/...` path — no organization scoping at this layer since the
// API key itself (checked separately) is what proves tenancy for runtime
// calls, matching APIDOC.md's note that the MCP runtime uses a
// gh_live_/gh_test_ key, not the admin JWT.
func (s *MCPServerService) GetBySlug(ctx context.Context, slug string) (*models.MCPServer, error) {
	srv := &models.MCPServer{Slug: slug}
	err := s.pool.QueryRow(ctx, `
		SELECT id, organization_id, name, description, custom_instructions, status, created_at, updated_at
		FROM mcp_servers WHERE slug = $1
	`, slug).Scan(&srv.ID, &srv.OrganizationID, &srv.Name, &srv.Description, &srv.CustomInstructions, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return srv, err
}

// --- API keys -----------------------------------------------------------

// CreateAPIKey generates a new gh_live_/gh_test_-prefixed key, returning the
// full secret once — only its prefix and a sha256 hash are persisted.
func (s *MCPServerService) CreateAPIKey(ctx context.Context, mcpServerID int64, label string, live bool) (fullKey string, key *models.MCPServerAPIKey, err error) {
	secretBytes := make([]byte, 24)
	if _, err = rand.Read(secretBytes); err != nil {
		return "", nil, fmt.Errorf("controlplane: generate api key: %w", err)
	}
	prefix := "gh_test_"
	if live {
		prefix = "gh_live_"
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	fullKey = prefix + secret
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])

	key = &models.MCPServerAPIKey{MCPServerID: mcpServerID, Label: label, KeyPrefix: prefix + secret[:6]}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO mcp_server_api_keys (mcp_server_id, label, key_prefix, key_hash)
		VALUES ($1,$2,$3,$4)
		RETURNING id, created_at
	`, mcpServerID, label, key.KeyPrefix, hashHex).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return "", nil, fmt.Errorf("controlplane: create api key: %w", err)
	}
	return fullKey, key, nil
}

func (s *MCPServerService) ListAPIKeys(ctx context.Context, mcpServerID int64) ([]*models.MCPServerAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, label, key_prefix, created_at, revoked_at
		FROM mcp_server_api_keys WHERE mcp_server_id = $1 ORDER BY created_at DESC
	`, mcpServerID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: list api keys: %w", err)
	}
	defer rows.Close()

	var out []*models.MCPServerAPIKey
	for rows.Next() {
		k := &models.MCPServerAPIKey{MCPServerID: mcpServerID}
		if err := rows.Scan(&k.ID, &k.Label, &k.KeyPrefix, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, fmt.Errorf("controlplane: scan api key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *MCPServerService) RevokeAPIKey(ctx context.Context, mcpServerID, keyID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mcp_server_api_keys SET revoked_at = now()
		WHERE mcp_server_id = $1 AND id = $2 AND revoked_at IS NULL
	`, mcpServerID, keyID)
	if err != nil {
		return fmt.Errorf("controlplane: revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// VerifyAPIKey resolves a full presented key to its owning server, for the
// MCP runtime's own auth (separate from the admin JWT session).
func (s *MCPServerService) VerifyAPIKey(ctx context.Context, fullKey string) (*models.MCPServer, error) {
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])

	srv := &models.MCPServer{}
	err := s.pool.QueryRow(ctx, `
		SELECT s.id, s.organization_id, s.name, s.slug, s.status
		FROM mcp_server_api_keys k
		JOIN mcp_servers s ON s.id = k.mcp_server_id
		WHERE k.key_hash = $1 AND k.revoked_at IS NULL
	`, hashHex).Scan(&srv.ID, &srv.OrganizationID, &srv.Name, &srv.Slug, &srv.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("controlplane: api key invalid or revoked")
	}
	return srv, err
}
