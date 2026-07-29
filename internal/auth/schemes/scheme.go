package schemes

import (
	"context"
	"errors"

	"gridhook.dev/connector-backend/internal/models"
)

var ErrIncompleteCredentials = errors.New("schemes: credentials incomplete")

type Credentials struct {
	Headers map[string]string

	QueryParams map[string]string

	ExpiresInSeconds int
}

func (c Credentials) String() string {
	return "schemes.Credentials{redacted}"
}

type Scheme interface {
	Resolve(ctx context.Context, creds *models.ConnectorCredentials) (Credentials, error)
}
