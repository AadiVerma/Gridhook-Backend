package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL      string
	HTTPAddr         string
	SessionTTL       time.Duration
	KMSKeyID         string
	MCPPublicBaseURL string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("config: load .env: %w", err)
	}

	cfg := Config{
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://localhost:5432/gridhook?sslmode=disable"),
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
