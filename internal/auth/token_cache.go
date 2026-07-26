package auth

import (
	"context"
	"sync"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
)

// TokenCache caches resolved Credentials per connector_api so a hot tool
// doesn't re-run an oauth2 client-credentials exchange on every call. Keyed
// as "connector_auth_cache:{connector_api_id}", matching trd.md's Valkey key
// shape — the interface is intentionally Redis-shaped (Get/Set with a TTL)
// so swapping InMemoryTokenCache for a go-redis-backed implementation is a
// one-file change with no callers touched.
type TokenCache interface {
	Get(ctx context.Context, connectorAPIID int64) (schemes.Credentials, bool)
	Set(ctx context.Context, connectorAPIID int64, creds schemes.Credentials, ttl time.Duration)
}

type cacheEntry struct {
	creds     schemes.Credentials
	expiresAt time.Time
}

// InMemoryTokenCache is the local-process default. Fine for a single
// replica; a multi-replica deployment should back this with Valkey/Redis
// instead so all replicas share one oauth2 token per connector.
type InMemoryTokenCache struct {
	mu      sync.Mutex
	entries map[int64]cacheEntry
}

func NewInMemoryTokenCache() *InMemoryTokenCache {
	return &InMemoryTokenCache{entries: make(map[int64]cacheEntry)}
}

func (c *InMemoryTokenCache) Get(_ context.Context, connectorAPIID int64) (schemes.Credentials, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[connectorAPIID]
	if !ok || time.Now().After(entry.expiresAt) {
		return schemes.Credentials{}, false
	}
	return entry.creds, true
}

func (c *InMemoryTokenCache) Set(_ context.Context, connectorAPIID int64, creds schemes.Credentials, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[connectorAPIID] = cacheEntry{creds: creds, expiresAt: time.Now().Add(ttl)}
}
