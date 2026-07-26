package schemes

import (
	"context"
	"fmt"

	"gridhook.dev/connector-backend/internal/models"
)

// APIKeyScheme attaches a static key as a header (default) — the header
// name is configurable per connector since upstreams vary ("X-API-Key",
// "Ocp-Apim-Subscription-Key", etc).
type APIKeyScheme struct{}

func (APIKeyScheme) Resolve(_ context.Context, creds *models.ConnectorCredentials) (Credentials, error) {
	if creds.APIKeyValue == "" {
		return Credentials{}, fmt.Errorf("schemes: api_key credentials missing value")
	}
	header := creds.APIKeyHeader
	if header == "" {
		header = "X-API-Key"
	}
	return Credentials{
		Headers: map[string]string{header: creds.APIKeyValue},
	}, nil
}
