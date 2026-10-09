// Command api runs the HTTP service: it loads configuration, starts the
// server, and shuts it down gracefully on SIGINT or SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	httpapi "github.com/ewgsta/go-api-starter/internal/http"

	"github.com/ewgsta/go-api-starter/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}

// run wires the application together. It returns an error instead of exiting so
// that deferred cleanup runs before the process terminates with a non-zero
// status.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	logger := newLogger(cfg)
	logger.Info("api_starting",
		"environment", cfg.Environment,
		"addr", cfg.HTTP.Addr,
		"log_level", cfg.Log.Level.String(),
	)

	server := httpapi.NewServer(
		cfg.HTTP.Addr,
		httpapi.NewRouter(logger),
		logger,
		httpapi.ServerOptions{
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       cfg.HTTP.IdleTimeout,
			ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		},
	)

	// Bind before serving so an address-in-use failure is reported before any
	// request is accepted.
	if err := server.Listen(); err != nil {
		logger.Error("api_start_failed", "error", err.Error())
		return err
	}

	// Signals are registered before Serve so a termination request during
	// startup is not missed.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve()
	}()

	select {
	case err := <-serveErr:
		// The server stopped on its own. A non-nil error is an unexpected
		// failure and must surface as a non-zero exit.
		return err
	case <-ctx.Done():
		stop()
		logger.Info("api_shutdown_signal_received")
	}

	// Bounded grace period: in-flight requests can finish, but a stuck handler
	// cannot hold the process open indefinitely.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("api_shutdown_failed", "error", err.Error())
		return err
	}

	// Serve returns once Shutdown completes. Collect the result so an
	// unexpected server failure that raced with the signal is not lost.
	if err := <-serveErr; err != nil {
		return err
	}

	logger.Info("api_stopped")
	return nil
}

// newLogger builds the structured JSON logger. Logs go to stderr so stdout
// remains free for process supervisors.
func newLogger(cfg config.Config) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.Log.Level}))
}
