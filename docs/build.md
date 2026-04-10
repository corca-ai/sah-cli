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
go build -trimpath -ldflags="-s -w -X main.version=v0.1.0" -o bin/sah ./cmd/sah
```

## Run

Run from the project root.

```sh
sah auth login
sah run
sah me
sah daemon install
```

## Test

```sh
go test ./...
```

## Lint

```sh
golangci-lint run
```

## Release

Pushing a `v*` tag triggers GitHub Actions (`.github/workflows/release.yml`), which runs GoReleaser. It builds macOS binaries, creates archives with checksums, publishes a GitHub Release, and updates the Homebrew tap.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Required repository secrets:

- `HOMEBREW_TAP_TOKEN`: token with write access to `corca-ai/homebrew-tap`

Configuration is in `.goreleaser.yaml`.
