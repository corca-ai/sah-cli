# Testing & Quality

## Goal

Keep `sah-cli` easy to change without regressing worker behavior, CLI guidance, or compatibility with already deployed SCIENCE@home servers.

## Test Layers

- `internal/sah/*_test.go`: unit tests for worker protocol handling, auth helpers, service-manager integration, payload parsing, summaries, and compatibility shims.
- `cmd/sah/*_test.go`: command-facing tests for discovery output, help text, onboarding suggestions, and user-visible follow-up guidance.
- HTTP client tests use `httptest.Server` to lock request paths, headers, and JSON decoding against the current API contract.

## Compatibility Rules

- When adding a new protocol path, keep existing fallbacks working until the deployed API no longer needs them.
- When code accepts both current and legacy response shapes, leave a short code comment at the fallback branch and add a test that proves both shapes still work.
- Keep examples and command descriptions aligned with the current onboarding and daemon behavior. Update docs in the same change when CLI output meaning changes.

## Quality Gates

Run these before pushing:

```sh
go test ./...
./scripts/check_coverage.sh 35
golangci-lint run ./...
GOOS=linux GOARCH=amd64 golangci-lint run ./...
```

Coverage is enforced through `./scripts/check_coverage.sh` and must stay at or above `35%`.

`golangci-lint` also enforces:

- `gocognit` for cognitive complexity
- `funlen` for function length
- `dupl` for duplicated blocks

## Pre-commit Expectations

The shared `.githooks/pre-commit` hook runs the same coverage and lint gates plus:

- `go mod verify`
- `go mod tidy` with a no-diff check
- `CGO_ENABLED=1 go test -race ./...`
- `go build -o .tmp-bin/sah ./cmd/sah`

Enable it once per clone:

```sh
git config core.hooksPath .githooks
```

## Change Checklist

- Add or update tests for every user-visible command behavior change.
- Add or update compatibility tests when touching legacy fields, fallback routes, or worker contract negotiation.
- Keep docs small and topic-specific. If build instructions grow test-specific detail, move that detail here and link back from `build.md` or `AGENTS.md`.
