package controlplane

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gridhook.dev/connector-backend/internal/db"
	"gridhook.dev/connector-backend/internal/models"
)

type APIService struct {
	db     *gorm.DB
	sealer db.Sealer
}

func NewAPIService(gdb *gorm.DB, sealer db.Sealer) *APIService {
	return &APIService{db: gdb, sealer: sealer}
}

type CreateAPIInput struct {
	Name       string
	EngineType models.EngineType
	BaseURL    string
	AuthType   models.AuthType
	SpecURL    string
	GroupID    *int64
}

func (s *APIService) Create(ctx context.Context, connectorID int64, in CreateAPIInput) (*models.ConnectorAPI, error) {
	api := &models.ConnectorAPI{
		ConnectorID: connectorID,
		Name:        in.Name,
		EngineType:  in.EngineType,
		BaseURL:     in.BaseURL,
		AuthType:    in.AuthType,
		SpecURL:     in.SpecURL,
		IsActive:    true,
		GroupID:     in.GroupID,
	}
	if err := s.db.WithContext(ctx).Create(api).Error; err != nil {
		return nil, fmt.Errorf("controlplane: create connector_api: %w", err)
	}
	return api, nil
}

func (s *APIService) ListByConnector(ctx context.Context, connectorID int64) ([]*models.ConnectorAPI, error) {
	var apis []*models.ConnectorAPI
	if err := s.db.WithContext(ctx).Where("connector_id = ?", connectorID).Order("created_at").Find(&apis).Error; err != nil {
		return nil, fmt.Errorf("controlplane: list connector_apis: %w", err)
	}
	return apis, nil
}

func (s *APIService) ListByGroup(ctx context.Context, connectorID, groupID int64) ([]*models.ConnectorAPI, error) {
	var apis []*models.ConnectorAPI
	err := s.db.WithContext(ctx).Where("connector_id = ? AND group_id = ?", connectorID, groupID).
		Order("created_at").Find(&apis).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list connector_apis by group: %w", err)
	}
	return apis, nil
}

func (s *APIService) Get(ctx context.Context, id int64) (*models.ConnectorAPI, error) {
	api := &models.ConnectorAPI{}
	err := s.db.WithContext(ctx).Where("id = ?", id).First(api).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return api, err
}

type UpdateAPIInput struct {
	Name     *string
	BaseURL  *string
	AuthType *models.AuthType
	IsActive *bool
	GroupID  **int64
}

func (s *APIService) Update(ctx context.Context, id int64, in UpdateAPIInput) (*models.ConnectorAPI, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.BaseURL != nil {
		updates["base_url"] = *in.BaseURL
	}
	if in.AuthType != nil {
		updates["auth_type"] = *in.AuthType
	}
	if in.IsActive != nil {
		updates["is_active"] = *in.IsActive
	}
	if in.GroupID != nil {
		updates["group_id"] = *in.GroupID
	}
	if len(updates) == 0 {
		return s.Get(ctx, id)
	}
	updates["updated_at"] = gorm.Expr("now()")

	res := s.db.WithContext(ctx).Model(&models.ConnectorAPI{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, fmt.Errorf("controlplane: update connector_api: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

type PutCredentialsInput struct {
	AuthType      models.AuthType
	TokenURL      string
	ClientID      string
	ClientSecret  string
	BearerToken   string
	APIKeyHeader  string
	APIKeyValue   string
	BasicUsername string
	BasicPassword string
	Headers       map[string]any
}

func (s *APIService) PutCredentials(ctx context.Context, connectorAPIID int64, in PutCredentialsInput) error {
	authType := in.AuthType
	if authType == "" {
		api, err := s.Get(ctx, connectorAPIID)
		if err != nil {
			return fmt.Errorf("controlplane: put credentials: resolve auth type: %w", err)
		}
		authType = api.AuthType
	}
	cred := &models.ConnectorCredentials{
		ConnectorAPIID: connectorAPIID,
		AuthType:       authType,
		TokenURL:       in.TokenURL,
		ClientID:       in.ClientID,
		ClientSecret:   in.ClientSecret,
		BearerToken:    in.BearerToken,
		APIKeyHeader:   in.APIKeyHeader,
		APIKeyValue:    in.APIKeyValue,
		BasicUsername:  in.BasicUsername,
		BasicPassword:  in.BasicPassword,
		Headers:        in.Headers,
	}
	updates := clause.AssignmentColumns([]string{
		"auth_type", "token_url", "client_id", "client_secret", "bearer_token",
		"api_key_header", "api_key_value", "basic_username", "basic_password", "headers",
	})
	updates = append(updates, clause.Assignment{Column: clause.Column{Name: "updated_at"}, Value: gorm.Expr("now()")})

	err := models.WithSealer(s.db.WithContext(ctx), s.sealer).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "connector_api_id"}},
		DoUpdates: updates,
	}).Create(cred).Error
	if err != nil {
		return fmt.Errorf("controlplane: put credentials: %w", err)
	}
	return nil
}

func (s *APIService) LoadCredentials(ctx context.Context, connectorAPIID int64) (*models.ConnectorCredentials, error) {
	c := &models.ConnectorCredentials{}
	err := models.WithSealer(s.db.WithContext(ctx), s.sealer).Where("connector_api_id = ?", connectorAPIID).First(c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("controlplane: no credentials configured for connector_api %d", connectorAPIID)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: load credentials: %w", err)
	}
	return c, nil
}
