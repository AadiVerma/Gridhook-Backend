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

const evictionThreshold = 512

type cacheEntry struct {
	creds     schemes.Credentials
	expiresAt time.Time
}

type InMemoryTokenCache struct {
	mu      sync.Mutex
	entries map[int64]cacheEntry
	now     func() time.Time
}

var _ TokenCache = (*InMemoryTokenCache)(nil)

func NewInMemoryTokenCache() *InMemoryTokenCache {
	return &InMemoryTokenCache{
		entries: make(map[int64]cacheEntry),
		now:     time.Now,
	}
}

func (c *InMemoryTokenCache) Get(_ context.Context, connectorAPIID int64) (schemes.Credentials, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[connectorAPIID]
	if !ok {
		return schemes.Credentials{}, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, connectorAPIID)
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

	if len(c.entries) >= evictionThreshold {
		c.evictExpiredLocked()
	}
	c.entries[connectorAPIID] = cacheEntry{creds: creds, expiresAt: c.now().Add(ttl)}
}

func (c *InMemoryTokenCache) evictExpiredLocked() {
	now := c.now()
	for id, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, id)
		}
	}
}

func (c *InMemoryTokenCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
