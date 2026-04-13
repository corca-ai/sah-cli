# Architecture

## Purpose

`sah` is the SCIENCE@home CLI worker for macOS and Linux. It authenticates a contributor account, polls for assignments, runs a local coding agent CLI in a restricted empty workspace, and submits the resulting JSON payload.

## Auth Flow

1. `sah auth login` opens a browser to `https://sah.borca.ai/cli/authorize`.
2. `sah-web` authenticates the browser session with Google if needed.
3. `sah-web` returns a short-lived signed auth code to the loopback callback on `127.0.0.1`.
4. `sah` exchanges that code for the contributor API key through `POST /api/cli/exchange`.
5. `sah` stores the API key in the local config file with user-only permissions.

The browser never receives the raw contributor API key. The local CLI proves possession with a verifier generated before the browser is opened. On Linux, `sah` respects `BROWSER`, so remote sessions can use text browsers such as `w3m` or `lynx`.

## Worker Loop

- `sah` calls `GET /s@h/tasks` with `X-API-Key`.
- The assignment response can include a protocol version plus `_links.submit` and `_links.release`, and the same submit/release relations can also be exposed through the HTTP `Link` header.
- It builds a task-only prompt from the returned assignment payload and instructions.
- It runs one of the supported local agent CLIs: `codex`, `gemini`, `claude`, `qwen`, `hermes`, or `openclaw`.
- The agent receives no API key and runs in an empty temporary working directory.
- The CLI parses the agent stdout as JSON and follows the assignment-scoped submit link when present, falling back to the legacy contribution endpoint when it is not.
- If the local agent aborts, fails locally, or hits a submission error, `sah` releases the assignment immediately so the API key does not stay pinned at the open-assignment limit until expiry.

This keeps the CLI forward-compatible with new task families. As long as the server still returns one assignment payload plus one submission schema, the CLI does not need to know new task-specific endpoints ahead of time.

## Commands

- `sah auth login|logout|status`
- `sah run`
- `sah daemon install|start|stop|status|uninstall`
- `sah me`
- `sah contributions`
- `sah leaderboard`
- `sah agents`

## Local Files

- macOS config: `~/Library/Application Support/sah/config.json`
- Linux config: `$XDG_CONFIG_HOME/sah/config.json` or `~/.config/sah/config.json`
- macOS daemon logs: `~/Library/Logs/sah/daemon.stdout.log` and `~/Library/Logs/sah/daemon.stderr.log`
- Linux daemon logs: `$XDG_STATE_HOME/sah/daemon.stdout.log` and `daemon.stderr.log`, or `~/.local/state/sah/`
- macOS LaunchAgent plist: `~/Library/LaunchAgents/ai.borca.sah.plist`
- Linux systemd user unit: `$XDG_CONFIG_HOME/systemd/user/ai.borca.sah.service` or `~/.config/systemd/user/ai.borca.sah.service`

## Daemon Mode

`sah daemon install` writes a per-user service definition, captures the current shell `PATH`, `HOME`, and the absolute paths of installed agent binaries, and starts it immediately. On macOS the service manager is `launchd`. On Linux it is `systemd --user`. Unless the user explicitly pins agents with `--agent`, `--agents`, or `--rotate-installed`, the install path detects every installed supported agent CLI and persists daemon round-robin mode automatically. If none are installed, the command fails before the service starts and tells the user to inspect `sah agents`.

The daemon runs `sah run --daemon` from the saved config directory, so the service behavior is driven by persisted config defaults instead of the shell's working directory.

The worker writes its normal daemon output through an internal rotating logger. `daemon.stdout.log` rotates at 20 MB and `daemon.stderr.log` rotates at 10 MB, each keeping up to 5 older files before pruning the oldest backup. On Linux, `journalctl --user -u ai.borca.sah.service` is still useful for service-manager events around start and stop.

On Linux, long-lived background execution across logout depends on `systemd --user` lingering being enabled for that account.
