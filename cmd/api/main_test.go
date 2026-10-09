package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/ewgsta/go-api-starter/internal/config"
)

func TestNewLoggerWritesJSONToStderr(t *testing.T) {
	out := captureStderr(t)

	logger := newLogger(config.Config{Log: config.LogConfig{Level: slog.LevelDebug}})
	logger.Debug("debug_line")
	logger.Info("info_line")

	logged := out()
	if logged == "" {
		t.Fatal("no log output captured")
	}

	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not valid JSON: %v", line, err)
		}
	}
	if !strings.Contains(logged, "debug_line") || !strings.Contains(logged, "info_line") {
		t.Errorf("output %q, want both records", logged)
	}
}

// TestNewLoggerHonorsLevel verifies that the configured level filters records.
func TestNewLoggerHonorsLevel(t *testing.T) {
	out := captureStderr(t)

	logger := newLogger(config.Config{Log: config.LogConfig{Level: slog.LevelError}})
	logger.Info("suppressed_info")
	logger.Error("emitted_error")

	logged := out()
	if strings.Contains(logged, "suppressed_info") {
		t.Errorf("info record emitted at error level: %s", logged)
	}
	if !strings.Contains(logged, "emitted_error") {
		t.Errorf("error record missing at error level: %s", logged)
	}
}

// TestRunRejectsInvalidConfig verifies that a bad configuration stops startup
// instead of silently falling back to defaults.
func TestRunRejectsInvalidConfig(t *testing.T) {
	t.Setenv("HTTP_ADDR", "not-an-address")

	err := run()
	if err == nil {
		t.Fatal("run() error = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("run() error = %q, want it to mention invalid configuration", err)
	}
}

func TestRunRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "chatty")

	err := run()
	if err == nil {
		t.Fatal("run() error = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Errorf("run() error = %q, want it to mention LOG_LEVEL", err)
	}
}

// captureStderr redirects os.Stderr to a pipe. The returned function closes the
// pipe and returns everything written to it, so the caller controls the flush.
func captureStderr(t *testing.T) func() string {
	t.Helper()

	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stderr = writer

	t.Cleanup(func() {
		os.Stderr = original
		_ = reader.Close()
	})

	return func() string {
		if err := writer.Close(); err != nil {
			t.Fatalf("close pipe writer: %v", err)
		}
		captured, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read captured output: %v", err)
		}
		return string(captured)
	}
}
