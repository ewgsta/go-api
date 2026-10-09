# go-api-starter

A minimal, production-minded Go HTTP API starter template.

## Project goals

- A clean, clone-and-run starting point for small Go HTTP services.
- Predictable behavior: explicit timeouts, validated configuration, one JSON
  response contract, graceful shutdown.
- Small dependency surface: `net/http`, `chi/v5`, `log/slog`, and the standard
  library only.
- Tests that run without network access or external services.

## Scope

In scope: HTTP server lifecycle, routing, request IDs, structured logging,
JSON success and error envelopes, panic recovery, health endpoints,
configuration, tests, CI, and a non-root container image.

Out of scope for the MVP: databases, migrations, authentication, caching, rate
limiting, tracing, metrics exporters, code generation, background jobs,
WebSockets, file uploads, CRUD scaffolding, and deployment automation.

## Toolchain

Go 1.27.2 (latest stable release).

## License

[MIT](LICENSE)

Full usage documentation is added in the final phase of this repository's
initialization.