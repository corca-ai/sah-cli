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
- The assignment response can include a protocol version plus `_links.submit` and `_links.release`, and the same submit/release relations can also be exposed through the HTTP `Link` header.
- It builds a task-only prompt from the returned assignment payload and instructions.
- It runs one of the supported local agent CLIs: `codex`, `gemini`, or `claude`.
- The agent receives no API key and runs in an empty temporary working directory.
- The CLI parses the agent stdout as JSON and follows the assignment-scoped submit link when present, falling back to the legacy contribution endpoint when it is not.
- If the local agent aborts or fails before submission, `sah` releases the assignment immediately so the API key does not stay pinned at the open-assignment limit until expiry.

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

- Config: `~/Library/Application Support/sah/config.json`
- Logs: `~/Library/Logs/sah/`
- LaunchAgent plist: `~/Library/LaunchAgents/ai.borca.sah.plist`

## Daemon Mode

`sah daemon install` writes a per-user `launchd` plist, captures the current shell `PATH`, `HOME`, and the absolute paths of installed agent binaries, bootstraps it, and starts it immediately. The daemon runs `sah run --daemon` from `~/Library/Application Support/sah`, so the service behavior is driven by the saved config defaults instead of the user's home directory.
