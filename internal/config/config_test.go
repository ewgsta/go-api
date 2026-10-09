package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// env builds a lookup function from a map, matching os.LookupEnv semantics.
func env(pairs map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := pairs[key]
		return value, ok
	}
}

func TestLoadFromDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := loadFrom(env(nil))
	if err != nil {
		t.Fatalf("loadFrom() error = %v, want nil", err)
	}

	if cfg.Environment != DefaultEnvironment {
		t.Errorf("Environment = %q, want %q", cfg.Environment, DefaultEnvironment)
	}
	if cfg.HTTP.Addr != DefaultAddr {
		t.Errorf("Addr = %q, want %q", cfg.HTTP.Addr, DefaultAddr)
	}
	if cfg.HTTP.ReadTimeout != DefaultReadTimeout {
		t.Errorf("ReadTimeout = %s, want %s", cfg.HTTP.ReadTimeout, DefaultReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != DefaultWriteTimeout {
		t.Errorf("WriteTimeout = %s, want %s", cfg.HTTP.WriteTimeout, DefaultWriteTimeout)
	}
	if cfg.HTTP.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("IdleTimeout = %s, want %s", cfg.HTTP.IdleTimeout, DefaultIdleTimeout)
	}
	if cfg.HTTP.ReadHeaderTimeout != DefaultReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %s, want %s", cfg.HTTP.ReadHeaderTimeout, DefaultReadHeaderTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %s, want %s", cfg.HTTP.ShutdownTimeout, DefaultShutdownTimeout)
	}
	if cfg.Log.Level != slog.LevelInfo {
		t.Errorf("Log.Level = %s, want %s", cfg.Log.Level, slog.LevelInfo)
	}
}

