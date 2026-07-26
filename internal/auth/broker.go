package auth

import (
	"context"
	"fmt"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

type CredentialsStore interface {
	LoadCredentials(ctx context.Context, connectorAPIID int64) (*models.ConnectorCredentials, error)
}

type Broker struct {
	store   CredentialsStore
	cache   TokenCache
	schemes map[models.AuthType]schemes.Scheme
}

func NewBroker(store CredentialsStore, cache TokenCache) *Broker {
	return &Broker{
		store: store,
		cache: cache,
		schemes: map[models.AuthType]schemes.Scheme{
			models.AuthBearer:     schemes.BearerScheme{},
			models.AuthAPIKey:     schemes.APIKeyScheme{},
			models.AuthBasic:      schemes.BasicScheme{},
			models.AuthOAuth2:     schemes.NewOAuth2Scheme(),
			models.AuthLoginToken: schemes.BearerScheme{},
		},
	}
}

func (b *Broker) Resolve(ctx context.Context, api *models.ConnectorAPI) (schemes.Credentials, error) {
	if api.AuthType == models.AuthNone {
		return schemes.Credentials{}, nil
	}

	if cached, ok := b.cache.Get(ctx, api.ID); ok {
		return cached, nil
	}

	scheme, ok := b.schemes[api.AuthType]
	if !ok {
		return schemes.Credentials{}, fmt.Errorf("auth: no scheme registered for auth type %q", api.AuthType)
	}

	credsRow, err := b.store.LoadCredentials(ctx, api.ID)
	if err != nil {
		return schemes.Credentials{}, fmt.Errorf("auth: load credentials for connector_api %d: %w", api.ID, err)
	}

	resolved, err := scheme.Resolve(ctx, credsRow)
	if err != nil {
		return schemes.Credentials{}, err
	}

	ttl := time.Duration(resolved.ExpiresInSeconds) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	} else if ttl > time.Minute {
		ttl -= time.Minute
	}
	b.cache.Set(ctx, api.ID, resolved, ttl)

	return resolved, nil
}
