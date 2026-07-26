// Package config loads process configuration from the environment. Kept
// deliberately tiny — no viper/koanf — since the surface area here is small
// and explicit beats magic for a handful of env vars.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	DatabaseURL      string
	RedisAddr        string
	HTTPAddr         string
	SessionTTL       time.Duration
	KMSKeyID         string // passed to internal/db.Sealer implementations
	MCPPublicBaseURL string // e.g. https://gw.gridhook.dev/mcp
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://localhost:5432/gridhook?sslmode=disable"),
		RedisAddr:        getEnv("REDIS_ADDR", "localhost:6379"),
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		SessionTTL:       30 * 24 * time.Hour,
		KMSKeyID:         os.Getenv("KMS_KEY_ID"),
		MCPPublicBaseURL: getEnv("MCP_PUBLIC_BASE_URL", "https://gw.gridhook.dev/mcp"),
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
