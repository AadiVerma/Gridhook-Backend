package auth

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/httpx"
	"gridhook.dev/connector-backend/internal/models"
)

type CredentialsStore interface {
	LoadCredentials(ctx context.Context, connectorAPIID int64) (*models.ConnectorCredentials, error)
}

type Broker struct {
	store   CredentialsStore
	cache   TokenCache
	schemes map[models.AuthType]schemes.Scheme

	inflight singleflight.Group
}

func NewBroker(store CredentialsStore, cache TokenCache, httpClient *httpx.Client) *Broker {
	return &Broker{
		store: store,
		cache: cache,
		schemes: map[models.AuthType]schemes.Scheme{
			models.AuthBearer:     schemes.BearerScheme{},
			models.AuthAPIKey:     schemes.APIKeyScheme{},
			models.AuthBasic:      schemes.BasicScheme{},
			models.AuthOAuth2:     schemes.NewOAuth2Scheme(httpClient),
			models.AuthLoginToken: schemes.BearerScheme{},
		},
	}
}

const defaultCacheTTL = 5 * time.Minute

const refreshMargin = time.Minute

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

	key := strconv.FormatInt(api.ID, 10)
	resolved, err, _ := b.inflight.Do(key, func() (any, error) {

		if cached, ok := b.cache.Get(ctx, api.ID); ok {
			return cached, nil
		}

		credsRow, err := b.store.LoadCredentials(ctx, api.ID)
		if err != nil {
			return schemes.Credentials{}, fmt.Errorf("auth: load credentials for connector_api %d: %w", api.ID, err)
		}

		creds, err := scheme.Resolve(ctx, credsRow)
		if err != nil {
			return schemes.Credentials{}, err
		}

		b.cache.Set(ctx, api.ID, creds, cacheTTL(creds.ExpiresInSeconds))
		return creds, nil
	})
	if err != nil {
		return schemes.Credentials{}, err
	}

	creds, ok := resolved.(schemes.Credentials)
	if !ok {
		return schemes.Credentials{}, fmt.Errorf("auth: unexpected resolution type %T", resolved)
	}
	return creds, nil
}

func cacheTTL(expiresInSeconds int) time.Duration {
	if expiresInSeconds <= 0 {
		return defaultCacheTTL
	}
	ttl := time.Duration(expiresInSeconds) * time.Second
	if ttl > refreshMargin {
		return ttl - refreshMargin
	}

	return ttl
}
