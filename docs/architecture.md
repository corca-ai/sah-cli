# Architecture

## Purpose

`sah` is the SCIENCE@home CLI worker for macOS. It authenticates a contributor account, polls for assignments, runs a local coding agent CLI in a restricted empty workspace, and submits the resulting JSON payload.

## Auth Flow

1. `sah auth login` opens a browser to `https://sah.borca.ai/cli/authorize`.
2. `sah-web` authenticates the browser session with Google if needed.
3. `sah-web` returns a short-lived signed auth code to the loopback callback on `127.0.0.1`.
4. `sah` exchanges that code for the contributor API key through `POST /api/cli/exchange`.
5. `sah` stores the API key in the local config file with user-only permissions.

The browser never receives the raw contributor API key. The local CLI proves possession with a verifier generated before the browser is opened.

## Worker Loop

- `sah` calls `GET /s@h/tasks` with `X-API-Key`.
- It builds a task-only prompt from the returned assignment payload and instructions.
- It runs one of the supported local agent CLIs: `codex`, `gemini`, or `claude`.
- The agent receives no API key and runs in an empty temporary working directory.
- The CLI parses the agent stdout as JSON and submits it through `POST /s@h/contributions`.

## Commands

- `sah auth login|logout|status`
- `sah run`
- `sah daemon install|start|stop|status|uninstall`
- `sah me`
- `sah contributions`
- `sah leaderboard`
- `sah agents`

## Local Files

- Config: `~/Library/Application Support/sah/config.json`
- Logs: `~/Library/Logs/sah/`
- LaunchAgent plist: `~/Library/LaunchAgents/ai.borca.sah.plist`

## Daemon Mode

`sah daemon install` writes a per-user `launchd` plist and bootstraps it. The daemon runs `sah run --daemon`, so the service behavior is driven by the saved config defaults.
