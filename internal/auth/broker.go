// Package auth resolves CONNECTOR credential auth — the secret needed to
// call an upstream API. This is entirely separate from internal/identity,
// which owns this platform's own login/session.
package auth

import (
	"context"
	"fmt"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
	"gridhook.dev/connector-backend/internal/models"
)

// CredentialsStore loads (and decrypts) the stored credentials row for a
// connector API. Implemented by internal/controlplane against the DB +
// Sealer; kept as an interface here so auth has zero DB dependency.
type CredentialsStore interface {
	LoadCredentials(ctx context.Context, connectorAPIID int64) (*models.ConnectorCredentials, error)
}

// Broker resolves a ConnectorAPI down to the generic Credentials its engine
// should attach to the outbound call, caching per connector_api so a hot
// tool doesn't re-authenticate on every dispatch.
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
			models.AuthLoginToken: schemes.BearerScheme{}, // a login_token is presented the same way as a static bearer
		},
	}
}

// Resolve returns cached Credentials if present and unexpired; otherwise it
// loads the connector's stored credentials, resolves them through the
// matching Scheme, caches the result (oauth2's expires_in becomes the TTL —
// everything else caches for a conservative default), and returns it.
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
		ttl = 5 * time.Minute // static creds: still cache briefly to absorb bursts
	} else if ttl > time.Minute {
		ttl -= time.Minute // refresh slightly before real expiry
	}
	b.cache.Set(ctx, api.ID, resolved, ttl)

	return resolved, nil
}
