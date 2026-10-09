// Package middleware provides the HTTP middleware chain used by the router.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

type contextKey struct{}

// requestIDKey is the context key under which the request ID is stored. A
// private type prevents collisions with keys defined elsewhere.
var requestIDKey = contextKey{}

const (
	// HeaderRequestID is the request and response header carrying the ID.
	HeaderRequestID = "X-Request-ID"

	// maxIncomingRequestIDLen bounds accepted client-supplied IDs. Longer
	// values are replaced instead of trusted.
	maxIncomingRequestIDLen = 64

	// requestIDBytes is the entropy of a generated ID.
	requestIDBytes = 16
)

// incomingRequestIDPattern accepts conservative, log-safe IDs: ASCII letters,
// digits, dot, underscore, and hyphen. Anything else, including whitespace,
// control characters, and log format verbs, is rejected.
var incomingRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// NewRequestID returns a random, hex-encoded request ID. It uses
// crypto/rand because request IDs are not secrets but must be unpredictable
// enough to avoid collisions across concurrent requests.
func NewRequestID() string {
	var buf [requestIDBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand.Read never fails on supported platforms; falling back to
		// a fixed-length zero ID keeps the request flow alive if it ever does.
		return hex.EncodeToString(make([]byte, requestIDBytes))
	}
	return hex.EncodeToString(buf[:])
}

// ValidRequestID reports whether an incoming header value may be reused as a
// request ID.
func ValidRequestID(value string) bool {
	return incomingRequestIDPattern.MatchString(value)
}

// RequestIDFrom returns the request ID stored in ctx, or an empty string when
// the middleware is not installed.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// ContextWithRequestID returns a copy of ctx carrying id.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID assigns a request ID to every request, stores it in the request
// context, and echoes it in the X-Request-ID response header.
//
// A valid incoming ID is reused so that a proxy or client can correlate calls.
// A missing or invalid ID is replaced with a generated one rather than
// rejected: a malformed header should not turn into a failed request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incoming := r.Header.Get(HeaderRequestID)
		if !ValidRequestID(incoming) {
			incoming = NewRequestID()
		}

		w.Header().Set(HeaderRequestID, incoming)
		next.ServeHTTP(w, r.WithContext(ContextWithRequestID(r.Context(), incoming)))
	})
}
