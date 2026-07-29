package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Environment string

const (
	EnvDevelopment Environment = "development"

	EnvProduction Environment = "production"
)

type Config struct {
	Env           Environment
	HTTP          HTTP
	Database      Database
	Session       Session
	Security      Security
	MCP           MCP
	Upstream      Upstream
	Observability Observability
	Worker        Worker
}

type HTTP struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	MaxRequestBytes int64
	AllowedOrigins  []string
}

type Database struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

type Session struct {
	TTL time.Duration
}

type Security struct {
	DataKey string

	KMSKeyID string

	InternalToken string

	PublicIDKey string
}

type MCP struct {
	PublicBaseURL string
}

type Upstream struct {
	Timeout              time.Duration
	MaxResponseBytes     int64
	MaxRetries           int
	MaxIdleConnsPerHost  int
	AllowPrivateNetworks bool
}

type Observability struct {
	LogLevel  string
	LogFormat string

	LogColor string
}

type Worker struct {
	SessionSweepInterval  time.Duration
	SessionSweepRetention time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("config: load .env: %w", err)
	}

	env := Environment(strings.ToLower(stringVar("APP_ENV", string(EnvDevelopment))))

	cfg := Config{
		Env: env,
		HTTP: HTTP{
			Addr:              stringVar("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: durationVar("HTTP_READ_HEADER_TIMEOUT", 10*time.Second),
			ReadTimeout:       durationVar("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:      durationVar("HTTP_WRITE_TIMEOUT", 60*time.Second),
			IdleTimeout:       durationVar("HTTP_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout:   durationVar("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
			MaxRequestBytes:   bytesVar("HTTP_MAX_REQUEST_BYTES", 4<<20),
			AllowedOrigins:    csvVar("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000"),
		},
		Database: Database{
			URL:             stringVar("DATABASE_URL", ""),
			MaxOpenConns:    intVar("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    intVar("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: durationVar("DB_CONN_MAX_LIFETIME", time.Hour),
			ConnMaxIdleTime: durationVar("DB_CONN_MAX_IDLE_TIME", 10*time.Minute),
			ConnectTimeout:  durationVar("DB_CONNECT_TIMEOUT", 10*time.Second),
		},
		Session: Session{
			TTL: durationVar("SESSION_TTL", 30*24*time.Hour),
		},
		Security: Security{
			DataKey:       stringVar("KMS_DATA_KEY", ""),
			KMSKeyID:      stringVar("KMS_KEY_ID", ""),
			InternalToken: stringVar("INTERNAL_TOKEN", ""),
			PublicIDKey:   stringVar("PUBLIC_ID_KEY", ""),
		},
		MCP: MCP{
			PublicBaseURL: stringVar("MCP_PUBLIC_BASE_URL", "https://gw.gridhook.dev/mcp"),
		},
		Upstream: Upstream{
			Timeout:              durationVar("UPSTREAM_TIMEOUT", 30*time.Second),
			MaxResponseBytes:     bytesVar("UPSTREAM_MAX_RESPONSE_BYTES", 8<<20),
			MaxRetries:           intVar("UPSTREAM_MAX_RETRIES", 2),
			MaxIdleConnsPerHost:  intVar("UPSTREAM_MAX_IDLE_CONNS_PER_HOST", 16),
			AllowPrivateNetworks: boolVar("UPSTREAM_ALLOW_PRIVATE_NETWORKS", true),
		},
		Observability: Observability{
			LogLevel:  stringVar("LOG_LEVEL", "info"),
			LogFormat: strings.ToLower(stringVar("LOG_FORMAT", defaultLogFormat(env))),
			LogColor:  strings.ToLower(stringVar("LOG_COLOR", LogColorAuto)),
		},
		Worker: Worker{
			SessionSweepInterval:  durationVar("WORKER_SESSION_SWEEP_INTERVAL", time.Hour),
			SessionSweepRetention: durationVar("WORKER_SESSION_SWEEP_RETENTION", 30*24*time.Hour),
		},
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string

	switch c.Env {
	case EnvDevelopment, EnvProduction:
	default:
		problems = append(problems, fmt.Sprintf("APP_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.Env))
	}

	if c.Database.URL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	if c.Database.MaxOpenConns <= 0 {
		problems = append(problems, "DB_MAX_OPEN_CONNS must be positive")
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		problems = append(problems, "DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}

	if c.Env == EnvProduction && c.Security.DataKey == "" {
		problems = append(problems, "KMS_DATA_KEY is required when APP_ENV=production")
	}

	if c.Env == EnvProduction && c.Security.PublicIDKey == "" {
		problems = append(problems, "PUBLIC_ID_KEY is required when APP_ENV=production")
	}

	if c.HTTP.Addr == "" {
		problems = append(problems, "HTTP_ADDR is required")
	}
	if c.HTTP.MaxRequestBytes <= 0 {
		problems = append(problems, "HTTP_MAX_REQUEST_BYTES must be positive")
	}
	if c.Session.TTL <= 0 {
		problems = append(problems, "SESSION_TTL must be positive")
	}
	if c.MCP.PublicBaseURL == "" {
		problems = append(problems, "MCP_PUBLIC_BASE_URL is required")
	}
	if c.Upstream.MaxResponseBytes <= 0 {
		problems = append(problems, "UPSTREAM_MAX_RESPONSE_BYTES must be positive")
	}
	if c.Upstream.MaxRetries < 0 {
		problems = append(problems, "UPSTREAM_MAX_RETRIES must not be negative")
	}
	if _, err := ParseLogLevel(c.Observability.LogLevel); err != nil {
		problems = append(problems, err.Error())
	}
	if err := ValidateLogFormat(c.Observability.LogFormat); err != nil {
		problems = append(problems, err.Error())
	}
	if _, err := ResolveLogColor(c.Observability.LogColor, false); err != nil {
		problems = append(problems, err.Error())
	}

	if len(problems) > 0 {
		return fmt.Errorf("config: invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func (c Config) IsProduction() bool { return c.Env == EnvProduction }

func defaultLogFormat(env Environment) string {
	if env == EnvProduction {
		return LogFormatJSON
	}
	return LogFormatText
}

func stringVar(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func intVar(key string, fallback int) int {
	v, err := strconv.Atoi(stringVar(key, ""))
	if err != nil {
		return fallback
	}
	return v
}

func bytesVar(key string, fallback int64) int64 {
	v, err := strconv.ParseInt(stringVar(key, ""), 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func boolVar(key string, fallback bool) bool {
	v, err := strconv.ParseBool(stringVar(key, ""))
	if err != nil {
		return fallback
	}
	return v
}

func durationVar(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(stringVar(key, ""))
	if err != nil {
		return fallback
	}
	return v
}

func csvVar(key, fallback string) []string {
	parts := strings.Split(stringVar(key, fallback), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
