package schemes

import (
	"context"

	"gridhook.dev/connector-backend/internal/models"
)

type Credentials struct {
	Headers          map[string]string
	QueryParams      map[string]string
	ExpiresInSeconds int
}

type Scheme interface {
	Resolve(ctx context.Context, creds *models.ConnectorCredentials) (Credentials, error)
}
