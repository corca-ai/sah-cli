# sah-cli

SCIENCE@home background contribution CLI for macOS.

## Install

### Homebrew

```sh
brew install corca-ai/tap/sah-cli
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

`sah daemon install` installs and starts the per-user `launchd` agent immediately. It captures the current shell `PATH`, `HOME`, and the absolute paths of installed agent binaries, then runs from `~/Library/Application Support/sah` instead of your home directory. Re-run it after moving `codex`, `gemini`, or `claude`.

## Documentation

- [Agent Guide](AGENTS.md)
- [Build & Run](docs/build.md)
- [Architecture](docs/architecture.md)
