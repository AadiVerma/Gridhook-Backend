package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/models"
)

type MCPServerService struct {
	db         *gorm.DB
	publicBase string
}

func NewMCPServerService(gdb *gorm.DB, publicBaseURL string) *MCPServerService {
	return &MCPServerService{db: gdb, publicBase: publicBaseURL}
}

type CreateServerInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

func (s *MCPServerService) Create(ctx context.Context, orgID int64, in CreateServerInput) (*models.MCPServer, error) {
	slug := in.Slug
	if slug == "" {

		var err error
		if slug, err = s.uniqueSlug(ctx, orgID, in.Name); err != nil {
			return nil, err
		}
	}

	srv := &models.MCPServer{
		OrganizationID: orgID,
		Name:           in.Name,
		Slug:           slug,
		Description:    in.Description,
		Status:         models.ServerStopped,
	}
	if err := s.db.WithContext(ctx).Create(srv).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create mcp_server: %w", err)
	}
	srv.Endpoint = s.endpoint(srv.Slug)
	return srv, nil
}

func (s *MCPServerService) uniqueSlug(ctx context.Context, orgID int64, name string) (string, error) {
	base := slugify(name)
	slug := base
	for attempt := 2; ; attempt++ {
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.MCPServer{}).
			Where("organization_id = ? AND slug = ?", orgID, slug).Count(&count).Error; err != nil {
			return "", fmt.Errorf("controlplane: create mcp_server: generate slug: %w", err)
		}
		if count == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, attempt)
	}
}

func (s *MCPServerService) endpoint(slug string) string {
	return s.publicBase + "/" + slug
}

