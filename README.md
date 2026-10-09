# go-api-starter

A minimal, production-minded Go HTTP API starter template. Clone it, set
environment variables, and you have a running service with structured logging,
request ID propagation, a consistent JSON contract, graceful shutdown, tests,
CI, and a non-root container image — and nothing else.

## Status

This is a starter template, not a finished service. It guarantees that the
project builds, the tests pass, and the documented endpoints behave as
described. It makes no production-readiness claim: there is no database, no
authentication, no rate limiting, and no observability stack. See
[Scope](#scope) for what is deliberately absent.

## Features

- `net/http` server with explicit read, write, idle, and header timeouts
- `chi/v5` router with a single, explicit route registration point
- `log/slog` structured JSON logging to stderr
- Request ID generation, validation, context propagation, `X-Request-ID` header
- Consistent JSON success and error envelopes with stable error codes
- Panic recovery that returns a generic 500 and never leaks internals
- `GET /healthz` and `GET /readyz`
- Environment-variable configuration validated at startup
- Table-driven tests using only `testing` and `net/http/httptest`
- GitHub Actions CI, `golangci-lint`, working Makefile targets, multi-stage
  non-root Dockerfile

## Technology choices

| Concern | Choice |
| --- | --- |
| Language | Go 1.27.2 |
| HTTP server | `net/http` (standard library) |
| Router | `github.com/go-chi/chi/v5` |
| Logging | `log/slog` (standard library) |
| Configuration | Environment variables, standard library parsing |
| Tests | `testing`, `net/http/httptest` |
| Linting | `golangci-lint` v2 |

`chi` is the only third-party dependency. Everything else is the standard
library.

## Prerequisites

- Go 1.27.2 or newer
- Make (optional, but the documented workflow)
- Docker (optional, only for the container image)
- `golangci-lint` (optional, only for `make lint`)

## Quick start

```bash
go build ./...
go run ./cmd/api
```

```bash
curl -i localhost:8080/v1/hello
```

```json
{
  "data": { "message": "API is running" },
  "request_id": "0f1b0e1c7d1a4b0f9a5f2c1d3e4f5a6b"
}
```

Run the full local check suite:

```bash
make check
```

## Configuration

Configuration comes from environment variables only. An invalid value fails at
startup with an actionable message; it is never silently replaced by a default.

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `development` | Environment name: `development`, `staging`, or `production`. |
| `HTTP_ADDR` | `:8080` | Listen address. |
| `HTTP_READ_TIMEOUT` | `10s` | Maximum duration for reading an entire request. |
| `HTTP_WRITE_TIMEOUT` | `15s` | Maximum duration for writing a response. |
| `HTTP_IDLE_TIMEOUT` | `60s` | Maximum duration for keep-alive idle connections. |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Maximum duration for reading request headers. |
| `HTTP_SHUTDOWN_TIMEOUT` | `15s` | Grace period for in-flight requests on shutdown. |
| `LOG_LEVEL` | `info` | Minimum log level: `debug`, `info`, `warn`, `error`. |

See [`.env.example`](.env.example) for annotated values. The application does
not read `.env` files: export the variables in your shell or pass them with
`docker run -e ...`. That keeps the image and CI free of a dotenv dependency.

## Routes

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness. `200` when the process is serving. |
| `GET` | `/readyz` | Readiness. `200` when ready, `503` when a future dependency check fails. |
| `GET` | `/v1/hello` | Sample endpoint demonstrating the response contract. |

Routes are registered in one place: [`internal/http/router.go`](internal/http/router.go).
`/v1/hello` is sample functionality — delete it and register your own.

### Response contract

Success:

```json
{
  "data": { "message": "API is running" },
  "request_id": "0f1b0e1c7d1a4b0f9a5f2c1d3e4f5a6b"
}
```

Error:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "invalid request",
    "details": [{ "field": "name", "reason": "required" }]
  },
  "request_id": "0f1b0e1c7d1a4b0f9a5f2c1d3e4f5a6b"
}
```

`data` and `error` are mutually exclusive. `details` is omitted when empty.
Both envelopes always carry `request_id`.

### Error codes

| Code | HTTP status |
| --- | --- |
| `BAD_REQUEST` | 400 |
| `VALIDATION_ERROR` | 400 |
| `UNAUTHORIZED` | 401 |
| `FORBIDDEN` | 403 |
| `NOT_FOUND` | 404 |
| `METHOD_NOT_ALLOWED` | 405 |
| `CONFLICT` | 409 |
| `RATE_LIMITED` | 429 |
| `INTERNAL_ERROR` | 500 |
| `SERVICE_UNAVAILABLE` | 503 |

The mapping lives in one table in [`internal/http/errors.go`](internal/http/errors.go).
Codes not on the list fall back to `500 INTERNAL_ERROR`.

**Validation policy:** malformed syntax (unparseable JSON, wrong type) is
`BAD_REQUEST`; semantic validation of well-formed input is `VALIDATION_ERROR`.
Both are `400`. This API never returns `422`. `UNAUTHORIZED`, `FORBIDDEN`,
`RATE_LIMITED`, and `CONFLICT` are defined so the contract is ready when a
future project needs them; nothing in the MVP raises them.

## Request IDs and logging

Every request gets an ID:

- A valid incoming `X-Request-ID` is reused.
- A missing or invalid one is replaced with a generated ID. A malformed header
  never fails the request and never reaches a log line unsanitized.
- The ID is stored in the request context, returned in the `X-Request-ID`
  response header, and included in every log record for that request.

Accepted incoming IDs match `^[A-Za-z0-9._-]{1,64}$`. Generated IDs are 32 hex
characters from `crypto/rand`. Request IDs correlate requests within this
service; they are not distributed trace IDs, and this starter does no tracing.

One JSON record per completed request:

```json
{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"http_request","request_id":"...","method":"GET","path":"/v1/hello","status":200,"bytes":72,"duration_ms":1.234}
```

`5xx` logs at `ERROR`, other `4xx` at `WARN`, everything else at `INFO`.
`path` is the matched route pattern when chi has one. Request logs never
include headers, cookies, bodies, or query strings, because those routinely
carry credentials.

## Local development

| Command | Description |
| --- | --- |
| `make help` | List targets. |
| `make run` | Run the API with default configuration. |
| `make build` | Build `bin/api`. |
| `make test` | Run all tests. |
| `make test-race` | Run tests with the race detector and coverage. |
| `make cover` | Run tests and open the coverage report. |
| `make fmt` | Format all Go files. |
| `make fmt-check` | Verify formatting without writing. |
| `make vet` | Run `go vet`. |
| `make lint` | Run `golangci-lint`. |
| `make tidy` | Tidy `go.mod` and `go.sum`. |
| `make check` | `fmt-check`, `vet`, `test`, `build`. |
| `make clean` | Remove build and coverage artifacts. |

## Testing and linting

```bash
go test ./...
go test -race -cover ./...
go vet ./...
gofmt -l .
golangci-lint run
```

Tests use `httptest` only. No test needs network access or an external service.
The server lifecycle tests bind `127.0.0.1:0` and shut down cleanly, so they are
safe to run in parallel and offline.

Install the linter with:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
```

