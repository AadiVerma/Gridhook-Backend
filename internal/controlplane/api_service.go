package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gridhook.dev/connector-backend/internal/db"
	"gridhook.dev/connector-backend/internal/models"
)

// APIService owns connector_apis and their credentials — the layer added on
// top of trd.md's design so one connector can bundle several protocol
// endpoints. It also implements auth.CredentialsStore, so the auth broker
// can load decrypted credentials without depending on controlplane's other
// concerns (connector/tool CRUD).
type APIService struct {
	pool   *pgxpool.Pool
	sealer db.Sealer
}

func NewAPIService(pool *pgxpool.Pool, sealer db.Sealer) *APIService {
	return &APIService{pool: pool, sealer: sealer}
}

type CreateAPIInput struct {
	Name       string
	EngineType models.EngineType
	BaseURL    string
	AuthType   models.AuthType
	SpecURL    string
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
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO connector_apis (connector_id, name, engine_type, base_url, auth_type, spec_url, is_active)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),true)
		RETURNING id, created_at, updated_at
	`, connectorID, in.Name, in.EngineType, in.BaseURL, in.AuthType, in.SpecURL).Scan(&api.ID, &api.CreatedAt, &api.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("controlplane: create connector_api: %w", err)
	}
	return api, nil
}

func (s *APIService) ListByConnector(ctx context.Context, connectorID int64) ([]*models.ConnectorAPI, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, connector_id, name, engine_type, base_url, auth_type, coalesce(spec_url,''), is_active, created_at, updated_at
		FROM connector_apis WHERE connector_id = $1 ORDER BY created_at
	`, connectorID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: list connector_apis: %w", err)
	}
	defer rows.Close()

	var out []*models.ConnectorAPI
	for rows.Next() {
		a := &models.ConnectorAPI{}
		if err := rows.Scan(&a.ID, &a.ConnectorID, &a.Name, &a.EngineType, &a.BaseURL, &a.AuthType, &a.SpecURL, &a.IsActive, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("controlplane: scan connector_api: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *APIService) Get(ctx context.Context, id int64) (*models.ConnectorAPI, error) {
	a := &models.ConnectorAPI{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, connector_id, name, engine_type, base_url, auth_type, coalesce(spec_url,''), is_active, created_at, updated_at
		FROM connector_apis WHERE id = $1
	`, id).Scan(&a.ID, &a.ConnectorID, &a.Name, &a.EngineType, &a.BaseURL, &a.AuthType, &a.SpecURL, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// PutCredentials upserts the secret material for one connector_api,
// encrypting the sensitive columns with the configured Sealer before they
// ever reach the database. Called from a write-only admin endpoint — these
// values are never read back out over the API.
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
	sealedSecret, err := s.sealIfSet(in.ClientSecret)
	if err != nil {
		return err
	}
	sealedBearer, err := s.sealIfSet(in.BearerToken)
	if err != nil {
		return err
	}
	sealedAPIKey, err := s.sealIfSet(in.APIKeyValue)
	if err != nil {
		return err
	}
	sealedBasicPW, err := s.sealIfSet(in.BasicPassword)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO connector_credentials
			(connector_api_id, auth_type, token_url, client_id, client_secret, bearer_token,
			 api_key_header, api_key_value, basic_username, basic_password, headers)
		VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,NULLIF($7,''),$8,NULLIF($9,''),$10,$11)
		ON CONFLICT (connector_api_id) DO UPDATE SET
			auth_type = EXCLUDED.auth_type,
			token_url = EXCLUDED.token_url,
			client_id = EXCLUDED.client_id,
			client_secret = EXCLUDED.client_secret,
			bearer_token = EXCLUDED.bearer_token,
			api_key_header = EXCLUDED.api_key_header,
			api_key_value = EXCLUDED.api_key_value,
			basic_username = EXCLUDED.basic_username,
			basic_password = EXCLUDED.basic_password,
			headers = EXCLUDED.headers,
			updated_at = now()
	`, connectorAPIID, in.AuthType, in.TokenURL, in.ClientID, sealedSecret, sealedBearer,
		in.APIKeyHeader, sealedAPIKey, in.BasicUsername, sealedBasicPW, in.Headers)
	if err != nil {
		return fmt.Errorf("controlplane: put credentials: %w", err)
	}
	return nil
}

func (s *APIService) sealIfSet(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	return s.sealer.Seal(plaintext)
}

// LoadCredentials implements auth.CredentialsStore: load + decrypt.
func (s *APIService) LoadCredentials(ctx context.Context, connectorAPIID int64) (*models.ConnectorCredentials, error) {
	c := &models.ConnectorCredentials{}
	var clientSecret, bearerToken, apiKeyValue, basicPassword string
	err := s.pool.QueryRow(ctx, `
		SELECT connector_api_id, auth_type, coalesce(token_url,''), coalesce(client_id,''), coalesce(client_secret,''),
		       coalesce(bearer_token,''), coalesce(api_key_header,''), coalesce(api_key_value,''),
		       coalesce(basic_username,''), coalesce(basic_password,'')
		FROM connector_credentials WHERE connector_api_id = $1
	`, connectorAPIID).Scan(&c.ConnectorAPIID, &c.AuthType, &c.TokenURL, &c.ClientID, &clientSecret,
		&bearerToken, &c.APIKeyHeader, &apiKeyValue, &c.BasicUsername, &basicPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("controlplane: no credentials configured for connector_api %d", connectorAPIID)
	}
	if err != nil {
		return nil, fmt.Errorf("controlplane: load credentials: %w", err)
	}

	if c.ClientSecret, err = s.openIfSet(clientSecret); err != nil {
		return nil, err
	}
	if c.BearerToken, err = s.openIfSet(bearerToken); err != nil {
		return nil, err
	}
	if c.APIKeyValue, err = s.openIfSet(apiKeyValue); err != nil {
		return nil, err
	}
	if c.BasicPassword, err = s.openIfSet(basicPassword); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *APIService) openIfSet(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	return s.sealer.Open(ciphertext)
}
