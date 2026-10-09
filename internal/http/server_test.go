package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// testServerOptions returns timeouts short enough for tests.
func testServerOptions() ServerOptions {
	return ServerOptions{
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		ReadHeaderTimeout: time.Second,
	}
}

// startServer binds a server on an ephemeral port, serves in the background,
// and shuts it down when the test finishes.
func startServer(t *testing.T, handler http.Handler) *Server {
	t.Helper()

	var buf bytes.Buffer
	server := NewServer("127.0.0.1:0", handler, testLogger(&buf), testServerOptions())
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Serve()
	}()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	}

	t.Cleanup(cleanup)
	return server
}

func TestServerServesRequests(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))
	server := startServer(t, router)

	body, status, header := get(t, server, "/healthz")

	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if got := header.Get("X-Request-ID"); !ValidRequestIDForTest(got) {
		t.Errorf("X-Request-ID = %q, want a valid id", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body %q is not valid JSON: %v", body, err)
	}
}

// get performs a request against a running test server and returns the body,
// status, and headers. It always closes the response body.
func get(t *testing.T, server *Server, path string) ([]byte, int, http.Header) {
	t.Helper()

	//nolint:noctx // short-lived test request
	resp, err := http.Get("http://" + server.Addr() + path)
	if err != nil {
		t.Fatalf("GET %s error = %v", path, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", path, err)
	}
	return body, resp.StatusCode, resp.Header
}

func TestServerAppliesTimeouts(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testServerOptions()
	server := NewServer("127.0.0.1:0", http.NotFoundHandler(), testLogger(&buf), opts)

	if server.http.ReadTimeout != opts.ReadTimeout {
		t.Errorf("ReadTimeout = %s, want %s", server.http.ReadTimeout, opts.ReadTimeout)
	}
	if server.http.WriteTimeout != opts.WriteTimeout {
		t.Errorf("WriteTimeout = %s, want %s", server.http.WriteTimeout, opts.WriteTimeout)
	}
	if server.http.IdleTimeout != opts.IdleTimeout {
		t.Errorf("IdleTimeout = %s, want %s", server.http.IdleTimeout, opts.IdleTimeout)
	}
	if server.http.ReadHeaderTimeout != opts.ReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %s, want %s", server.http.ReadHeaderTimeout, opts.ReadHeaderTimeout)
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var buf bytes.Buffer

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-started
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})

	// Route through the server directly so shutdown timing is observable.
	srv := NewServer("127.0.0.1:0", handler, testLogger(&buf), testServerOptions())
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve()
	}()

	type result struct {
		status int
		body   string
		err    error
	}
	results := make(chan result, 1)
	go func() {
		//nolint:noctx // short-lived test request
		resp, err := http.Get("http://" + srv.Addr() + "/slow")
		if err != nil {
			results <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		results <- result{status: resp.StatusCode, body: string(body), err: err}
	}()

	// Give the request time to reach the handler, then shut down while it is
	// still running.
	time.Sleep(50 * time.Millisecond)
	close(started)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if err := <-done; err != nil {
		t.Errorf("Serve() error = %v, want nil after graceful shutdown", err)
	}

	res := <-results
	if res.err != nil {
		t.Fatalf("in-flight request failed during graceful shutdown: %v", res.err)
	}
	if res.status != http.StatusOK {
		t.Errorf("in-flight request status = %d, want 200", res.status)
	}
	if res.body != "done" {
		t.Errorf("in-flight request body = %q, want %q", res.body, "done")
	}
}

func TestServerShutdownBoundedByContext(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	var buf bytes.Buffer

	server := NewServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}), testLogger(&buf), testServerOptions())
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Serve()
	}()

	go func() {
		//nolint:noctx // short-lived test request
		resp, err := http.Get("http://" + server.Addr() + "/block")
		if err != nil {
			return
		}
		_ = resp.Body.Close()
	}()

	time.Sleep(50 * time.Millisecond)

	// A short deadline must expire while the handler is still blocked.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := server.Shutdown(ctx)
	if err == nil {
		t.Fatal("Shutdown() error = nil, want a deadline error")
	}
	if !strings.Contains(err.Error(), "shutdown") {
		t.Errorf("Shutdown() error = %q, want it to mention shutdown", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Shutdown() blocked for %s, want it bounded by the context", elapsed)
	}

	close(block)
}

func TestServerListenFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := NewServer("256.256.256.256:99999", http.NotFoundHandler(), testLogger(&buf), testServerOptions())

	err := server.Listen()
	if err == nil {
		t.Fatal("Listen() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "listen on") {
		t.Errorf("Listen() error = %q, want it to mention the address", err)
	}
}

func TestServerLogsLifecycle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := testLogger(&buf)
	server := NewServer("127.0.0.1:0", http.NotFoundHandler(), logger, testServerOptions())
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Serve()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	out := buf.String()
	for _, msg := range []string{"http_server_started", "http_server_stopped", "http_server_shutdown_complete"} {
		if !strings.Contains(out, msg) {
			t.Errorf("logs missing %q: %s", msg, out)
		}
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
	}
}

func TestServerAddrBeforeListen(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := NewServer("127.0.0.1:1234", http.NotFoundHandler(), testLogger(&buf), testServerOptions())

	if got := server.Addr(); got != "127.0.0.1:1234" {
		t.Errorf("Addr() = %q, want the configured address", got)
	}
}

// TestServerServeRequiresListen documents that binding is an explicit step, so
// Serve never mutates the listener behind a caller's back.
func TestServerServeRequiresListen(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := NewServer("127.0.0.1:0", http.NotFoundHandler(), testLogger(&buf), testServerOptions())

	err := server.Serve()
	if err == nil {
		t.Fatal("Serve() error = nil, want an error when Listen was not called")
	}
	if !strings.Contains(err.Error(), "Listen") {
		t.Errorf("Serve() error = %q, want it to mention Listen", err)
	}
	if !strings.Contains(buf.String(), "http_server_failed") {
		t.Errorf("logs missing failure record: %s", buf.String())
	}
}

func TestServerLogsServeFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := NewServer("127.0.0.1:0", http.NotFoundHandler(), logger, testServerOptions())
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	// Closing the listener before Serve makes the accept loop fail, which is
	// the unexpected-server-failure path.
	if err := server.listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err := server.Serve()
	if err == nil {
		t.Fatal("Serve() error = nil, want an error from the closed listener")
	}
	if errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Serve() error = %v, want a real failure", err)
	}
	if !strings.Contains(buf.String(), "http_server_failed") {
		t.Errorf("logs missing failure record: %s", buf.String())
	}
}
