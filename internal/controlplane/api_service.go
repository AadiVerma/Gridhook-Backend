package controlplane

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/secrets"
)

type APIService struct {
	db     *gorm.DB
	sealer secrets.Sealer
}

func NewAPIService(gdb *gorm.DB, sealer secrets.Sealer) *APIService {
	return &APIService{db: gdb, sealer: sealer}
}

func orgScopedAPIs(tx *gorm.DB, orgID int64) *gorm.DB {
	return tx.Where(
		"connector_apis.connector_id IN (SELECT id FROM connectors WHERE organization_id = ?)",
		orgID,
	)
}

type CreateAPIInput struct {
	Name       string
	EngineType models.EngineType
	BaseURL    string
	AuthType   models.AuthType
	SpecURL    string
	GroupID    *int64
}

func (s *APIService) Create(ctx context.Context, orgID, connectorID int64, in CreateAPIInput) (*models.ConnectorAPI, error) {
	if err := s.assertConnectorInOrg(ctx, orgID, connectorID); err != nil {
		return nil, err
	}
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

func (s *APIService) assertConnectorInOrg(ctx context.Context, orgID, connectorID int64) error {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.Connector{}).
		Where("id = ? AND organization_id = ?", connectorID, orgID).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("controlplane: verify connector ownership: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *APIService) ListByConnector(ctx context.Context, orgID, connectorID int64) ([]*models.ConnectorAPI, error) {
	var apis []*models.ConnectorAPI
	err := orgScopedAPIs(s.db.WithContext(ctx).Model(&models.ConnectorAPI{}), orgID).
		Where("connector_id = ?", connectorID).
		Order("created_at").
		Find(&apis).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list connector_apis: %w", err)
	}
	return apis, nil
}

func (s *APIService) ListByGroup(ctx context.Context, orgID, connectorID, groupID int64) ([]*models.ConnectorAPI, error) {
	var apis []*models.ConnectorAPI
	err := orgScopedAPIs(s.db.WithContext(ctx).Model(&models.ConnectorAPI{}), orgID).
		Where("connector_id = ? AND group_id = ?", connectorID, groupID).
		Order("created_at").
		Find(&apis).Error
	if err != nil {
		return nil, fmt.Errorf("controlplane: list connector_apis by group: %w", err)
	}
	return apis, nil
}

func (s *APIService) Get(ctx context.Context, orgID, id int64) (*models.ConnectorAPI, error) {
	api := &models.ConnectorAPI{}
	err := orgScopedAPIs(s.db.WithContext(ctx).Model(&models.ConnectorAPI{}), orgID).
		Where("connector_apis.id = ?", id).
		First(api).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: get connector_api: %w", err)
	}
	return api, nil
}

type UpdateAPIInput struct {
	Name     *string
	BaseURL  *string
	AuthType *models.AuthType
	IsActive *bool
	GroupID  **int64
}

func (s *APIService) Update(ctx context.Context, orgID, id int64, in UpdateAPIInput) (*models.ConnectorAPI, error) {
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
		return s.Get(ctx, orgID, id)
	}
	updates["updated_at"] = gorm.Expr("now()")

	res := orgScopedAPIs(s.db.WithContext(ctx).Model(&models.ConnectorAPI{}), orgID).
		Where("connector_apis.id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return nil, fmt.Errorf("controlplane: update connector_api: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, orgID, id)
}

type PutCredentialsInput struct {
	AuthType      models.AuthType `json:"authType"`
	TokenURL      string          `json:"tokenUrl"`
	ClientID      string          `json:"clientId"`
	ClientSecret  string          `json:"clientSecret"`
	BearerToken   string          `json:"bearerToken"`
	APIKeyHeader  string          `json:"apiKeyHeader"`
	APIKeyValue   string          `json:"apiKeyValue"`
	BasicUsername string          `json:"basicUsername"`
	BasicPassword string          `json:"basicPassword"`
	Headers       map[string]any  `json:"headers"`
}

func (s *APIService) PutCredentials(ctx context.Context, orgID, connectorAPIID int64, in PutCredentialsInput) error {

	api, err := s.Get(ctx, orgID, connectorAPIID)
	if err != nil {
		return err
	}

	authType := in.AuthType
	if authType == "" {
		authType = api.AuthType
	}

	cred, err := sealCredentials(s.sealer, models.ConnectorCredentials{
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
	})
	if err != nil {
		return err
	}

	assignments := clause.AssignmentColumns([]string{
		"auth_type", "token_url", "client_id", "client_secret", "bearer_token",
		"api_key_header", "api_key_value", "basic_username", "basic_password", "headers",
	})
	assignments = append(assignments, clause.Assignment{
		Column: clause.Column{Name: "updated_at"}, Value: gorm.Expr("now()"),
	})

	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "connector_api_id"}},
		DoUpdates: assignments,
	}).Create(&cred).Error
	if err != nil {
		return fmt.Errorf("controlplane: put credentials: %w", err)
	}
	return nil
}

func (s *APIService) LoadCredentials(ctx context.Context, connectorAPIID int64) (*models.ConnectorCredentials, error) {
	cred := &models.ConnectorCredentials{}
	err := s.db.WithContext(ctx).Where("connector_api_id = ?", connectorAPIID).First(cred).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("controlplane: no credentials configured for connector_api %d: %w", connectorAPIID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: load credentials: %w", err)
	}
	if err := openCredentials(s.sealer, cred); err != nil {
		return nil, err
	}
	return cred, nil
}
