package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestLogger returns a logger writing JSON records to buf.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// records decodes the JSON log lines written to buf.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not valid JSON: %v", line, err)
		}
		out = append(out, record)
	}
	return out
}

func TestRequestLoggerRecordsRequest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	handler := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exercise the implicit 200 from Write.
		_, _ = w.Write([]byte("hello"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/things?token=secret", nil)
	req = req.WithContext(ContextWithRequestID(req.Context(), "req-1"))

	handler.ServeHTTP(rec, req)

	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("got %d log records, want exactly 1: %s", len(logs), buf.String())
	}

	record := logs[0]
	if record["msg"] != "http_request" {
		t.Errorf("msg = %v, want http_request", record["msg"])
	}
	if record["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", record["level"])
	}
	if record["request_id"] != "req-1" {
		t.Errorf("request_id = %v, want req-1", record["request_id"])
	}
	if record["method"] != http.MethodPost {
		t.Errorf("method = %v, want POST", record["method"])
	}
	if record["path"] != "/v1/things" {
		t.Errorf("path = %v, want /v1/things", record["path"])
	}
	if record["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", record["status"])
	}
	if record["bytes"] != float64(5) {
		t.Errorf("bytes = %v, want 5", record["bytes"])
	}
	if _, ok := record["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v, want a number", record["duration_ms"])
	}
}

func TestRequestLoggerSeverity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		wantLevel string
	}{
		{"success", http.StatusOK, "INFO"},
		{"redirect", http.StatusMovedPermanently, "INFO"},
		{"client error", http.StatusNotFound, "WARN"},
		{"unauthorized", http.StatusUnauthorized, "WARN"},
		{"server error", http.StatusInternalServerError, "ERROR"},
		{"unavailable", http.StatusServiceUnavailable, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			handler := RequestLogger(newTestLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

			logs := records(t, &buf)
			if len(logs) != 1 {
				t.Fatalf("got %d log records, want 1", len(logs))
			}
			if logs[0]["level"] != tt.wantLevel {
				t.Errorf("level = %v, want %v", logs[0]["level"], tt.wantLevel)
			}
		})
	}
}

func TestRequestLoggerExcludesQueryAndHeaders(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := RequestLogger(newTestLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/search?q=secret-value", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("Cookie", "session=secret-session")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	for _, forbidden := range []string{"secret-value", "super-secret-token", "secret-session", "Authorization", "Cookie"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("log output contains %q: %s", forbidden, out)
		}
	}
	if !strings.Contains(out, `"path":"/search"`) {
		t.Errorf("log output missing path field: %s", out)
	}
}

// TestRequestLoggerLogsOnceWhenComposed verifies that composing the logger
// twice still produces a single request log.
func TestRequestLoggerLogsOnceWhenComposed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := RequestLogger(newTestLogger(&buf))

	handler := log(log(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("got %d log records for a doubled middleware, want 1: %s", len(logs), buf.String())
	}
	if logs[0]["msg"] != "http_request" {
		t.Errorf("msg = %v, want http_request", logs[0]["msg"])
	}
}

func TestLogResponseWriter(t *testing.T) {
	t.Parallel()

	t.Run("records explicit status", func(t *testing.T) {
		t.Parallel()

		rec := httptest.NewRecorder()
		w := NewLogResponseWriter(rec)
		w.WriteHeader(http.StatusTeapot)

		if w.Status() != http.StatusTeapot {
			t.Errorf("Status() = %d, want %d", w.Status(), http.StatusTeapot)
		}
		if rec.Code != http.StatusTeapot {
			t.Errorf("response code = %d, want %d", rec.Code, http.StatusTeapot)
		}
	})

	t.Run("defaults to 200", func(t *testing.T) {
		t.Parallel()

		w := NewLogResponseWriter(httptest.NewRecorder())
		if _, err := w.Write([]byte("abc")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		if w.Status() != http.StatusOK {
			t.Errorf("Status() = %d, want 200", w.Status())
		}
		if w.Bytes() != 3 {
			t.Errorf("Bytes() = %d, want 3", w.Bytes())
		}
	})

	t.Run("keeps first status", func(t *testing.T) {
		t.Parallel()

		w := NewLogResponseWriter(httptest.NewRecorder())
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusTeapot)

		if w.Status() != http.StatusCreated {
			t.Errorf("Status() = %d, want %d", w.Status(), http.StatusCreated)
		}
	})
}
