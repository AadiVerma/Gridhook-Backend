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
	if err := s.db.WithContext(ctx).Create(srv).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create mcp_server: %w", err)
	}
	srv.Endpoint = s.publicBase + "/" + srv.Slug
	return srv, nil
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
	for _, srv := range servers {
		srv.Endpoint = s.publicBase + "/" + srv.Slug
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
	srv.Endpoint = s.publicBase + "/" + srv.Slug
	if err := s.hydrate(ctx, []*models.MCPServer{srv}); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *MCPServerService) hydrate(ctx context.Context, servers []*models.MCPServer) error {
	for _, srv := range servers {
		var ids []int64
		err := s.db.WithContext(ctx).Model(&models.MCPTool{}).Distinct().
			Joins("JOIN mcp_server_tool_groups g ON g.tool_group_id = mcp_tools.group_id").
			Joins("JOIN connector_apis a ON a.id = mcp_tools.connector_api_id").
			Where("g.mcp_server_id = ?", srv.ID).
			Pluck("a.connector_id", &ids).Error
		if err != nil {
			return fmt.Errorf("controlplane: hydrate connector ids: %w", err)
		}
		srv.ConnectorIDs = ids

		var count int64
		if err := s.db.WithContext(ctx).Model(&models.MCPServerAPIKey{}).
			Where("mcp_server_id = ? AND revoked_at IS NULL", srv.ID).
			Count(&count).Error; err != nil {
			return fmt.Errorf("controlplane: hydrate api key count: %w", err)
		}
		srv.APIKeyCount = int(count)
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
		updates["status"] = *in.Status
	}
	updates["updated_at"] = gorm.Expr("now()")

	tx := s.db.WithContext(ctx).Model(&models.MCPServer{}).Where("organization_id = ? AND id = ?", orgID, id).Updates(updates)
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

func (s *MCPServerService) GetBySlug(ctx context.Context, slug string) (*models.MCPServer, error) {
	srv := &models.MCPServer{}
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(srv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return srv, err
}

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

	key = &models.MCPServerAPIKey{MCPServerID: mcpServerID, Label: label, KeyPrefix: prefix + secret[:6], KeyHash: hashHex}
	if err := s.db.WithContext(ctx).Create(key).Error; err != nil {
		return "", nil, fmt.Errorf("controlplane: create api key: %w", err)
	}
	return fullKey, key, nil
}

func (s *MCPServerService) ListAPIKeys(ctx context.Context, mcpServerID int64) ([]*models.MCPServerAPIKey, error) {
	var keys []*models.MCPServerAPIKey
	if err := s.db.WithContext(ctx).Where("mcp_server_id = ?", mcpServerID).Order("created_at DESC").Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("controlplane: list api keys: %w", err)
	}
	return keys, nil
}

func (s *MCPServerService) RevokeAPIKey(ctx context.Context, mcpServerID, keyID int64) error {
	tx := s.db.WithContext(ctx).Model(&models.MCPServerAPIKey{}).
		Where("mcp_server_id = ? AND id = ? AND revoked_at IS NULL", mcpServerID, keyID).
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
	hashHex := hex.EncodeToString(hash[:])

	srv := &models.MCPServer{}
	err := s.db.WithContext(ctx).
		Joins("JOIN mcp_server_api_keys k ON k.mcp_server_id = mcp_servers.id").
		Where("k.key_hash = ? AND k.revoked_at IS NULL", hashHex).
		First(srv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("controlplane: api key invalid or revoked")
	}
	return srv, err
}
