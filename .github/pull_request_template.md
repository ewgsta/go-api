## What changed

<!-- What does this PR do, and why? -->

## How it was verified

- [ ] `make check` passes (`fmt-check`, `vet`, `test`, `build`)
- [ ] `golangci-lint run` passes
- [ ] Tests added or updated for changed behavior
- [ ] Manual verification described below

<!-- Manual steps, sample requests, or observed output. -->

## Checklist

- [ ] No secrets, tokens, or real credentials are committed
- [ ] Public error messages do not expose internal details
- [ ] Logs do not include headers, cookies, bodies, or query strings
- [ ] README or `.env.example` updated if configuration changed
- [ ] New routes are registered in `internal/http/router.go`

## Notes and limitations

<!-- Anything a reviewer should know: trade-offs, follow-ups, out-of-scope work. -->