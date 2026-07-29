package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const (
	LogFormatJSON = "json"

	LogFormatText = "text"
)

const (
	LogColorAuto = "auto"

	LogColorAlways = "always"

	LogColorNever = "never"
)

func ResolveLogColor(mode string, stdoutIsTerminal bool) (bool, error) {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case LogColorAlways:
		return true, nil
	case LogColorNever:
		return false, nil
	case LogColorAuto, "":
		return stdoutIsTerminal, nil
	default:
		return false, fmt.Errorf("LOG_COLOR must be one of %s, %s, %s; got %q",
			LogColorAuto, LogColorAlways, LogColorNever, mode)
	}
}

func ValidateLogFormat(format string) error {
	switch format {
	case LogFormatJSON, LogFormatText:
		return nil
	default:
		return fmt.Errorf("LOG_FORMAT must be %s or %s; got %q", LogFormatJSON, LogFormatText, format)
	}
}

func ParseLogLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, error; got %q", name)
	}
}
