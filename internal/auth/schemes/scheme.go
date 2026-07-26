// Package schemes implements one Scheme per connector auth type. A Scheme's
// only job is turning a stored ConnectorCredentials row into a generic
// Credentials bundle (headers/query params to attach to the outbound call) —
// it knows nothing about REST vs SOAP vs GraphQL, that's the engines'
// concern entirely.
package schemes

import (
	"context"

	"gridhook.dev/connector-backend/internal/models"
)

// Credentials is the generic, engine-agnostic result of resolving a
// connector's auth. Every engine applies these the same way regardless of
// which Scheme produced them.
type Credentials struct {
	Headers     map[string]string
	QueryParams map[string]string
	// ExpiresInSeconds is 0 for credentials that never expire (api key,
	// basic, static bearer). oauth2 sets it so the broker's token cache
	// knows when to re-resolve.
	ExpiresInSeconds int
}

type Scheme interface {
	Resolve(ctx context.Context, creds *models.ConnectorCredentials) (Credentials, error)
}
