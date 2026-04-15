# sah-cli

SCIENCE@home background contribution CLI for macOS and Linux.

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
sah
sah help
sah auth login
sah run --once
```

For always-on background work:

```sh
sah daemon install
```

`sah` without extra arguments now acts as the discovery entrypoint. It explains what SCIENCE@home is, shows the machine's current state, and suggests the next command to run. `sah help` shows the full command guide, and successful commands suggest follow-up commands based on the current auth, agent, and daemon state.

`sah daemon install` installs and starts the per-user background service immediately. On macOS it uses `launchd`. On Linux it uses `systemd --user`. By default it detects every installed supported agent CLI (`codex`, `gemini`, `claude`, `qwen`) and round-robins through them. If none are detected, the install fails before the service starts and points you to `sah agents`. In all cases it captures the current shell `PATH`, `HOME`, and the absolute paths of installed agent binaries. Re-run it after moving those binaries.

On Linux, if you want the user service to survive logout and reboot without an active login session, enable lingering for that user with `loginctl enable-linger`.

`sah auth login` now uses a standard OAuth 2.0 Device Authorization Grant. The CLI prints a verification URL and a short code, you complete sign-in in the browser, and the CLI stores an OAuth token set locally once the flow is approved. If you already have a stored legacy contributor API key from an older CLI, the new CLI still accepts it so upgrades do not break existing machines immediately.

Read-only API calls use a private disk-backed HTTP cache that follows server `Cache-Control` headers. Worker claim, submit, release, and auth-polling traffic stays uncached.

Daemon worker logs are written through an internal rotating logger. On macOS they live under `~/Library/Logs/sah/`. On Linux they live under `$XDG_STATE_HOME/sah/` or `~/.local/state/sah/`.

## Documentation

- [Agent Guide](AGENTS.md)
- [Build & Run](docs/build.md)
- [Testing & Quality](docs/testing.md)
- [Architecture](docs/architecture.md)
- [CLI Updates](docs/updates.md)
