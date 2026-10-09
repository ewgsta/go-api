package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggerFromDefault(t *testing.T) {
	t.Parallel()

	got := LoggerFrom(context.Background())
	if got != slog.Default() {
		t.Errorf("LoggerFrom(background) = %p, want the default logger", got)
	}
}

func TestLoggerFromContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	if got := LoggerFrom(ContextWithLogger(context.Background(), logger)); got != logger {
		t.Errorf("LoggerFrom(ctx) = %p, want the stored logger", got)
	}
}

func TestLoggerFromIgnoresNil(t *testing.T) {
	t.Parallel()

	ctx := ContextWithLogger(context.Background(), nil)
	if got := LoggerFrom(ctx); got != slog.Default() {
		t.Errorf("LoggerFrom(ctx with nil logger) = %p, want the default logger", got)
	}
}

func TestWithLoggerStoresLogger(t *testing.T) {
	t.Parallel()

	var (
		buf      bytes.Buffer
		observed *slog.Logger
	)

	logger := newTestLogger(&buf)
	handler := WithLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = LoggerFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if observed != logger {
		t.Error("handler did not see the installed logger")
	}
}

// TestWithLoggerEmitsToConfiguredSink verifies the point of the middleware: log
// records reach the configured buffer, not the process default logger.
func TestWithLoggerEmitsToConfiguredSink(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	handler := WithLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LoggerFrom(r.Context()).InfoContext(r.Context(), "handler_logged")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("got %d records, want 1: %s", len(logs), buf.String())
	}
	if logs[0]["msg"] != "handler_logged" {
		t.Errorf("msg = %v, want handler_logged", logs[0]["msg"])
	}
}
