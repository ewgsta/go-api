// Package health implements the liveness and readiness endpoints.
//
// The MVP has no external dependencies, so readiness reports process-level
// readiness only. It must not pretend to check services that do not exist.
// Add dependency checks in Ready when the project gains real dependencies.
package health

import (
	"net/http"
	"time"
)

// startedAt is the process start time used for the liveness uptime field.
var startedAt = time.Now()

// JSONFunc writes a success response using the shared response envelope. It is
// injected by the caller so this package does not depend on the HTTP layer,
// which already depends on this package to register the routes.
type JSONFunc func(w http.ResponseWriter, r *http.Request, status int, data any)

// Live reports that the process is running and able to serve requests.
//
// Liveness stays cheap and dependency-free: a failing dependency should not
// cause an orchestrator to restart an otherwise healthy process.
func Live(respond JSONFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, map[string]string{
			"status": "ok",
			"uptime": time.Since(startedAt).Round(time.Second).String(),
		})
	}
}

// Ready reports whether the application is ready to accept traffic.
//
// The current implementation has nothing to wait for, so it always returns
// 200 with the shared envelope. When a database or cache is added, run its
// check here and return http.StatusServiceUnavailable with
// "SERVICE_UNAVAILABLE" while the application is not able to serve.
func Ready(respond JSONFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, map[string]string{"status": "ready"})
	}
}
