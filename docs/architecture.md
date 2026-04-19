# Architecture

## Purpose

`sah` is the SCIENCE@home CLI worker for macOS and Linux. It authenticates a contributor account, discovers server-advertised help and navigation, claims assignments, runs a local coding agent CLI in a restricted empty workspace, and submits the resulting JSON payload.

## Discovery and Help

`sah` treats the server as the primary source of CLI navigation.

- `sah` and `sah help` fetch `GET /s@h` for the service document when available.
- They also send local machine state to `POST /s@h/navigation` to get ordered next-action suggestions.
- Those discovery requests intentionally run through an unauthenticated client, so stale local bearer tokens do not block public server-driven help.
- The binary still keeps a small local kernel for commands that are inherently machine-local: `auth`, `run`, `daemon`, `agents`, and `version`.
- The current first-party read commands `me`, `contributions`, and `leaderboard` are still explicit CLI commands in the binary, but their help text and follow-up navigation are server-advertised when the service is reachable.
- If the server is unreachable, discovery falls back to that local kernel instead of failing hard.

## Auth Flow

1. `sah auth login` fetches OAuth authorization server metadata from `GET /.well-known/oauth-authorization-server`.
2. The CLI starts an OAuth 2.0 Device Authorization Grant with `POST /oauth/device_authorization`.
3. `sah-web` returns a verification URL, user code, device code, expiry, and polling interval.
4. The CLI prints the verification URL and user code instead of opening a browser automatically.
5. The user visits `/oauth/device`, signs in with Google if needed, and enters the code.
6. The CLI polls `POST /oauth/token` with the device code until the flow is approved, then stores the OAuth access token, refresh token, token type, and expiry in the local config file with user-only permissions.

The browser still never receives the CLI's stored bearer token. Legacy `/api/cli/device-authorizations`, `/api/cli/device-token`, `/cli/authorize`, and `/api/cli/exchange` routes remain server-side for older clients, but the current CLI now prefers the standard OAuth device flow and only falls back implicitly when an older stored API key is already present in local config.

## Worker Loop

- `sah` claims work with `POST /s@h/assignments` using `Authorization: Bearer` when an OAuth access token is available, or a stored legacy `X-API-Key` otherwise.
- The assignment response can include a protocol version plus `_links.self`, `_links.submit`, and `_links.release`, and the same submit/release relations can also be exposed through the HTTP `Link` header.
- During rollout, assignment responses may include both `agent_request` and `instructions`.
  `sah-cli` v0.9.x prefers the server-owned `agent_request` execution contract, while `<= v0.8.x` still renders the final prompt locally from `instructions`.
- It runs one of the supported local agent CLIs: `codex`, `gemini`, `claude`, or `qwen`.
- The agent receives no SCIENCE@home credential and runs in an empty temporary working directory.
- The CLI parses the agent stdout as JSON and follows the assignment-scoped submit link when present, falling back to the legacy contribution endpoint when it is not.
- If the local agent aborts, fails locally, or hits a submission error, `sah` releases the assignment immediately so the assignment does not stay pinned at the open-assignment limit until expiry.

This keeps the CLI forward-compatible with new task families. As long as the server still returns one assignment payload plus one submission schema, the CLI does not need to know new task-specific endpoints ahead of time.

## HTTP Behavior

The CLI is intentionally built on normal HTTP client semantics instead of bespoke per-endpoint transport code.

- Safe reads use a private disk-backed RFC 9111 cache under the config directory.
- OAuth authorization server metadata uses that same cache instead of a bespoke auth-side cache.
- The cache key varies on auth and representation headers such as `X-API-Key`, `Authorization`, `Accept`, and `X-SAH-CLI-Version`.
- Redirect following stays enabled, but the CLI preserves method and body for redirected non-GET form posts and only forwards `Authorization` or `X-API-Key` across trusted canonical redirects.
- Safe reads retry once when the server responds with `429` or `503` plus a short `Retry-After` value.
- Auth polling, assignment claim, submission, and release requests bypass caching through server `Cache-Control: no-store`.
- Expired OAuth access tokens are refreshed automatically with the stored refresh token before authenticated requests when possible.
- The old `client-release.json` TTL cache has been removed. Release metadata now rides on the same HTTP cache as other safe reads.

## Commands

- `sah`
- `sah help [command]`
- `sah auth login|logout|status`
- `sah run`
- `sah daemon install|start|stop|status|uninstall`
- `sah me`
- `sah contributions`
- `sah leaderboard`
- `sah agents`

The CLI treats `sah` itself as the discovery entrypoint. It derives a local journey state from auth, detected agent CLIs, and daemon status, then merges that with server-provided navigation so successful commands can suggest follow-up commands instead of ending at raw output only.

Release discovery and worker-contract negotiation are described in [updates.md](updates.md).

## Local Files

- macOS config: `~/Library/Application Support/sah/config.json`
- Linux config: `$XDG_CONFIG_HOME/sah/config.json` or `~/.config/sah/config.json`
- macOS HTTP cache: `~/Library/Application Support/sah/http-cache/`
- Linux HTTP cache: `$XDG_CONFIG_HOME/sah/http-cache/` or `~/.config/sah/http-cache/`
- macOS daemon logs: `~/Library/Logs/sah/daemon.stdout.log` and `~/Library/Logs/sah/daemon.stderr.log`
- Linux daemon logs: `$XDG_STATE_HOME/sah/daemon.stdout.log` and `daemon.stderr.log`, or `~/.local/state/sah/`
- macOS LaunchAgent plist: `~/Library/LaunchAgents/ai.borca.sah.plist`
- Linux systemd user unit: `$XDG_CONFIG_HOME/systemd/user/ai.borca.sah.service` or `~/.config/systemd/user/ai.borca.sah.service`

## Daemon Mode

`sah daemon install` writes a per-user service definition, captures the current shell `PATH`, `HOME`, and the absolute paths of installed agent binaries, and starts it immediately. On macOS the service manager is `launchd`. On Linux it is `systemd --user`. Unless the user explicitly pins agents with `--agent`, `--agents`, or `--rotate-installed`, the install path detects every installed supported agent CLI and persists daemon round-robin mode automatically. If none are installed, the command fails before the service starts and tells the user to inspect `sah agents`.

The daemon runs `sah run --daemon` from the saved config directory, so the service behavior is driven by persisted config defaults instead of the shell's working directory.

The worker writes its normal daemon output through an internal rotating logger. `daemon.stdout.log` rotates at 20 MB and `daemon.stderr.log` rotates at 10 MB, each keeping up to 5 older files before pruning the oldest backup. On Linux, `journalctl --user -u ai.borca.sah.service` is still useful for service-manager events around start and stop.

On Linux, long-lived background execution across logout depends on `systemd --user` lingering being enabled for that account.
