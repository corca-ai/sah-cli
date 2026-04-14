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
go build -trimpath -ldflags="-s -w -X main.version=v0.7.0" -o bin/sah ./cmd/sah
```

## Run

Run from the project root.

```sh
sah auth login
sah run
sah me
sah daemon install
```

`sah daemon install` both installs and starts the per-user background service. Use `sah daemon start` only after a manual `stop` or after logging back into the relevant service manager session.

On Linux, `sah daemon install` writes a per-user `systemd --user` unit and restarts it immediately. On macOS, it writes a per-user `launchd` plist and bootstraps it immediately. Unless you pass `--agent`, `--agents`, or `--rotate-installed`, the install command detects every installed supported agent CLI and persists round-robin mode for the daemon automatically.

If you want the Linux user service to keep running without an active login session, enable lingering first:

```sh
loginctl enable-linger "$USER"
```

If the daemon cannot find `codex`, `gemini`, `claude`, or `qwen`, `sah daemon install` fails before it starts the service. Run `sah agents` to inspect detection, then re-run the install from a shell where at least one supported CLI is already on `PATH`. The install command captures the current shell environment for the background service manager, stores absolute agent binary paths, and runs the daemon from the saved config directory instead of the shell's working directory.

For remote Linux sessions, you can use a text browser during auth:

```sh
export BROWSER=w3m
sah auth login
```

## Test

```sh
go test ./...
```

## Lint

```sh
golangci-lint run
```

To match the Linux CI target from macOS, also run:

```sh
GOOS=linux GOARCH=amd64 golangci-lint run ./...
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
- `GOOS=linux GOARCH=amd64 golangci-lint run ./...`
- `go build -o .tmp-bin/sah ./cmd/sah`

## Release

Pushing a `v*` tag triggers GitHub Actions (`.github/workflows/release.yml`), which runs GoReleaser. It builds macOS and Linux binaries, creates archives with checksums, publishes a GitHub Release, and updates the Homebrew tap.

```sh
git tag v0.7.0
git push origin v0.7.0
```

Required repository secrets:

- `HOMEBREW_TAP_TOKEN`: token with write access to `corca-ai/homebrew-tap`

Configuration is in `.goreleaser.yaml`.
