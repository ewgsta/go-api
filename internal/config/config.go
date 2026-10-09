// Package config loads and validates runtime configuration from environment
// variables. Invalid values are reported as errors instead of being replaced by
// defaults, so a misconfigured deployment fails fast at startup.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Default values for configuration that is safe to omit.
const (
	DefaultAddr              = ":8080"
	DefaultReadTimeout       = 10 * time.Second
	DefaultWriteTimeout      = 15 * time.Second
	DefaultIdleTimeout       = 60 * time.Second
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultShutdownTimeout   = 15 * time.Second
	DefaultLogLevel          = slog.LevelInfo
	DefaultEnvironment       = "development"
)

// Environment names understood by APP_ENV.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config is the validated application configuration.
type Config struct {
	Environment string
	HTTP        HTTPConfig
	Log         LogConfig
}

// HTTPConfig holds the HTTP server settings.
type HTTPConfig struct {
	// Addr is the listen address, for example ":8080".
	Addr string
	// ReadTimeout bounds reading the entire request, including the body.
	ReadTimeout time.Duration
	// WriteTimeout bounds writing the response.
	WriteTimeout time.Duration
	// IdleTimeout bounds how long a keep-alive connection may stay idle.
	IdleTimeout time.Duration
	// ReadHeaderTimeout bounds reading request headers. It keeps slow-header
	// clients from occupying a connection indefinitely.
	ReadHeaderTimeout time.Duration
	// ShutdownTimeout bounds graceful shutdown before in-flight connections
	// are closed.
	ShutdownTimeout time.Duration
}

// LogConfig holds logging settings.
type LogConfig struct {
	// Level is the minimum level that is written.
	Level slog.Level
}

// Load reads configuration from the process environment and validates it.
func Load() (Config, error) {
	return loadFrom(os.LookupEnv)
}

// loadFrom builds a Config from the given environment lookup, which keeps the
// parsing logic testable without mutating the process environment.
func loadFrom(lookup func(string) (string, bool)) (Config, error) {
	var (
		cfg Config
		err error
	)

	cfg.Environment = stringOr(lookup, "APP_ENV", DefaultEnvironment)
	switch cfg.Environment {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		return Config{}, fmt.Errorf("APP_ENV: invalid value %q: must be one of %q, %q, %q",
			cfg.Environment, EnvDevelopment, EnvStaging, EnvProduction)
	}

	if cfg.HTTP.Addr, err = addr(lookup); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.ReadTimeout, err = duration(lookup, "HTTP_READ_TIMEOUT", DefaultReadTimeout); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.WriteTimeout, err = duration(lookup, "HTTP_WRITE_TIMEOUT", DefaultWriteTimeout); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.IdleTimeout, err = duration(lookup, "HTTP_IDLE_TIMEOUT", DefaultIdleTimeout); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.ReadHeaderTimeout, err = duration(lookup, "HTTP_READ_HEADER_TIMEOUT", DefaultReadHeaderTimeout); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.ShutdownTimeout, err = duration(lookup, "HTTP_SHUTDOWN_TIMEOUT", DefaultShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.Log.Level, err = logLevel(lookup); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// addr validates the listen address. An empty value is rejected rather than
// treated as a valid address, because net/http would silently bind to ":http".
func addr(lookup func(string) (string, bool)) (string, error) {
	raw, ok := lookup("HTTP_ADDR")
	if !ok || strings.TrimSpace(raw) == "" {
		if !ok {
			return DefaultAddr, nil
		}
		return "", fmt.Errorf("HTTP_ADDR: must not be empty")
	}

	value := strings.TrimSpace(raw)
	host, port, err := splitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("HTTP_ADDR: invalid value %q: %w", raw, err)
	}
	if port == "" {
		return "", fmt.Errorf("HTTP_ADDR: missing port in %q: expected a value like \":8080\"", raw)
	}
	if strings.ContainsAny(host, " \t") {
		return "", fmt.Errorf("HTTP_ADDR: invalid host %q", host)
	}

	return value, nil
}

// splitHostPort accepts both "host:port" and ":port" without requiring a
// network lookup, unlike net.SplitHostPort alone on some inputs.
func splitHostPort(value string) (host, port string, err error) {
	idx := strings.LastIndex(value, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("missing port")
	}
	host, port = value[:idx], value[idx+1:]
	if strings.HasPrefix(port, ":") {
		return "", "", fmt.Errorf("invalid port %q", port)
	}
	return host, port, nil
}

func duration(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		if !ok {
			return fallback, nil
		}
		return 0, fmt.Errorf("%s: must not be empty", key)
	}

	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: expected a value like \"10s\"", key, raw)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be greater than zero, got %s", key, value)
	}

	return value, nil
}

func logLevel(lookup func(string) (string, bool)) (slog.Level, error) {
	raw, ok := lookup("LOG_LEVEL")
	if !ok || strings.TrimSpace(raw) == "" {
		if !ok {
			return DefaultLogLevel, nil
		}
		return 0, fmt.Errorf("LOG_LEVEL: must not be empty")
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimSpace(raw))); err != nil {
		return 0, fmt.Errorf("LOG_LEVEL: invalid value %q: must be one of \"debug\", \"info\", \"warn\", \"error\"", raw)
	}

	return level, nil
}

func stringOr(lookup func(string) (string, bool), key, fallback string) string {
	if raw, ok := lookup(key); ok {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}
