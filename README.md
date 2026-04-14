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

Daemon worker logs are written through an internal rotating logger. On macOS they live under `~/Library/Logs/sah/`. On Linux they live under `$XDG_STATE_HOME/sah/` or `~/.local/state/sah/`.

For remote Linux sessions, set `BROWSER` to a text browser before `sah auth login`, for example:

```sh
export BROWSER=w3m
```

## Documentation

- [Agent Guide](AGENTS.md)
- [Build & Run](docs/build.md)
- [Architecture](docs/architecture.md)
- [CLI Updates](docs/updates.md)
