package engines

import (
	"testing"

	"gridhook.dev/connector-backend/internal/httpx"
)

func newTestClient(t *testing.T) *httpx.Client {
	t.Helper()

	cfg := httpx.DefaultConfig()
	cfg.MaxRetries = 0
	cfg.AllowPrivateNetworks = true

	client, err := httpx.New(cfg)
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	return client
}