## Docker

```bash
docker build -t go-api-starter .
docker run --rm -p 8080:8080 go-api-starter
curl localhost:8080/healthz
```

The build is multi-stage: a pinned Go toolchain builds a static binary, and the
final Alpine image contains only that binary, a CA bundle, and a non-root user
(uid 10001). The API is PID 1 and handles `SIGTERM`, so `docker stop` triggers
a graceful shutdown instead of killing in-flight requests.

Override configuration the same way you would with any container:

```bash
docker run --rm -p 8080:8080 -e LOG_LEVEL=debug -e HTTP_ADDR=:9090 go-api-starter
```

The image runs as a non-root user. If your platform requires a read-only root
filesystem, mount one and be aware the service needs no writable paths.

## Using this as a GitHub template

1. Click **Use this template**, or clone directly:
   ```bash
   git clone https://github.com/ewgsta/go-api-starter.git my-api
   cd my-api
   ```
2. Rename the module to your repository path:
   ```bash
   go mod edit -module github.com/you/my-api
   grep -rl 'github.com/ewgsta/go-api-starter' --include='*.go' . \
     | xargs sed -i 's|github.com/ewgsta/go-api-starter|github.com/you/my-api|g'
   ```
3. Replace the sample endpoint in `internal/http/router.go`.
4. Update the `LICENSE` copyright line and `.env.example`.
5. Verify:
   ```bash
   make check
   golangci-lint run
   ```

## Security considerations

- The runtime image runs as a non-root user and contains no toolchain or source.
- No secrets are committed; `.env` is git-ignored and `.env.example` holds
  placeholders only.
- Recovered panics return a generic message; stack traces stay in server logs.
- Client errors never echo internal error strings, connection details, or
  driver messages.
- Request logs exclude headers, cookies, bodies, and query strings so
  credentials do not leak into log storage.
- Incoming request IDs are validated against a strict character class and length
  limit before reuse, so a hostile header cannot inject content into logs.
- Timeouts are explicit, which bounds slow-client exposure.
- There is no authentication or authorization. Do not expose this service
  publicly without adding it.
- The linter and CI are quality gates, not security audits.

## Scope

Included: HTTP server lifecycle, routing, request IDs, structured logging, the
JSON error contract, panic recovery, health endpoints, configuration, tests,
CI, and a non-root container image.

Deliberately excluded from the MVP: database access and migrations,
authentication, JWT and API keys, caching, rate limiting, distributed tracing,
metrics exporters, OpenAPI generation, background jobs, WebSockets, file
uploads, CRUD scaffolding, plugin systems, Kubernetes manifests, and deployment
automation. Each is a possible future extension of a project built on this
starter; none exists here today.

## License

[MIT](LICENSE)