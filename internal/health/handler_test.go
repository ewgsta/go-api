package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// responder stands in for the response layer. The handler contract is the
// status code it forwards and the data value it passes along.
func responder(w http.ResponseWriter, r *http.Request, status int, data any) {
	body, err := json.Marshal(map[string]any{
		"data":       data,
		"request_id": "req-test",
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func TestLive(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Live(responder)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var payload struct {
		Data struct {
			Status string `json:"status"`
			Uptime string `json:"uptime"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if payload.Data.Status != "ok" {
		t.Errorf("status = %q, want %q", payload.Data.Status, "ok")
	}
	if payload.Data.Uptime == "" {
		t.Error("uptime is empty, want a duration")
	}
	if payload.RequestID != "req-test" {
		t.Errorf("request_id = %q, want %q", payload.RequestID, "req-test")
	}
}

func TestReady(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Ready(responder)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if payload.Data.Status != "ready" {
		t.Errorf("status = %q, want %q", payload.Data.Status, "ready")
	}
}

// TestHealthEndpointsDoNotCheckDependencies documents that neither endpoint
// inspects external services: the MVP has none, and readiness must not claim
// otherwise.
func TestHealthEndpointsDoNotCheckDependencies(t *testing.T) {
	t.Parallel()

	endpoints := map[string]http.HandlerFunc{
		"liveness":  Live(responder),
		"readiness": Ready(responder),
	}

	for name, handler := range endpoints {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			for _, forbidden := range []string{"database", "postgres", "redis", "dial"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("body mentions %q: %s", forbidden, body)
				}
			}
		})
	}
}
