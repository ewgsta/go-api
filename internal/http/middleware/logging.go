package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// LogResponseWriter wraps http.ResponseWriter to capture the status code and
// the number of bytes written for the request log.
type LogResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

// NewLogResponseWriter wraps w for logging.
func NewLogResponseWriter(w http.ResponseWriter) *LogResponseWriter {
	return &LogResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader records the status code of the first call only, so a handler that
// calls it twice cannot change what the client sees.
func (w *LogResponseWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write records the response size and the implicit 200 status.
func (w *LogResponseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap exposes the underlying writer to http.ResponseController and to
// chi's own middleware.
func (w *LogResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Status returns the recorded status code.
func (w *LogResponseWriter) Status() int { return w.status }

// Bytes returns the number of response body bytes written.
func (w *LogResponseWriter) Bytes() int { return w.bytes }

// loggedKey marks a request whose completion has already been logged. It keeps
// a nested instance of this middleware from emitting a second record for the
// same request.
type loggedKey struct{}

// RequestLogger logs one structured record per completed request.
//
// Only the request path is logged. Query strings, headers, cookies, and bodies
// are excluded because they routinely carry credentials. Status 5xx is logged
// at error level, 4xx at warning, everything else at info.
//
// Applying the middleware more than once still produces exactly one record.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(loggedKey{}) != nil {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			wrapped := NewLogResponseWriter(w)
			ctx := r.WithContext(context.WithValue(r.Context(), loggedKey{}, true))

			// Deferred so a request is still logged when the chain unwinds,
			// including when an inner recovery middleware converts a panic into
			// a 500 before the log line is written.
			defer func() {
				route := chi.RouteContext(ctx.Context()).RoutePattern()
				if route == "" {
					// Unmatched requests have no pattern; the path is still safe
					// because it excludes the query string.
					route = r.URL.Path
				}

				attrs := []any{
					"request_id", RequestIDFrom(ctx.Context()),
					"method", r.Method,
					"path", route,
					"status", wrapped.Status(),
					"bytes", wrapped.Bytes(),
					"duration_ms", float64(time.Since(start).Microseconds()) / 1000,
				}

				switch {
				case wrapped.Status() >= http.StatusInternalServerError:
					logger.ErrorContext(ctx.Context(), "http_request", attrs...)
				case wrapped.Status() >= http.StatusBadRequest:
					logger.WarnContext(ctx.Context(), "http_request", attrs...)
				default:
					logger.InfoContext(ctx.Context(), "http_request", attrs...)
				}
			}()

			next.ServeHTTP(wrapped, ctx)
		})
	}
}
