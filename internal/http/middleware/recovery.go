package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// ErrorResponder writes a minimal error response. It is the subset of the
// response layer that the recovery middleware needs: depending on a function
// keeps this package free of an import cycle with the response helpers and
// keeps recovery testable on its own.
type ErrorResponder func(w http.ResponseWriter, r *http.Request, status int, code, message string)

// Recover converts a panic in the handler chain into a 500 response.
//
// Behavior and limitations:
//
//   - The panic value, the request ID, and the stack trace are logged
//     server-side. None of that reaches the client.
//   - If the response has already been committed (headers or body written), no
//     second response is attempted. The connection is left as-is and the
//     client sees a truncated body. This is unavoidable: HTTP/1.1 has no way to
//     retract bytes already sent.
//   - The panic is not re-raised, so the process keeps serving traffic. Use the
//     crash log for alerting.
func Recover(logger *slog.Logger, respond ErrorResponder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := NewLogResponseWriter(w)

			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				// http.ErrAbortHandler is the documented way for a handler to
				// abort a response; it must not be treated as a crash.
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}

				logger.ErrorContext(r.Context(), "http_panic",
					"request_id", RequestIDFrom(r.Context()),
					"panic", formatPanic(recovered),
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)

				if recorder.wrote {
					// Headers or body are already on the wire.
					return
				}

				respond(recorder, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}()

			next.ServeHTTP(recorder, r)
		})
	}
}

// formatPanic renders a panic value for logging. The value is passed as an
// argument to %v rather than used as a format string, so untrusted content
// cannot introduce formatting verbs, and the result is a single slog value
// rather than a message template.
func formatPanic(value any) string {
	if err, ok := value.(error); ok {
		return err.Error()
	}
	return fmt.Sprintf("%v", value)
}
