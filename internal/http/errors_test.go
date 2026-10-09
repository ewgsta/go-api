package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestNewErrorMessages(t *testing.T) {
	t.Parallel()

	codes := []string{
		CodeBadRequest,
		CodeValidationError,
		CodeUnauthorized,
		CodeForbidden,
		CodeNotFound,
		CodeMethodNotAllowed,
		CodeConflict,
		CodeRateLimited,
		CodeInternalError,
		CodeServiceUnavailable,
	}

	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			err := NewError(code)
			if err.Code != code {
				t.Errorf("Code = %q, want %q", err.Code, code)
			}
			if err.Message == "" {
				t.Error("Message is empty, want a client-safe default")
			}
			if got := err.Status(); got != statusByCode[code] {
				t.Errorf("Status() = %d, want %d", got, statusByCode[code])
			}
		})
	}
}

func TestNewInternalErrorKeepsCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("secret detail")
	err := NewInternalError(cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is() = false, want the cause to be reachable")
	}
	if err.Message != "internal server error" {
		t.Errorf("Message = %q, want %q", err.Message, "internal server error")
	}
	if got := err.Status(); got != http.StatusInternalServerError {
		t.Errorf("Status() = %d, want 500", got)
	}
}

func TestErrorfUsesCustomMessage(t *testing.T) {
	t.Parallel()

	err := Errorf(CodeNotFound, "widget 42 does not exist")
	if err.Message != "widget 42 does not exist" {
		t.Errorf("Message = %q, want the custom message", err.Message)
	}
	if got := err.Status(); got != http.StatusNotFound {
		t.Errorf("Status() = %d, want 404", got)
	}
}

func TestErrorErrorString(t *testing.T) {
	t.Parallel()

	plain := NewError(CodeNotFound)
	if plain.Error() == "" {
		t.Error("Error() is empty")
	}

	withCause := NewError(CodeServiceUnavailable).Wrap(errors.New("pool exhausted"))
	if got := withCause.Error(); got == "" || !contains(got, "pool exhausted") {
		t.Errorf("Error() = %q, want it to include the cause", got)
	}
}

func TestAsError(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")

	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantStatus int
	}{
		{"typed", NewError(CodeConflict), CodeConflict, http.StatusConflict},
		{"wrapped typed", fmt.Errorf("service: %w", NewError(CodeNotFound)), CodeNotFound, http.StatusNotFound},
		{"internal", NewInternalError(cause), CodeInternalError, http.StatusInternalServerError},
		{"plain", cause, CodeInternalError, http.StatusInternalServerError},
		{"unknown code", &Error{Code: "MYSTERY"}, CodeInternalError, http.StatusInternalServerError},
		{"missing code", &Error{Message: "oops"}, CodeInternalError, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := asError(tt.err)
			if got.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", got.Code, tt.wantCode)
			}
			if got.Status() != tt.wantStatus {
				t.Errorf("Status() = %d, want %d", got.Status(), tt.wantStatus)
			}
			if got.Message == "" {
				t.Error("Message is empty")
			}
		})
	}
}

// TestAsErrorNeverLeaksInternalText verifies that a non-application error is
// reported with a fixed message only.
func TestAsErrorNeverLeaksInternalText(t *testing.T) {
	t.Parallel()

	got := asError(errors.New("password=hunter2 host=db.internal"))
	if contains(got.Message, "hunter2") || contains(got.Message, "db.internal") {
		t.Errorf("Message = %q, want a generic message", got.Message)
	}
}

func TestValidationErrorDetails(t *testing.T) {
	t.Parallel()

	err := ValidationError(
		FieldError{Field: "name", Reason: "required"},
		FieldError{Field: "email", Reason: "invalid format"},
	)

	if len(err.Details) != 2 {
		t.Fatalf("Details = %+v, want 2 entries", err.Details)
	}
	if got := err.Status(); got != http.StatusBadRequest {
		t.Errorf("Status() = %d, want 400", got)
	}
	if err.Message != publicMessage(CodeValidationError) {
		t.Errorf("Message = %q, want %q", err.Message, publicMessage(CodeValidationError))
	}
}

func TestEveryCodeHasStatus(t *testing.T) {
	t.Parallel()

	known := []string{
		CodeBadRequest, CodeValidationError, CodeUnauthorized, CodeForbidden,
		CodeNotFound, CodeMethodNotAllowed, CodeConflict, CodeRateLimited,
		CodeInternalError, CodeServiceUnavailable,
	}

	for _, code := range known {
		if _, ok := statusByCode[code]; !ok {
			t.Errorf("code %q has no status mapping", code)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
