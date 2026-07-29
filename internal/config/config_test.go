package config

import (
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		Env:           EnvDevelopment,
		HTTP:          HTTP{Addr: ":8080", MaxRequestBytes: 1 << 20},
		Database:      Database{URL: "postgres://localhost/db", MaxOpenConns: 25, MaxIdleConns: 5},
		Session:       Session{TTL: time.Hour},
		MCP:           MCP{PublicBaseURL: "https://gw.example.com/mcp"},
		Upstream:      Upstream{MaxResponseBytes: 1 << 20, MaxRetries: 2},
		Observability: Observability{LogLevel: "info", LogFormat: "json"},
	}
}

func TestValidate_AcceptsValidConfig(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_RejectsInvalidFields(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*Config)
		wantText string
	}{
		{"missing database url", func(c *Config) { c.Database.URL = "" }, "DATABASE_URL"},
		{"zero max open conns", func(c *Config) { c.Database.MaxOpenConns = 0 }, "DB_MAX_OPEN_CONNS"},
		{"idle exceeds open", func(c *Config) { c.Database.MaxIdleConns = 100 }, "DB_MAX_IDLE_CONNS"},
		{"missing http addr", func(c *Config) { c.HTTP.Addr = "" }, "HTTP_ADDR"},
		{"zero request cap", func(c *Config) { c.HTTP.MaxRequestBytes = 0 }, "HTTP_MAX_REQUEST_BYTES"},
		{"zero session ttl", func(c *Config) { c.Session.TTL = 0 }, "SESSION_TTL"},
		{"missing mcp base url", func(c *Config) { c.MCP.PublicBaseURL = "" }, "MCP_PUBLIC_BASE_URL"},
		{"zero upstream cap", func(c *Config) { c.Upstream.MaxResponseBytes = 0 }, "UPSTREAM_MAX_RESPONSE_BYTES"},
		{"negative retries", func(c *Config) { c.Upstream.MaxRetries = -1 }, "UPSTREAM_MAX_RETRIES"},
		{"bad log level", func(c *Config) { c.Observability.LogLevel = "verbose" }, "LOG_LEVEL"},
		{"unknown environment", func(c *Config) { c.Env = "staging" }, "APP_ENV"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate accepted an invalid config (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("error = %v, want it to name %s", err, tc.wantText)
			}
		})
	}
}

func TestValidate_ProductionRequiresDataKey(t *testing.T) {
	cfg := validConfig()
	cfg.Env = EnvProduction

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted production with no KMS_DATA_KEY")
	}
	if !strings.Contains(err.Error(), "KMS_DATA_KEY") {
		t.Errorf("error = %v, want it to name KMS_DATA_KEY", err)
	}

	cfg.Security.DataKey = "a-real-key"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate rejected production with a data key set: %v", err)
	}
}

func TestValidate_DevelopmentAllowsMissingDataKey(t *testing.T) {
	cfg := validConfig()
	cfg.Env = EnvDevelopment

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate rejected development without a data key: %v", err)
	}
}

func TestValidate_ReportsEveryProblemAtOnce(t *testing.T) {
	cfg := validConfig()
	cfg.Database.URL = ""
	cfg.HTTP.Addr = ""
	cfg.Session.TTL = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a config with three problems")
	}
	for _, want := range []string{"DATABASE_URL", "HTTP_ADDR", "SESSION_TTL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, missing %s", err, want)
		}
	}
}

func TestParseLogLevel(t *testing.T) {
	for _, name := range []string{"debug", "info", "warn", "warning", "error", "INFO", " info ", ""} {
		if _, err := ParseLogLevel(name); err != nil {
			t.Errorf("ParseLogLevel(%q) = %v, want it accepted", name, err)
		}
	}
	for _, name := range []string{"verbose", "trace", "fatal"} {
		if _, err := ParseLogLevel(name); err == nil {
			t.Errorf("ParseLogLevel(%q) was accepted, want an error", name)
		}
	}
}

func TestDefaultLogFormat(t *testing.T) {
	if got := defaultLogFormat(EnvProduction); got != "json" {
		t.Errorf("production log format = %q, want json for log shippers", got)
	}
	if got := defaultLogFormat(EnvDevelopment); got != "text" {
		t.Errorf("development log format = %q, want text for humans", got)
	}
}

func TestEnvVarHelpers(t *testing.T) {
	t.Setenv("GRIDHOOK_TEST_STRING", "value")
	t.Setenv("GRIDHOOK_TEST_INT", "42")
	t.Setenv("GRIDHOOK_TEST_BOOL", "true")
	t.Setenv("GRIDHOOK_TEST_DURATION", "90s")
	t.Setenv("GRIDHOOK_TEST_BAD_INT", "not-a-number")
	t.Setenv("GRIDHOOK_TEST_BLANK", "   ")

	if got := stringVar("GRIDHOOK_TEST_STRING", "fallback"); got != "value" {
		t.Errorf("stringVar = %q", got)
	}

	if got := stringVar("GRIDHOOK_TEST_BLANK", "fallback"); got != "fallback" {
		t.Errorf("stringVar on a blank value = %q, want the fallback", got)
	}
	if got := stringVar("GRIDHOOK_TEST_ABSENT", "fallback"); got != "fallback" {
		t.Errorf("stringVar on an absent key = %q", got)
	}
	if got := intVar("GRIDHOOK_TEST_INT", 7); got != 42 {
		t.Errorf("intVar = %d", got)
	}
	if got := intVar("GRIDHOOK_TEST_BAD_INT", 7); got != 7 {
		t.Errorf("intVar on unparseable input = %d, want the fallback", got)
	}
	if got := boolVar("GRIDHOOK_TEST_BOOL", false); !got {
		t.Error("boolVar = false, want true")
	}
	if got := durationVar("GRIDHOOK_TEST_DURATION", time.Second); got != 90*time.Second {
		t.Errorf("durationVar = %v", got)
	}
}

func TestCSVVar(t *testing.T) {
	t.Setenv("GRIDHOOK_TEST_CSV", " a , b ,, c ")

	got := csvVar("GRIDHOOK_TEST_CSV", "")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("csvVar = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("csvVar[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
