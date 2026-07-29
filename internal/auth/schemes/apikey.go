package schemes

import (
	"context"
	"fmt"

	"gridhook.dev/connector-backend/internal/models"
)

type APIKeyScheme struct{}

func (APIKeyScheme) Resolve(_ context.Context, creds *models.ConnectorCredentials) (Credentials, error) {
	if creds.APIKeyValue == "" {
		return Credentials{}, fmt.Errorf("%w: api_key needs api_key_value", ErrIncompleteCredentials)
	}
	header := creds.APIKeyHeader
	if header == "" {
		header = "X-API-Key"
	}
	return Credentials{
		Headers: map[string]string{header: creds.APIKeyValue},
	}, nil
}
