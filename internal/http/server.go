package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server owns the HTTP listener and its lifecycle. Handlers and routing live in
// router.go, so this type can be tested without starting the application.
type Server struct {
	http     *http.Server
	listener net.Listener
	logger   *slog.Logger
	addr     string
}

// NewServer builds an HTTP server with explicit timeouts around handler.
func NewServer(addr string, handler http.Handler, logger *slog.Logger, opts ServerOptions) *Server {
	return &Server{
		addr:   addr,
		logger: logger,
		http: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadTimeout:       opts.ReadTimeout,
			WriteTimeout:      opts.WriteTimeout,
			IdleTimeout:       opts.IdleTimeout,
			ReadHeaderTimeout: opts.ReadHeaderTimeout,
		},
	}
}

// ServerOptions carries the timeout values applied to a server. The shutdown
// timeout is not part of it: it bounds the Shutdown call, not the server.
type ServerOptions struct {
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration
}

// Listen binds the configured address without serving traffic. Binding before
// startup is logged makes an address-in-use failure explicit and immediate.
func (s *Server) Listen() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	s.listener = listener
	return nil
}

// Addr returns the bound address. It is useful in tests, where the port is
// assigned by the kernel.
func (s *Server) Addr() string {
	if s.listener == nil {
		return s.addr
	}
	return s.listener.Addr().String()
}

// Serve accepts connections until Shutdown is called. It returns nil on a
// clean shutdown and http.ErrServerClosed otherwise.
func (s *Server) Serve() error {
	if s.listener == nil {
		if err := s.Listen(); err != nil {
			s.logger.Error("http_server_failed", "error", err.Error())
			return err
		}
	}

	s.logger.Info("http_server_started",
		"addr", s.Addr(),
		"read_timeout", s.http.ReadTimeout.String(),
		"write_timeout", s.http.WriteTimeout.String(),
		"idle_timeout", s.http.IdleTimeout.String(),
		"read_header_timeout", s.http.ReadHeaderTimeout.String(),
	)

	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		s.logger.Info("http_server_stopped")
		return nil
	}
	if err != nil {
		s.logger.Error("http_server_failed", "error", err.Error())
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// Shutdown stops accepting connections and waits for in-flight requests,
// bounded by ctx. It returns the context error if the deadline expires first.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	s.logger.Info("http_server_shutdown_complete")
	return nil
}