func TestLoadFromValidValues(t *testing.T) {
	t.Parallel()

	cfg, err := loadFrom(env(map[string]string{
		"APP_ENV":                  "production",
		"HTTP_ADDR":                "127.0.0.1:9000",
		"HTTP_READ_TIMEOUT":        "3s",
		"HTTP_WRITE_TIMEOUT":       "2500ms",
		"HTTP_IDLE_TIMEOUT":        "1m",
		"HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_SHUTDOWN_TIMEOUT":    "30s",
		"LOG_LEVEL":                "warn",
	}))
	if err != nil {
		t.Fatalf("loadFrom() error = %v, want nil", err)
	}

	if cfg.Environment != EnvProduction {
		t.Errorf("Environment = %q, want %q", cfg.Environment, EnvProduction)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9000" {
		t.Errorf("Addr = %q, want %q", cfg.HTTP.Addr, "127.0.0.1:9000")
	}
	if cfg.HTTP.ReadTimeout != 3*time.Second {
		t.Errorf("ReadTimeout = %s, want 3s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 2500*time.Millisecond {
		t.Errorf("WriteTimeout = %s, want 2.5s", cfg.HTTP.WriteTimeout)
	}
	if cfg.HTTP.IdleTimeout != time.Minute {
		t.Errorf("IdleTimeout = %s, want 1m", cfg.HTTP.IdleTimeout)
	}
	if cfg.HTTP.ReadHeaderTimeout != 2*time.Second {
		t.Errorf("ReadHeaderTimeout = %s, want 2s", cfg.HTTP.ReadHeaderTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Log.Level != slog.LevelWarn {
		t.Errorf("Log.Level = %s, want %s", cfg.Log.Level, slog.LevelWarn)
	}
}

func TestLoadFromInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"unknown app env", map[string]string{"APP_ENV": "prod"}, "APP_ENV"},
		{"empty addr", map[string]string{"HTTP_ADDR": "  "}, "HTTP_ADDR: must not be empty"},
		{"addr without port", map[string]string{"HTTP_ADDR": "localhost"}, "HTTP_ADDR"},
		{"addr with invalid host", map[string]string{"HTTP_ADDR": "local host:8080"}, "HTTP_ADDR"},
		{"addr with non-numeric port", map[string]string{"HTTP_ADDR": ":http"}, "HTTP_ADDR"},
		{"addr with out-of-range port", map[string]string{"HTTP_ADDR": ":70000"}, "HTTP_ADDR"},
		{"addr with empty port", map[string]string{"HTTP_ADDR": "localhost:"}, "HTTP_ADDR"},
		{"empty read timeout", map[string]string{"HTTP_READ_TIMEOUT": ""}, "HTTP_READ_TIMEOUT: must not be empty"},
		{"unparsable read timeout", map[string]string{"HTTP_READ_TIMEOUT": "ten"}, "HTTP_READ_TIMEOUT"},
		{"unitless read timeout", map[string]string{"HTTP_READ_TIMEOUT": "10"}, "HTTP_READ_TIMEOUT"},
		{"zero write timeout", map[string]string{"HTTP_WRITE_TIMEOUT": "0s"}, "HTTP_WRITE_TIMEOUT"},
		{"negative idle timeout", map[string]string{"HTTP_IDLE_TIMEOUT": "-1s"}, "HTTP_IDLE_TIMEOUT"},
		{"bad read header timeout", map[string]string{"HTTP_READ_HEADER_TIMEOUT": "nope"}, "HTTP_READ_HEADER_TIMEOUT"},
		{"zero shutdown timeout", map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "0"}, "HTTP_SHUTDOWN_TIMEOUT"},
		{"unknown log level", map[string]string{"LOG_LEVEL": "verbose"}, "LOG_LEVEL"},
		{"empty log level", map[string]string{"LOG_LEVEL": ""}, "LOG_LEVEL: must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadFrom(env(tt.env))
			if err == nil {
				t.Fatalf("loadFrom() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("loadFrom() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadFromAcceptsValidAddresses(t *testing.T) {
	t.Parallel()

	tests := []string{
		":8080",
		"0.0.0.0:80",
		"127.0.0.1:9000",
		"localhost:3000",
		"api.internal.example:65535",
		"[::1]:8080",
		"[::]:8080",
	}

	for _, addr := range tests {
		t.Run(addr, func(t *testing.T) {
			t.Parallel()

			cfg, err := loadFrom(env(map[string]string{"HTTP_ADDR": addr}))
			if err != nil {
				t.Fatalf("loadFrom() error = %v, want nil for %q", err, addr)
			}
			if cfg.HTTP.Addr != addr {
				t.Errorf("Addr = %q, want %q", cfg.HTTP.Addr, addr)
			}
		})
	}
}

func TestLoadFromTrimsWhitespace(t *testing.T) {
	t.Parallel()

	cfg, err := loadFrom(env(map[string]string{
		"APP_ENV":           " staging ",
		"HTTP_ADDR":         " 127.0.0.1:8080 ",
		"HTTP_READ_TIMEOUT": " 5s ",
		"LOG_LEVEL":         " debug ",
	}))
	if err != nil {
		t.Fatalf("loadFrom() error = %v, want nil", err)
	}
	if cfg.Environment != EnvStaging {
		t.Errorf("Environment = %q, want %q", cfg.Environment, EnvStaging)
	}
	if cfg.HTTP.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want %q", cfg.HTTP.Addr, "127.0.0.1:8080")
	}
	if cfg.HTTP.ReadTimeout != 5*time.Second {
		t.Errorf("ReadTimeout = %s, want 5s", cfg.HTTP.ReadTimeout)
	}
	if cfg.Log.Level != slog.LevelDebug {
		t.Errorf("Log.Level = %s, want %s", cfg.Log.Level, slog.LevelDebug)
	}
}

// TestLoadUsesProcessEnvironment documents that Load reads from the process
// environment without requiring a .env file.
func TestLoadUsesProcessEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:8123")
	t.Setenv("LOG_LEVEL", "error")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:8123" {
		t.Errorf("Addr = %q, want %q", cfg.HTTP.Addr, "127.0.0.1:8123")
	}
	if cfg.Log.Level != slog.LevelError {
		t.Errorf("Log.Level = %s, want %s", cfg.Log.Level, slog.LevelError)
	}
}
