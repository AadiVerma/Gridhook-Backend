package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	SessionTTL         time.Duration
	KMSKeyID           string
	MCPPublicBaseURL   string
	CORSAllowedOrigins []string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("config: load .env: %w", err)
	}

	cfg := Config{
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		HTTPAddr:           getEnv("HTTP_ADDR", ":8080"),
		SessionTTL:         30 * 24 * time.Hour,
		KMSKeyID:           os.Getenv("KMS_KEY_ID"),
		MCPPublicBaseURL:   getEnv("MCP_PUBLIC_BASE_URL", "https://gw.gridhook.dev/mcp"),
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000")),
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

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
