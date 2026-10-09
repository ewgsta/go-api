package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ewgsta/go-api-starter/internal/health"
	"github.com/ewgsta/go-api-starter/internal/http/middleware"
)

// NewRouter builds the application router with the middleware chain installed.
// Middleware order is outermost first: request ID, request log, recovery.
//
// The ordering matters. WithLogger runs early so the response helpers share the
// configured logger. Recovery sits innermost so it catches a panic before it
// unwinds past the request logger, and it writes the 500 through the writer
// that the logger wraps, so the log line carries the final 500 status and the
// request is logged exactly once.
func NewRouter(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.WithLogger(logger))
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.Recover(logger, writeInternalError))

	// Health endpoints are registered outside the versioned API because
	// orchestrators call them directly.
	r.Get("/healthz", health.Live(JSON))
	r.Get("/readyz", health.Ready(JSON))

	// Sample route demonstrating the JSON contract. Delete or replace it in
	// your own project.
	r.Get("/v1/hello", hello)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		ErrorResponse(w, r, NewError(CodeNotFound))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		ErrorResponse(w, r, NewError(CodeMethodNotAllowed))
	})

	return r
}

// hello is sample functionality showing the JSON success contract, request ID
// propagation, and logging. Replace or delete it in your own project.
func hello(w http.ResponseWriter, r *http.Request) {
	JSON(w, r, http.StatusOK, map[string]string{"message": "API is running"})
}

// writeInternalError adapts the response layer to the recovery middleware so
// that recovered panics produce the same error envelope as any other failure.
func writeInternalError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, r, status, errorEnvelope{
		Error:     errorBody{Code: code, Message: message},
		RequestID: requestID(r),
	})
}
