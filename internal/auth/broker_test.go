package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gridhook.dev/connector-backend/internal/models"
)

type fakeStore struct {
	loads   atomic.Int32
	release chan struct{}
	creds   *models.ConnectorCredentials
	err     error
}

func (f *fakeStore) LoadCredentials(_ context.Context, _ int64) (*models.ConnectorCredentials, error) {
	f.loads.Add(1)
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.creds, nil
}

func bearerAPI() *models.ConnectorAPI {
	return &models.ConnectorAPI{ID: 42, AuthType: models.AuthBearer}
}

func bearerCreds() *models.ConnectorCredentials {
	return &models.ConnectorCredentials{ConnectorAPIID: 42, AuthType: models.AuthBearer, BearerToken: "tok"}
}

func TestBroker_Resolve_AuthNoneSkipsStore(t *testing.T) {
	store := &fakeStore{creds: bearerCreds()}
	broker := NewBroker(store, NewInMemoryTokenCache(), nil)

	got, err := broker.Resolve(t.Context(), &models.ConnectorAPI{ID: 1, AuthType: models.AuthNone})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Headers) != 0 {
		t.Errorf("Headers = %v, want empty for auth type none", got.Headers)
	}
	if n := store.loads.Load(); n != 0 {
		t.Errorf("store loads = %d, want 0 — an unauthenticated API must not hit the credential store", n)
	}
}

func TestBroker_Resolve_UnknownAuthType(t *testing.T) {
	broker := NewBroker(&fakeStore{creds: bearerCreds()}, NewInMemoryTokenCache(), nil)

	_, err := broker.Resolve(t.Context(), &models.ConnectorAPI{ID: 1, AuthType: models.AuthType("saml")})
	if err == nil {
		t.Fatal("Resolve accepted an unregistered auth type")
	}
}

func TestBroker_Resolve_AppliesScheme(t *testing.T) {
	broker := NewBroker(&fakeStore{creds: bearerCreds()}, NewInMemoryTokenCache(), nil)

	got, err := broker.Resolve(t.Context(), bearerAPI())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization = %q", got.Headers["Authorization"])
	}
}

func TestBroker_Resolve_CachesResult(t *testing.T) {
	store := &fakeStore{creds: bearerCreds()}
	broker := NewBroker(store, NewInMemoryTokenCache(), nil)

	for range 5 {
		if _, err := broker.Resolve(t.Context(), bearerAPI()); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}
	if n := store.loads.Load(); n != 1 {
		t.Errorf("store loads = %d, want 1 — subsequent resolutions must be served from cache", n)
	}
}

func TestBroker_Resolve_DoesNotCacheFailures(t *testing.T) {
	sentinel := errors.New("store unavailable")
	store := &fakeStore{err: sentinel}
	broker := NewBroker(store, NewInMemoryTokenCache(), nil)

	for range 3 {
		if _, err := broker.Resolve(t.Context(), bearerAPI()); !errors.Is(err, sentinel) {
			t.Fatalf("Resolve = %v, want the store error", err)
		}
	}
	if n := store.loads.Load(); n != 3 {
		t.Errorf("store loads = %d, want 3 — a failed resolution must not be cached", n)
	}
}

func TestBroker_Resolve_CollapsesConcurrentResolutions(t *testing.T) {
	store := &fakeStore{creds: bearerCreds(), release: make(chan struct{})}
	broker := NewBroker(store, NewInMemoryTokenCache(), nil)

	const callers = 32
	var wg sync.WaitGroup
	errs := make(chan error, callers)

	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := broker.Resolve(t.Context(), bearerAPI()); err != nil {
				errs <- err
			}
		}()
	}

	waitFor(t, func() bool { return store.loads.Load() >= 1 })
	close(store.release)

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Resolve: %v", err)
	}

	if n := store.loads.Load(); n != 1 {
		t.Errorf("store loads = %d, want 1 — concurrent resolutions must collapse into one", n)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestCacheTTL(t *testing.T) {
	cases := []struct {
		name             string
		expiresInSeconds int
		want             time.Duration
	}{
		{"unknown lifetime uses the default", 0, defaultCacheTTL},
		{"negative lifetime uses the default", -1, defaultCacheTTL},
		{"long lifetime is shortened by the refresh margin", 3600, 3600*time.Second - refreshMargin},

		{"lifetime equal to the margin is kept as-is", 60, time.Minute},
		{"lifetime below the margin is kept as-is", 30, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cacheTTL(tc.expiresInSeconds); got != tc.want {
				t.Errorf("cacheTTL(%d) = %v, want %v", tc.expiresInSeconds, got, tc.want)
			}
		})
	}
}
