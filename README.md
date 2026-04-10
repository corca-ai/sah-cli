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

## Documentation

- [Agent Guide](AGENTS.md)
- [Build & Run](docs/build.md)
- [Architecture](docs/architecture.md)
