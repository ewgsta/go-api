package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

// startServer binds a server on an ephemeral port and serves in the background.
// The returned cleanup shuts the server down.
func startServer(t *testing.T, handler http.Handler) (*Server, func()) {
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
	return server, cleanup
}

func TestServerServesRequests(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := NewRouter(testLogger(&buf))
	server, _ := startServer(t, router)

	resp, err := http.Get("http://" + server.Addr() + "/healthz") //nolint:noctx // short-lived test request
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-ID"); !ValidRequestIDForTest(got) {
		t.Errorf("X-Request-ID = %q, want a valid id", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body %q is not valid JSON: %v", body, err)
	}
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

	var srv *Server
	// Route through the server directly so shutdown timing is observable.
	server := NewServer("127.0.0.1:0", handler, testLogger(&buf), testServerOptions())
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	srv = server

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
		resp, err := http.Get("http://" + srv.Addr() + "/slow") //nolint:noctx // short-lived test request
		if err != nil {
			results <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		results <- result{status: resp.StatusCode, body: string(body)}
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
		_, _ = http.Get("http://" + server.Addr() + "/block")
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

func TestServerServeWithoutListenBinds(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := NewServer("127.0.0.1:0", http.NotFoundHandler(), testLogger(&buf), testServerOptions())

	done := make(chan error, 1)
	go func() {
		done <- server.Serve()
	}()

	// Serve binds lazily; wait until the address reports a real port.
	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == "127.0.0.1:0" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Errorf("Serve() error = %v", err)
	}
}

func TestServerLogsStartupFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := NewServer("127.0.0.1:70000", http.NotFoundHandler(), logger, testServerOptions())

	if err := server.Serve(); err == nil {
		t.Fatal("Serve() error = nil, want a bind failure")
	}
	if !strings.Contains(buf.String(), "http_server_failed") {
		t.Errorf("logs missing failure record: %s", buf.String())
	}
}
