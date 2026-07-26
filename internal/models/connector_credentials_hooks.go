package models

import (
	"fmt"

	"gorm.io/gorm"

	"gridhook.dev/connector-backend/internal/db"
)

const sealerSessionKey = "connector_credentials_sealer"

func WithSealer(tx *gorm.DB, sealer db.Sealer) *gorm.DB {
	return tx.Set(sealerSessionKey, sealer)
}

func sealerFromSession(tx *gorm.DB) (db.Sealer, error) {
	v, ok := tx.Get(sealerSessionKey)
	if !ok {
		return nil, fmt.Errorf("models: connector_credentials accessed without WithSealer on the session")
	}
	sealer, ok := v.(db.Sealer)
	if !ok {
		return nil, fmt.Errorf("models: session key %q is not a db.Sealer", sealerSessionKey)
	}
	return sealer, nil
}

func (c *ConnectorCredentials) BeforeSave(tx *gorm.DB) error {
	sealer, err := sealerFromSession(tx)
	if err != nil {
		return err
	}
	var sealErr error
	seal := func(plaintext string) string {
		if plaintext == "" || sealErr != nil {
			return plaintext
		}
		sealed, err := sealer.Seal(plaintext)
		if err != nil {
			sealErr = err
			return plaintext
		}
		return sealed
	}
	c.ClientSecret = seal(c.ClientSecret)
	c.BearerToken = seal(c.BearerToken)
	c.APIKeyValue = seal(c.APIKeyValue)
	c.BasicPassword = seal(c.BasicPassword)
	return sealErr
}

func (c *ConnectorCredentials) AfterFind(tx *gorm.DB) error {
	sealer, err := sealerFromSession(tx)
	if err != nil {
		return err
	}
	var openErr error
	open := func(ciphertext string) string {
		if ciphertext == "" || openErr != nil {
			return ciphertext
		}
		plain, err := sealer.Open(ciphertext)
		if err != nil {
			openErr = err
			return ciphertext
		}
		return plain
	}
	c.ClientSecret = open(c.ClientSecret)
	c.BearerToken = open(c.BearerToken)
	c.APIKeyValue = open(c.APIKeyValue)
	c.BasicPassword = open(c.BasicPassword)
	return openErr
}
