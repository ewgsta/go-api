package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonResponder mirrors what the response layer writes for a recovered panic.
func jsonResponder(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := `{"error":{"code":"` + code + `","message":"` + message + `"},"request_id":"` + RequestIDFrom(r.Context()) + `"}`
	if _, err := w.Write([]byte(body)); err != nil {
		return
	}
}

func TestRecoverPanicBeforeResponse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Recover(newTestLogger(&buf), jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom: database password leaked")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	req = req.WithContext(ContextWithRequestID(req.Context(), "req-9"))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", got)
	}

	body := rec.Body.String()
	if strings.Contains(body, "boom") || strings.Contains(body, "database password leaked") {
		t.Errorf("response body leaks panic details: %s", body)
	}
	if !strings.Contains(body, `"code":"INTERNAL_ERROR"`) {
		t.Errorf("body = %s, want INTERNAL_ERROR code", body)
	}
	if !strings.Contains(body, `"request_id":"req-9"`) {
		t.Errorf("body = %s, want request id propagation", body)
	}
}

func TestRecoverLogsDiagnostics(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Recover(newTestLogger(&buf), jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("storage failure"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	req = req.WithContext(ContextWithRequestID(req.Context(), "req-7"))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("got %d log records, want 1: %s", len(logs), buf.String())
	}

	record := logs[0]
	if record["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", record["level"])
	}
	if record["msg"] != "http_panic" {
		t.Errorf("msg = %v, want http_panic", record["msg"])
	}
	if record["request_id"] != "req-7" {
		t.Errorf("request_id = %v, want req-7", record["request_id"])
	}
	if record["panic"] != "storage failure" {
		t.Errorf("panic = %v, want %q", record["panic"], "storage failure")
	}
	if stack, ok := record["stack"].(string); !ok || !strings.Contains(stack, "goroutine") {
		t.Errorf("stack = %v, want a goroutine stack trace", record["stack"])
	}
	if record["path"] != "/v1/hello" {
		t.Errorf("path = %v, want /v1/hello", record["path"])
	}
}

func TestRecoverAfterCommittedResponse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var writes int

	handler := Recover(newTestLogger(&buf), jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		writes++
		panic("too late")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	handler.ServeHTTP(rec, req)

	// The first response stands; recovery must not attempt a second one.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the already committed 200", rec.Code)
	}
	if body := rec.Body.String(); body != "partial" {
		t.Errorf("body = %q, want %q", body, "partial")
	}
	if strings.Contains(rec.Body.String(), "INTERNAL_ERROR") {
		t.Errorf("body = %q, want no injected error envelope", rec.Body.String())
	}
	if writes != 1 {
		t.Errorf("handler writes = %d, want 1", writes)
	}

	// The panic is still reported server-side.
	if !strings.Contains(buf.String(), "http_panic") {
		t.Errorf("logs = %s, want an http_panic record", buf.String())
	}
}

func TestRecoverAfterHeadersOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var responses int

	respond := func(w http.ResponseWriter, r *http.Request, status int, code, message string) {
		responses++
		jsonResponder(w, r, status, code, message)
	}

	handler := Recover(newTestLogger(&buf), respond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("headers already sent")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if responses != 0 {
		t.Errorf("error responder called %d times, want 0", responses)
	}
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

func TestRecoverPassesThroughNormalRequests(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Recover(newTestLogger(&buf), jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("body = %q, want unchanged", rec.Body.String())
	}
	if buf.Len() != 0 {
		t.Errorf("logs = %s, want none", buf.String())
	}
}

func TestRecoverRethrowsErrAbortHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Recover(newTestLogger(&buf), jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("recover() = nil, want http.ErrAbortHandler re-raised")
		}
		err, ok := recovered.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recover() = %v, want http.ErrAbortHandler", recovered)
		}
		if buf.Len() != 0 {
			t.Errorf("logs = %s, want no crash record for an aborted response", buf.String())
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// TestFormatPanicTreatsValueAsData verifies that a panic value is rendered as
// data, so hostile content cannot inject log format directives.
func TestFormatPanicTreatsValueAsData(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Recover(newTestLogger(&buf), jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("%s %s leaked secret")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	logged := buf.String()
	if !strings.Contains(logged, "%!s(MISSING)") && !strings.Contains(logged, "leaked secret") {
		t.Errorf("logs do not contain the panic value as data: %s", logged)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logged)), &record); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	if _, ok := record["stack"]; !ok {
		t.Error("stack attribute missing")
	}
}

func TestFormatPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"error", errors.New("db down"), "db down"},
		{"string", "plain", "plain"},
		{"int", 42, "42"},
		{"struct", struct{ A int }{1}, "{1}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := formatPanic(tt.value); got != tt.want {
				t.Errorf("formatPanic(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// TestRecoverWithRequestLogger verifies that a recovered panic is logged once
// as a request with status 500 and once as a panic, and that the two
// cooperate on the final status.
func TestRecoverWithRequestLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	// Same order as the router: request ID, request log, recovery.
	handler := RequestID(RequestLogger(logger)(Recover(logger, jsonResponder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}))))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/hello", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var requestLogs, panicLogs int
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %v", err)
		}
		switch record["msg"] {
		case "http_request":
			requestLogs++
			if record["status"] != float64(http.StatusInternalServerError) {
				t.Errorf("request log status = %v, want 500", record["status"])
			}
		case "http_panic":
			panicLogs++
		}
	}

	if requestLogs != 1 {
		t.Errorf("request logs = %d, want exactly 1", requestLogs)
	}
	if panicLogs != 1 {
		t.Errorf("panic logs = %d, want exactly 1", panicLogs)
	}
}
