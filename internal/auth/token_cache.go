package auth

import (
	"context"
	"sync"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
)

type TokenCache interface {
	Get(ctx context.Context, connectorAPIID int64) (schemes.Credentials, bool)
	Set(ctx context.Context, connectorAPIID int64, creds schemes.Credentials, ttl time.Duration)
}

type cacheEntry struct {
	creds     schemes.Credentials
	expiresAt time.Time
}

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
