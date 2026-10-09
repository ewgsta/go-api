// Package httpapi contains the HTTP layer: routing, the JSON response
// contract, and server lifecycle. The package name differs from its directory
// so it does not shadow the standard net/http import.
package httpapi

import (
	"errors"
	"fmt"
	"net/http"
)

// Error codes returned to clients. Codes are part of the public API contract
// and must remain stable once released.
const (
	CodeBadRequest         = "BAD_REQUEST"
	CodeValidationError    = "VALIDATION_ERROR"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbidden          = "FORBIDDEN"
	CodeNotFound           = "NOT_FOUND"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeConflict           = "CONFLICT"
	CodeRateLimited        = "RATE_LIMITED"
	CodeInternalError      = "INTERNAL_ERROR"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

// statusByCode maps every error code to exactly one HTTP status code. Keeping
// the mapping in one place avoids the 400/422 inconsistency that grows when
// each handler picks its own status.
var statusByCode = map[string]int{
	CodeBadRequest:         http.StatusBadRequest,
	CodeValidationError:    http.StatusBadRequest,
	CodeUnauthorized:       http.StatusUnauthorized,
	CodeForbidden:          http.StatusForbidden,
	CodeNotFound:           http.StatusNotFound,
	CodeMethodNotAllowed:   http.StatusMethodNotAllowed,
	CodeConflict:           http.StatusConflict,
	CodeRateLimited:        http.StatusTooManyRequests,
	CodeInternalError:      http.StatusInternalServerError,
	CodeServiceUnavailable: http.StatusServiceUnavailable,
}

// publicMessage returns the message that may be sent to a client for a code.
// Internal errors get a fixed message so that implementation details, driver
// errors, or stack information never reach the caller.
func publicMessage(code string) string {
	switch code {
	case CodeBadRequest:
		return "malformed request"
	case CodeValidationError:
		return "invalid request"
	case CodeUnauthorized:
		return "authentication required"
	case CodeForbidden:
		return "access denied"
	case CodeNotFound:
		return "resource not found"
	case CodeMethodNotAllowed:
		return "method not allowed"
	case CodeConflict:
		return "resource conflict"
	case CodeRateLimited:
		return "too many requests"
	case CodeServiceUnavailable:
		return "service unavailable"
	case CodeInternalError:
		return "internal server error"
	default:
		return "request failed"
	}
}

// Error is an application error that carries a stable code, a client-safe
// message, and optional field-level details.
type Error struct {
	// Code is a stable, machine-readable identifier.
	Code string
	// Message is safe to return to the client.
	Message string
	// Details describes individual invalid fields. It may be empty.
	Details []FieldError
	// cause is the internal cause. It is logged, never returned to clients.
	cause error
}

// FieldError describes why a single field was rejected.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// NewError returns an Error with the public message defined for code.
func NewError(code string) *Error {
	return &Error{Code: code, Message: publicMessage(code)}
}

// Errorf returns an Error with a custom client-safe message.
func Errorf(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// ValidationError returns a 400 validation error with the given field details.
func ValidationError(details ...FieldError) *Error {
	err := NewError(CodeValidationError)
	err.Details = details
	return err
}

// NewInternalError wraps an internal cause as a 500 error. The cause is kept
// for server-side logging only.
func NewInternalError(cause error) *Error {
	return &Error{Code: CodeInternalError, Message: publicMessage(CodeInternalError), cause: cause}
}

// Wrap attaches an internal cause, for example a storage error, to err.
func (e *Error) Wrap(cause error) *Error {
	e.cause = cause
	return e
}

// Unwrap exposes the internal cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// Status returns the HTTP status code for the error's code.
func (e *Error) Status() int {
	if status, ok := statusByCode[e.Code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// asError converts err into an *Error. Unknown error types become a generic
// internal error so that raw Go error strings are never exposed to clients.
func asError(err error) *Error {
	var appErr *Error
	if !errors.As(err, &appErr) || appErr == nil {
		return NewInternalError(err)
	}
	if appErr.Code == "" || statusByCode[appErr.Code] == 0 {
		appErr.Code = CodeInternalError
	}
	if appErr.Message == "" {
		appErr.Message = publicMessage(appErr.Code)
	}
	return appErr
}