func (s *MCPServerService) List(ctx context.Context, orgID int64, statusFilter, query string) ([]*models.MCPServer, error) {
	tx := s.db.WithContext(ctx).Where("organization_id = ?", orgID)
	if statusFilter != "" {
		tx = tx.Where("status = ?", statusFilter)
	}
	if query != "" {
		tx = tx.Where("name ILIKE ?", "%"+query+"%")
	}

	var servers []*models.MCPServer
	if err := tx.Order("created_at DESC").Find(&servers).Error; err != nil {
		return nil, fmt.Errorf("controlplane: list mcp_servers: %w", err)
	}
	if err := s.hydrate(ctx, servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func (s *MCPServerService) Get(ctx context.Context, orgID, id int64) (*models.MCPServer, error) {
	srv := &models.MCPServer{}
	err := s.db.WithContext(ctx).Where("organization_id = ? AND id = ?", orgID, id).First(srv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: get mcp_server: %w", err)
	}
	if err := s.hydrate(ctx, []*models.MCPServer{srv}); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *MCPServerService) hydrate(ctx context.Context, servers []*models.MCPServer) error {
	if len(servers) == 0 {
		return nil
	}

	ids := make([]int64, len(servers))
	byID := make(map[int64]*models.MCPServer, len(servers))
	for i, srv := range servers {
		ids[i] = srv.ID
		byID[srv.ID] = srv
		srv.Endpoint = s.endpoint(srv.Slug)
		srv.ConnectorIDs = []int64{}
		srv.ToolGroupIDs = []int64{}
	}

	var connectorRows []struct {
		MCPServerID int64 `gorm:"column:mcp_server_id"`
		ConnectorID int64 `gorm:"column:connector_id"`
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT g.mcp_server_id, a.connector_id
		FROM mcp_server_tool_groups g
		JOIN mcp_tools t ON t.group_id = g.tool_group_id
		JOIN connector_apis a ON a.id = t.connector_api_id
		WHERE g.mcp_server_id IN ?
	`, ids).Scan(&connectorRows).Error
	if err != nil {
		return fmt.Errorf("controlplane: hydrate connector ids: %w", err)
	}
	for _, row := range connectorRows {
		if srv, ok := byID[row.MCPServerID]; ok {
			srv.ConnectorIDs = append(srv.ConnectorIDs, row.ConnectorID)
		}
	}

	var groupRows []struct {
		MCPServerID int64 `gorm:"column:mcp_server_id"`
		ToolGroupID int64 `gorm:"column:tool_group_id"`
	}
	err = s.db.WithContext(ctx).Model(&models.MCPServerToolGroup{}).
		Select("mcp_server_id, tool_group_id").
		Where("mcp_server_id IN ?", ids).
		Scan(&groupRows).Error
	if err != nil {
		return fmt.Errorf("controlplane: hydrate tool group ids: %w", err)
	}
	for _, row := range groupRows {
		if srv, ok := byID[row.MCPServerID]; ok {
			srv.ToolGroupIDs = append(srv.ToolGroupIDs, row.ToolGroupID)
		}
	}

	var keyRows []struct {
		MCPServerID int64 `gorm:"column:mcp_server_id"`
		Count       int   `gorm:"column:count"`
	}
	err = s.db.WithContext(ctx).Model(&models.MCPServerAPIKey{}).
		Select("mcp_server_id, count(*) AS count").
		Where("mcp_server_id IN ? AND revoked_at IS NULL", ids).
		Group("mcp_server_id").
		Scan(&keyRows).Error
	if err != nil {
		return fmt.Errorf("controlplane: hydrate api key count: %w", err)
	}
	for _, row := range keyRows {
		if srv, ok := byID[row.MCPServerID]; ok {
			srv.APIKeyCount = row.Count
		}
	}
	return nil
}

type UpdateServerInput struct {
	Name               *string                 `json:"name"`
	Description        *string                 `json:"description"`
	CustomInstructions *string                 `json:"customInstructions"`
	Status             *models.MCPServerStatus `json:"status"`
}

func (s *MCPServerService) Update(ctx context.Context, orgID, id int64, in UpdateServerInput) (*models.MCPServer, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if in.CustomInstructions != nil {
		updates["custom_instructions"] = *in.CustomInstructions
	}
	if in.Status != nil {
		if *in.Status != models.ServerRunning && *in.Status != models.ServerStopped {
			return nil, fmt.Errorf("%w: status must be %q or %q", ErrValidation, models.ServerRunning, models.ServerStopped)
		}
		updates["status"] = *in.Status
	}
	updates["updated_at"] = gorm.Expr("now()")

	tx := s.db.WithContext(ctx).Model(&models.MCPServer{}).
		Where("organization_id = ? AND id = ?", orgID, id).Updates(updates)
	if tx.Error != nil {
		return nil, fmt.Errorf("controlplane: update mcp_server: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, orgID, id)
}

func (s *MCPServerService) Delete(ctx context.Context, orgID, id int64) error {
	tx := s.db.WithContext(ctx).Where("organization_id = ? AND id = ?", orgID, id).Delete(&models.MCPServer{})
	if tx.Error != nil {
		return fmt.Errorf("controlplane: delete mcp_server: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MCPServerService) assertServerInOrg(ctx context.Context, orgID, mcpServerID int64) error {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.MCPServer{}).
		Where("id = ? AND organization_id = ?", mcpServerID, orgID).Count(&count).Error
	if err != nil {
		return fmt.Errorf("controlplane: verify mcp_server ownership: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

const apiKeySecretBytes = 24

func (s *MCPServerService) CreateAPIKey(ctx context.Context, orgID, mcpServerID int64, label string, live bool) (string, *models.MCPServerAPIKey, error) {
	if err := s.assertServerInOrg(ctx, orgID, mcpServerID); err != nil {
		return "", nil, err
	}

	secretBytes := make([]byte, apiKeySecretBytes)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", nil, fmt.Errorf("controlplane: generate api key: %w", err)
	}
	prefix := "gh_test_"
	if live {
		prefix = "gh_live_"
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	fullKey := prefix + secret
	hash := sha256.Sum256([]byte(fullKey))

	key := &models.MCPServerAPIKey{
		MCPServerID: mcpServerID,
		Label:       label,
		KeyPrefix:   prefix + secret[:6],
		KeyHash:     hex.EncodeToString(hash[:]),
	}
	if err := s.db.WithContext(ctx).Create(key).Error; err != nil {
		return "", nil, fmt.Errorf("controlplane: create api key: %w", err)
	}
	return fullKey, key, nil
}

func (s *MCPServerService) ListAPIKeys(ctx context.Context, orgID, mcpServerID int64) ([]*models.MCPServerAPIKey, error) {
	if err := s.assertServerInOrg(ctx, orgID, mcpServerID); err != nil {
		return nil, err
	}
	var keys []*models.MCPServerAPIKey
	err := s.db.WithContext(ctx).Where("mcp_server_id = ?", mcpServerID).
		Order("created_at DESC").Find(&keys).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list api keys: %w", err)
	}
	return keys, nil
}

func (s *MCPServerService) RevokeAPIKey(ctx context.Context, orgID, mcpServerID, keyID int64) error {
	tx := s.db.WithContext(ctx).Model(&models.MCPServerAPIKey{}).
		Where("id = ? AND mcp_server_id = ? AND revoked_at IS NULL", keyID, mcpServerID).
		Where("mcp_server_id IN (SELECT id FROM mcp_servers WHERE organization_id = ?)", orgID).
		Update("revoked_at", gorm.Expr("now()"))
	if tx.Error != nil {
		return fmt.Errorf("controlplane: revoke api key: %w", tx.Error)
	}
	if tx.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MCPServerService) VerifyAPIKey(ctx context.Context, fullKey string) (*models.MCPServer, error) {
	hash := sha256.Sum256([]byte(fullKey))

	srv := &models.MCPServer{}
	err := s.db.WithContext(ctx).Model(&models.MCPServer{}).
		Joins("JOIN mcp_server_api_keys k ON k.mcp_server_id = mcp_servers.id").
		Where("k.key_hash = ? AND k.revoked_at IS NULL", hex.EncodeToString(hash[:])).
		First(srv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("controlplane: api key invalid or revoked: %w", ErrUnauthorized)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: verify api key: %w", err)
	}
	srv.Endpoint = s.endpoint(srv.Slug)
	return srv, nil
}
