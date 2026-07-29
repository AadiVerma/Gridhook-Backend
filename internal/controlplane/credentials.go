package controlplane

import (
	"fmt"

	"gridhook.dev/connector-backend/internal/models"
	"gridhook.dev/connector-backend/internal/secrets"
)

func secretFields(c *models.ConnectorCredentials) []*string {
	return []*string{
		&c.ClientSecret,
		&c.BearerToken,
		&c.APIKeyValue,
		&c.BasicPassword,
	}
}

func sealCredentials(sealer secrets.Sealer, in models.ConnectorCredentials) (models.ConnectorCredentials, error) {
	out := in
	for _, field := range secretFields(&out) {
		sealed, err := sealer.Seal(*field)
		if err != nil {
			return models.ConnectorCredentials{}, fmt.Errorf("controlplane: seal credential: %w", err)
		}
		*field = sealed
	}
	return out, nil
}

func openCredentials(sealer secrets.Sealer, c *models.ConnectorCredentials) error {
	for _, field := range secretFields(c) {
		plaintext, err := sealer.Open(*field)
		if err != nil {

			return fmt.Errorf("controlplane: open credential for connector_api %d: %w", c.ConnectorAPIID, err)
		}
		*field = plaintext
	}
	return nil
}
