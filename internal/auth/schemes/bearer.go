package schemes

import (
	"context"
	"fmt"

	"gridhook.dev/connector-backend/internal/models"
)

type BearerScheme struct{}

func (BearerScheme) Resolve(_ context.Context, creds *models.ConnectorCredentials) (Credentials, error) {
	if creds.BearerToken == "" {
		return Credentials{}, fmt.Errorf("schemes: bearer credentials missing token")
	}
	return Credentials{
		Headers: map[string]string{"Authorization": "Bearer " + creds.BearerToken},
	}, nil
}
