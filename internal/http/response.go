package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ewgsta/go-api-starter/internal/http/middleware"
)

// contentTypeJSON is the only content type this API emits.
const contentTypeJSON = "application/json; charset=utf-8"

// successEnvelope is the wire format for successful responses.
type successEnvelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

// errorBody is the wire format of the "error" object.
type errorBody struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []FieldError `json:"details,omitempty"`
}

// errorEnvelope is the wire format for error responses.
type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

// JSON writes data as the "data" field of the success envelope with the given
// status code. It does not overwrite an already written response.
func JSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeJSON(w, r, status, successEnvelope{
		Data:      data,
		RequestID: requestID(r),
	})
}

// ErrorResponse writes err as the error envelope. Non-*Error values are
// converted to a generic internal error so internal details are never sent.
func ErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	appErr := asError(err)
	if appErr.Code == CodeInternalError {
		// The cause is useful in logs and useless (or harmful) to clients.
		loggerFrom(r).ErrorContext(r.Context(), "http_error",
			"error", appErr.Error(),
			"code", appErr.Code,
		)
	}

	writeJSON(w, r, appErr.Status(), errorEnvelope{
		Error: errorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
			Details: appErr.Details,
		},
		RequestID: requestID(r),
	})
}

// writeJSON encodes the envelope and writes it exactly once.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, envelope any) {
	body, err := json.Marshal(envelope)
	if err != nil {
		// Encoding failed: fall back to a hand-built payload. Status and
		// headers are untouched until now, so a single response is still sent.
		loggerFrom(r).ErrorContext(r.Context(), "http_encoding_failed", "error", err.Error())
		body, status = encodingFallback(requestID(r)), http.StatusInternalServerError
	}

	header := w.Header()
	header.Set("Content-Type", contentTypeJSON)
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	if _, err := w.Write(body); err != nil {
		// The status line is already sent, so nothing can be corrected here.
		loggerFrom(r).WarnContext(r.Context(), "http_response_write_failed", "error", err.Error())
	}
}

// encodingFallback builds the error envelope by hand for the case where the
// payload cannot be encoded. The request ID is inserted as a JSON string
// rather than concatenated raw.
func encodingFallback(requestID string) []byte {
	return []byte(`{"error":{"code":"INTERNAL_ERROR","message":"internal server error"},"request_id":` +
		jsonString(requestID) + `}`)
}

// jsonString renders s as a JSON string.
func jsonString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

func requestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	return middleware.RequestIDFrom(r.Context())
}

// loggerFrom returns the request's logger, so response-level diagnostics honor
// the configured level and sink instead of the global default.
func loggerFrom(r *http.Request) *slog.Logger {
	if r == nil {
		return slog.Default()
	}
	return middleware.LoggerFrom(r.Context())
}
