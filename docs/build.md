# Build & Run

## Prerequisites

A Go toolchain is required. The project uses [mise](https://mise.jdx.dev/) to manage it (`mise.toml`).

```sh
mise install
```

If `go` is not on `PATH`, resolve it through mise:

```sh
export PATH="$(mise where go)/bin:$PATH"
```

## Build

```sh
go build -o bin/sah ./cmd/sah
```

For release builds, inject the version via `ldflags`.

```sh
go build -trimpath -ldflags="-s -w -X main.version=v0.3.0" -o bin/sah ./cmd/sah
```

## Run

Run from the project root.

```sh
sah auth login
sah run
sah me
sah daemon install
```

`sah daemon install` both installs and starts the `launchd` agent. Use `sah daemon start` only after a manual `stop` or after logging back into macOS.

If the daemon cannot find `codex`, `gemini`, or `claude`, re-run `sah daemon install` from a shell where that agent is already on `PATH`. The install command captures the current shell environment for `launchd`, stores absolute agent binary paths, and runs the daemon from `~/Library/Application Support/sah` instead of `HOME`.

## Test

```sh
go test ./...
```

## Lint

```sh
golangci-lint run
```

## Pre-commit Hook

The repository includes a pre-commit hook in `.githooks/`. Enable it once after cloning:

```sh
git config core.hooksPath .githooks
```

The hook runs:

- `go mod verify`
- `go mod tidy` and checks that it does not rewrite `go.mod` or `go.sum`
- `CGO_ENABLED=1 go test -race ./...`
- `golangci-lint config verify`
- `golangci-lint run ./...`
- `go build -o .tmp-bin/sah ./cmd/sah`

## Release

Pushing a `v*` tag triggers GitHub Actions (`.github/workflows/release.yml`), which runs GoReleaser. It builds macOS binaries, creates archives with checksums, publishes a GitHub Release, and updates the Homebrew tap.

```sh
git tag v0.3.0
git push origin v0.3.0
```

Required repository secrets:

- `HOMEBREW_TAP_TOKEN`: token with write access to `corca-ai/homebrew-tap`

Configuration is in `.goreleaser.yaml`.
