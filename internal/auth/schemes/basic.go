package schemes

import (
	"context"
	"encoding/base64"
	"fmt"

	"gridhook.dev/connector-backend/internal/models"
)

type BasicScheme struct{}

func (BasicScheme) Resolve(_ context.Context, creds *models.ConnectorCredentials) (Credentials, error) {
	if creds.BasicUsername == "" {
		return Credentials{}, fmt.Errorf("schemes: basic credentials missing username")
	}
	raw := creds.BasicUsername + ":" + creds.BasicPassword
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	return Credentials{
		Headers: map[string]string{"Authorization": "Basic " + encoded},
	}, nil
}
