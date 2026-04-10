# sah-cli

SCIENCE@home background contribution CLI for macOS.

## Install

### Homebrew

```sh
brew install corca-ai/tap/sah
```

### From source

```sh
go build -o bin/sah ./cmd/sah
```

## Quick Start

```sh
sah auth login
sah run
```

For always-on background work:

```sh
sah daemon install
```

`sah daemon install` installs and starts the per-user `launchd` agent immediately. It also captures the current shell `PATH` and `HOME`, so re-run it after moving agent binaries or changing how `codex`, `gemini`, or `claude` are installed.

## Documentation

- [Agent Guide](AGENTS.md)
- [Build & Run](docs/build.md)
- [Architecture](docs/architecture.md)
