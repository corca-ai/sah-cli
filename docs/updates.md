# CLI Updates

## Goal

Keep `sah` upgrades easy to discover without coupling worker compatibility to the binary version string.

## User Flow

1. Run `sah` or `sah help`.
2. The CLI inspects local auth, detected agent CLIs, daemon state, and cached release metadata.
3. If a newer CLI release is available, it suggests `sah upgrade`.
4. Before worker-only commands start live task traffic, the CLI checks whether it still satisfies the server's worker contract. If not, it stops early and points the user to `sah upgrade`.

Read-only commands such as `sah me`, `sah contributions`, and `sah leaderboard` are not blocked by release policy.

## Commands

- `sah upgrade`

For Homebrew installs, `sah upgrade` runs:

```sh
brew upgrade corca-ai/tap/sah-cli
```

For other install methods, the CLI explains that automatic upgrade is not supported yet and points the user to the recommended manual command or release notes.

## Release Metadata

`sah` reads release metadata from `GET /s@h/client-release`.

The response includes:

- `latest_version`
- `recommended_version`
- `upgrade_command`
- `task_protocol_version`
- `required_task_protocol_version`
- `required_client_capabilities`
- `_links.upgrade`
- `_links.release_notes`

The CLI caches this response locally at:

- macOS: `~/Library/Application Support/sah/client-release.json`
- Linux: `$XDG_CONFIG_HOME/sah/client-release.json` or `~/.config/sah/client-release.json`

The cache TTL is 24 hours. When the cache is stale, `sah` refreshes it opportunistically with a short timeout and falls back to cached data when refresh fails.

## Worker Contract

The CLI advertises its worker contract only on routes that claim, submit, or release assignments:

- `GET /s@h/tasks`
- `POST /s@h/contributions`
- `POST /s@h/assignments/{id}/submission`
- `POST /s@h/assignments/{id}/release`

The advertised headers are:

- `X-SAH-Task-Protocol`
- `X-SAH-Client-Capabilities`

Today the built-in CLI capabilities cover assignment affordances such as assignment-scoped submit and release links. If the server requires a newer task protocol or new client capabilities, `sah run`, `sah daemon install`, and `sah daemon start` stop before starting worker traffic. If the contract changes after the worker has already started, the worker routes return a conflict and the CLI exits with an upgrade hint instead of looping forever.
