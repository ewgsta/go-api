package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ewgsta/go-api-starter/internal/http/middleware"
)

// withRequestID attaches a known request ID to the request context.
func withRequestID(r *http.Request, id string) *http.Request {
	return r.WithContext(middleware.ContextWithRequestID(context.Background(), id))
}

// decode unmarshals the response body into v.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()

	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("body %q is not valid JSON: %v", rec.Body.String(), err)
	}
}

func TestJSONSuccessEnvelope(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/v1/hello", nil), "req-1")

	JSON(rec, req, http.StatusOK, map[string]string{"message": "API is running"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != contentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	var payload struct {
		Data      map[string]string `json:"data"`
		RequestID string            `json:"request_id"`
		Error     *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, rec, &payload)

	if payload.Data["message"] != "API is running" {
		t.Errorf("data.message = %q, want %q", payload.Data["message"], "API is running")
	}
	if payload.RequestID != "req-1" {
		t.Errorf("request_id = %q, want %q", payload.RequestID, "req-1")
	}
	if payload.Error != nil {
		t.Errorf("error = %+v, want absent in a success response", payload.Error)
	}
}

func TestJSONStatuses(t *testing.T) {
	t.Parallel()

	statuses := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent,
	}

	for _, status := range statuses {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req")

			JSON(rec, req, status, map[string]string{"k": "v"})

			if rec.Code != status {
				t.Errorf("status = %d, want %d", rec.Code, status)
			}
		})
	}
}

// TestJSONEncodingFailure verifies the fallback when a payload cannot be
// encoded: the client still receives exactly one valid JSON response.
func TestJSONEncodingFailure(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req-enc")

	// A channel cannot be marshaled to JSON.
	JSON(rec, req, http.StatusOK, make(chan int))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	decode(t, rec, &payload)

	if payload.Error.Code != CodeInternalError {
		t.Errorf("code = %q, want %q", payload.Error.Code, CodeInternalError)
	}
	if payload.RequestID != "req-enc" {
		t.Errorf("request_id = %q, want %q", payload.RequestID, "req-enc")
	}
}

func TestErrorResponseEnvelope(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/v1/things", nil), "req-2")

	ErrorResponse(rec, req, ValidationError(
		FieldError{Field: "name", Reason: "required"},
	))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details []struct {
				Field  string `json:"field"`
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	decode(t, rec, &payload)

	if payload.Error.Code != CodeValidationError {
		t.Errorf("code = %q, want %q", payload.Error.Code, CodeValidationError)
	}
	if payload.RequestID != "req-2" {
		t.Errorf("request_id = %q, want %q", payload.RequestID, "req-2")
	}
	if len(payload.Error.Details) != 1 {
		t.Fatalf("details = %+v, want 1 entry", payload.Error.Details)
	}
	if payload.Error.Details[0].Field != "name" || payload.Error.Details[0].Reason != "required" {
		t.Errorf("details[0] = %+v, want name/required", payload.Error.Details[0])
	}
}

// TestErrorResponseHidesInternalErrors verifies that a raw internal error is
// never reflected to the client.
func TestErrorResponseHidesInternalErrors(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req-3")

	internal := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	ErrorResponse(rec, req, internal)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	for _, leaked := range []string{"10.0.0.5", "5432", "connection refused", "dial tcp"} {
		if strings.Contains(body, leaked) {
			t.Errorf("body leaks %q: %s", leaked, body)
		}
	}

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decode(t, rec, &payload)

	if payload.Error.Code != CodeInternalError {
		t.Errorf("code = %q, want %q", payload.Error.Code, CodeInternalError)
	}
	if payload.Error.Message != "internal server error" {
		t.Errorf("message = %q, want %q", payload.Error.Message, "internal server error")
	}
}

func TestErrorResponseStatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"bad request", NewError(CodeBadRequest), http.StatusBadRequest, CodeBadRequest},
		{"validation", ValidationError(), http.StatusBadRequest, CodeValidationError},
		{"unauthorized", NewError(CodeUnauthorized), http.StatusUnauthorized, CodeUnauthorized},
		{"forbidden", NewError(CodeForbidden), http.StatusForbidden, CodeForbidden},
		{"not found", NewError(CodeNotFound), http.StatusNotFound, CodeNotFound},
		{"method not allowed", NewError(CodeMethodNotAllowed), http.StatusMethodNotAllowed, CodeMethodNotAllowed},
		{"conflict", NewError(CodeConflict), http.StatusConflict, CodeConflict},
		{"rate limited", NewError(CodeRateLimited), http.StatusTooManyRequests, CodeRateLimited},
		{"internal", NewInternalError(errors.New("x")), http.StatusInternalServerError, CodeInternalError},
		{"unavailable", NewError(CodeServiceUnavailable), http.StatusServiceUnavailable, CodeServiceUnavailable},
		{"unknown code", &Error{Code: "TEAPOT"}, http.StatusInternalServerError, CodeInternalError},
		{"nil-ish wrapped", fmt.Errorf("layer: %w", NewError(CodeConflict)), http.StatusConflict, CodeConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req-map")

			ErrorResponse(rec, req, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			decode(t, rec, &payload)
			if payload.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", payload.Error.Code, tt.wantCode)
			}
		})
	}
}

// TestNo422 documents the validation policy: this API never returns 422.
func TestNo422(t *testing.T) {
	t.Parallel()

	if _, ok := statusByCode[CodeValidationError]; !ok {
		t.Fatal("VALIDATION_ERROR is not mapped")
	}
	for code, status := range statusByCode {
		if status == http.StatusUnprocessableEntity {
			t.Errorf("code %q maps to 422, which the API does not use", code)
		}
	}
}

func TestValidationPolicyIsAlways400(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req-v")

	ErrorResponse(rec, req, ValidationError(FieldError{Field: "email", Reason: "invalid format"}))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestErrorOmitsEmptyDetails(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := withRequestID(httptest.NewRequest(http.MethodGet, "/", nil), "req-d")

	ErrorResponse(rec, req, NewError(CodeNotFound))

	var payload map[string]any
	decode(t, rec, &payload)

	errorObj, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %v, want an object", payload["error"])
	}
	if _, present := errorObj["details"]; present {
		t.Errorf("details present for an error without details: %s", rec.Body.String())
	}
}

func TestErrorWrapAndUnwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")
	err := NewError(CodeServiceUnavailable).Wrap(cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is() = false, want true for the wrapped cause")
	}
	if got := err.Status(); got != http.StatusServiceUnavailable {
		t.Errorf("Status() = %d, want 503", got)
	}
	if err.Error() == "" {
		t.Error("Error() is empty")
	}
}

func TestRequestIDWithoutMiddleware(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	JSON(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, map[string]string{"k": "v"})

	var payload map[string]any
	decode(t, rec, &payload)

	id, ok := payload["request_id"].(string)
	if !ok {
		t.Fatalf("request_id = %v, want a string", payload["request_id"])
	}
	if id != "" {
		t.Errorf("request_id = %q, want empty without the middleware", id)
	}
}
