package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ewgsta/go-api-starter/internal/http/middleware"
)

// testLogger returns a logger writing to buf.
func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// do runs a request through the full router and returns the recorder.
func do(t *testing.T, h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouterHelloEndpoint(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	rec := do(t, router, http.MethodGet, "/v1/hello", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	if id := rec.Header().Get("X-Request-ID"); !ValidRequestIDForTest(id) {
		t.Errorf("X-Request-ID = %q, want a valid generated id", id)
	}

	var payload struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if payload.Data.Message != "API is running" {
		t.Errorf("message = %q, want %q", payload.Data.Message, "API is running")
	}
	if payload.RequestID != rec.Header().Get("X-Request-ID") {
		t.Errorf("body request_id = %q, header = %q, want equal", payload.RequestID, rec.Header().Get("X-Request-ID"))
	}
}

// ValidRequestIDForTest mirrors the middleware's validation rule for assertions.
func ValidRequestIDForTest(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func TestRouterHealthEndpoints(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			rec := do(t, router, http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}

			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("body is not valid JSON: %v", err)
			}
			if _, ok := payload["data"]; !ok {
				t.Errorf("body %s has no data field", rec.Body.String())
			}
			if id, ok := payload["request_id"].(string); !ok || id == "" {
				t.Errorf("request_id = %v, want a non-empty string", payload["request_id"])
			}
		})
	}
}

func TestRouterNotFound(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	rec := do(t, router, http.MethodGet, "/does-not-exist", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if payload.Error.Code != CodeNotFound {
		t.Errorf("code = %q, want %q", payload.Error.Code, CodeNotFound)
	}
	if payload.RequestID == "" {
		t.Error("request_id is empty in the error response")
	}
}

func TestRouterMethodNotAllowed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	rec := do(t, router, http.MethodPost, "/v1/hello", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if payload.Error.Code != CodeMethodNotAllowed {
		t.Errorf("code = %q, want %q", payload.Error.Code, CodeMethodNotAllowed)
	}
}

func TestRouterPropagatesIncomingRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	const incoming = "550e8400-e29b-41d4-a716-446655440000"
	rec := do(t, router, http.MethodGet, "/v1/hello", map[string]string{"X-Request-ID": incoming})

	if got := rec.Header().Get("X-Request-ID"); got != incoming {
		t.Errorf("X-Request-ID = %q, want %q", got, incoming)
	}

	// The ID also appears in the request log line.
	if !strings.Contains(buf.String(), incoming) {
		t.Errorf("logs do not contain the request id %q: %s", incoming, buf.String())
	}
}

func TestRouterReplacesMalformedRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	rec := do(t, router, http.MethodGet, "/v1/hello", map[string]string{"X-Request-ID": "bad id\ninjected"})

	got := rec.Header().Get("X-Request-ID")
	if got == "bad id\ninjected" {
		t.Error("malformed request id was accepted")
	}
	if !ValidRequestIDForTest(got) {
		t.Errorf("X-Request-ID = %q, want a generated valid id", got)
	}
}

func TestRouterLogsEveryRequest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	do(t, router, http.MethodGet, "/v1/hello?token=secret", nil)

	var requestLogs int
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"msg":"http_request"`) {
			requestLogs++
		}
	}
	if requestLogs != 1 {
		t.Errorf("request logs = %d, want exactly 1: %s", requestLogs, buf.String())
	}
	if strings.Contains(buf.String(), "secret") {
		t.Errorf("logs contain the query string: %s", buf.String())
	}
}

// TestRouterRecoversPanic verifies that a panic in a handler becomes a 500 with
// the standard envelope, a single request log with status 500, and no leaked
// details. The router is rebuilt here with a panicking route because
// NewRouter intentionally exposes no way to add one.
func TestRouterRecoversPanic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := testLogger(&buf)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.Recover(logger, writeInternalError))
	r.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("secret internal state")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret internal state") {
		t.Errorf("body leaks panic details: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"INTERNAL_ERROR"`) {
		t.Errorf("body = %s, want the standard error envelope", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "http_panic") {
		t.Errorf("logs missing panic record: %s", buf.String())
	}

	var requestLogs int
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if !strings.Contains(line, `"msg":"http_request"`) {
			continue
		}
		requestLogs++
		if !strings.Contains(line, `"status":500`) {
			t.Errorf("request log does not report status 500: %s", line)
		}
	}
	if requestLogs != 1 {
		t.Errorf("request logs = %d, want exactly 1", requestLogs)
	}
}

// TestRouterHandlesLargeBodyAndEmptyRequest checks ordinary edge cases.
func TestRouterHandlesEmptyAndOddRequests(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{"root path", http.MethodGet, "/", http.StatusNotFound},
		{"deep path", http.MethodGet, "/v1/things/1/2/3", http.StatusNotFound},
		{"trailing slash on version root", http.MethodGet, "/v1/", http.StatusNotFound},
		{"healthz with trailing slash", http.MethodGet, "/healthz/", http.StatusNotFound},
		// chi registers exactly the methods given to Get, so HEAD is a 405
		// rather than an implicit GET.
		{"head on hello", http.MethodHead, "/v1/hello", http.StatusMethodNotAllowed},
		{"delete on hello", http.MethodDelete, "/v1/hello", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, router, tt.method, tt.target, nil)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestWriteJSONWritesExactlyOnce(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req-once")

	JSON(rec, req, http.StatusOK, map[string]string{"k": "v"})

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body %q is not valid JSON (double write?): %v", body, err)
	}
}
