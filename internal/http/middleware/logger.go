package middleware

import (
	"context"
	"log/slog"
	"net/http"
)

type loggerKey struct{}

// WithLogger returns a middleware that stores logger in the request context so
// downstream code, including the response helpers, logs through the same
// configured logger instead of the global default.
func WithLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ContextWithLogger(r.Context(), logger)))
		})
	}
}

// ContextWithLogger returns a copy of ctx carrying logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFrom returns the logger stored in ctx, or the default logger when the
// middleware is not installed.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}
