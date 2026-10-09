package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRequestIDIsUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id := NewRequestID()
		if id == "" {
			t.Fatal("NewRequestID() returned an empty string")
		}
		if len(id) != requestIDBytes*2 {
			t.Fatalf("NewRequestID() = %q, want %d hex characters", id, requestIDBytes*2)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("NewRequestID() returned duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestValidRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"hex", "0f1b0e1c7d1a4b0f9a5f2c1d3e4f5a6b", true},
		{"dotted", "trace.id-01_x", true},
		{"single char", "a", true},
		{"max length", strings.Repeat("a", maxIncomingRequestIDLen), true},
		{"too long", strings.Repeat("a", maxIncomingRequestIDLen+1), false},
		{"empty", "", false},
		{"log format verb", "%s%s%s%s", false},
		{"newline injection", "abc\ndef", false},
		{"space", "abc def", false},
		{"quote", `abc"def`, false},
		{"json injection", `abc","level":"error`, false},
		{"unicode", "abcé", false},
		{"slash", "abc/def", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ValidRequestID(tt.value); got != tt.want {
				t.Errorf("ValidRequestID(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// echoHandler reports the request ID it observes in the context.
func echoHandler(t *testing.T, captured *string) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestRequestIDGeneratesMissingID(t *testing.T) {
	t.Parallel()

	var captured string
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	RequestID(echoHandler(t, &captured)).ServeHTTP(rec, req)

	header := rec.Header().Get(HeaderRequestID)
	if !ValidRequestID(header) {
		t.Fatalf("%s = %q, want a generated valid id", HeaderRequestID, header)
	}
	if captured != header {
		t.Errorf("context id = %q, header id = %q, want equal", captured, header)
	}
}

func TestRequestIDReusesValidIncomingID(t *testing.T) {
	t.Parallel()

	const incoming = "550e8400-e29b-41d4-a716-446655440000"

	var captured string
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, incoming)

	RequestID(echoHandler(t, &captured)).ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderRequestID); got != incoming {
		t.Errorf("%s = %q, want %q", HeaderRequestID, got, incoming)
	}
	if captured != incoming {
		t.Errorf("context id = %q, want %q", captured, incoming)
	}
}

func TestRequestIDReplacesInvalidIncomingID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"too long", strings.Repeat("x", 200)},
		{"control characters", "abc\x00def"},
		{"format verb", "%!s(MISSING)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var captured string
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(HeaderRequestID, tt.value)

			RequestID(echoHandler(t, &captured)).ServeHTTP(rec, req)

			header := rec.Header().Get(HeaderRequestID)
			if !ValidRequestID(header) {
				t.Fatalf("%s = %q, want a generated valid id", HeaderRequestID, header)
			}
			if header == tt.value {
				t.Errorf("%s = %q, want it replaced", HeaderRequestID, header)
			}
			if captured != header {
				t.Errorf("context id = %q, header id = %q, want equal", captured, header)
			}
		})
	}
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for range 100 {
		var captured string
		rec := httptest.NewRecorder()
		RequestID(echoHandler(t, &captured)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		id := rec.Header().Get(HeaderRequestID)
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate request id %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestRequestIDFromContext(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		if got := RequestIDFrom(context.Background()); got != "" {
			t.Errorf("RequestIDFrom(background) = %q, want empty", got)
		}
	})

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		ctx := ContextWithRequestID(context.Background(), "abc-123")
		if got := RequestIDFrom(ctx); got != "abc-123" {
			t.Errorf("RequestIDFrom(ctx) = %q, want %q", got, "abc-123")
		}
	})
}
