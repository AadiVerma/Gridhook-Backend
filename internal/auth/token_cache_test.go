package auth

import (
	"sync"
	"testing"
	"time"

	"gridhook.dev/connector-backend/internal/auth/schemes"
)

func newFrozenCache() (*InMemoryTokenCache, *time.Time) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache := NewInMemoryTokenCache()
	cache.now = func() time.Time { return now }
	return cache, &now
}

func creds(token string) schemes.Credentials {
	return schemes.Credentials{Headers: map[string]string{"Authorization": "Bearer " + token}}
}

func TestInMemoryTokenCache_GetSet(t *testing.T) {
	cache, _ := newFrozenCache()

	if _, ok := cache.Get(t.Context(), 1); ok {
		t.Error("Get on an empty cache reported a hit")
	}

	cache.Set(t.Context(), 1, creds("abc"), time.Minute)
	got, ok := cache.Get(t.Context(), 1)
	if !ok {
		t.Fatal("Get after Set reported a miss")
	}
	if got.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("cached credentials = %v", got.Headers)
	}
}

func TestInMemoryTokenCache_ExpiresEntries(t *testing.T) {
	cache, now := newFrozenCache()
	cache.Set(t.Context(), 1, creds("abc"), time.Minute)

	*now = now.Add(59 * time.Second)
	if _, ok := cache.Get(t.Context(), 1); !ok {
		t.Error("entry expired before its TTL elapsed")
	}

	*now = now.Add(2 * time.Second)
	if _, ok := cache.Get(t.Context(), 1); ok {
		t.Error("entry survived past its TTL")
	}
}

func TestInMemoryTokenCache_GetEvictsExpired(t *testing.T) {
	cache, now := newFrozenCache()
	cache.Set(t.Context(), 1, creds("abc"), time.Minute)

	*now = now.Add(2 * time.Minute)
	_, _ = cache.Get(t.Context(), 1)

	if got := cache.Len(); got != 0 {
		t.Errorf("Len after reading an expired entry = %d, want 0", got)
	}
}

func TestInMemoryTokenCache_IgnoresNonPositiveTTL(t *testing.T) {
	cache, _ := newFrozenCache()

	cache.Set(t.Context(), 1, creds("abc"), 0)
	cache.Set(t.Context(), 2, creds("def"), -time.Second)

	if got := cache.Len(); got != 0 {
		t.Errorf("Len = %d, want 0 — a non-positive TTL must not cache", got)
	}
}

func TestInMemoryTokenCache_EvictsExpiredOnGrowth(t *testing.T) {
	cache, now := newFrozenCache()

	for i := range int64(evictionThreshold) {
		cache.Set(t.Context(), i, creds("stale"), time.Minute)
	}
	if got := cache.Len(); got != evictionThreshold {
		t.Fatalf("Len = %d, want %d", got, evictionThreshold)
	}

	*now = now.Add(2 * time.Minute)
	cache.Set(t.Context(), 99999, creds("fresh"), time.Minute)

	if got := cache.Len(); got != 1 {
		t.Errorf("Len after eviction sweep = %d, want 1 (only the fresh entry)", got)
	}
	if _, ok := cache.Get(t.Context(), 99999); !ok {
		t.Error("the freshly written entry was evicted")
	}
}

func TestInMemoryTokenCache_ConcurrentAccess(t *testing.T) {
	cache := NewInMemoryTokenCache()

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 100 {
				id := int64((i*100 + j) % 64)
				cache.Set(t.Context(), id, creds("token"), time.Minute)
				cache.Get(t.Context(), id)
				cache.Len()
			}
		}()
	}
	wg.Wait()
}
