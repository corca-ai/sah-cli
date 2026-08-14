# CLI Updates

## Goal

Keep release discovery, worker compatibility, and CLI navigation separate while keeping the binary small.

## Discovery and Release Metadata

`sah` reads three server-owned documents:

- `GET /api/v3/client/service`
  Service document for the CLI entrypoint and server-advertised command affordances.
- `POST /api/v3/client/navigation`
  Ordered next-command suggestions based on the local machine state the CLI reports.
- `GET /api/v3/client/release`
  Release metadata and worker-contract requirements.

The CLI no longer ships a built-in `upgrade` command. Release metadata is still surfaced so the CLI can:

- tell the user when a newer release is recommended
- include release-notes links when worker compatibility fails
- keep the release policy separate from the local command surface

Read-only commands such as `sah me`, `sah contributions`, and `sah leaderboard` are not blocked by worker-contract policy.

## HTTP Cache

Safe reads share one private disk-backed HTTP cache:

- macOS: `~/Library/Application Support/sah/http-cache/`
- Linux: `$XDG_CONFIG_HOME/sah/http-cache/` or `~/.config/sah/http-cache/`

The cache honors server `Cache-Control` headers instead of keeping bespoke JSON TTL files. Native client metadata and safe account or recognition reads use the same transport and cache rules.

## Release Metadata

`sah` reads release metadata from `GET /api/v3/client/release`.

The response includes:

- `latest_version`
- `recommended_version`
- `upgrade_command`
- `task_protocol_version`
- `required_task_protocol_version`
- `required_client_capabilities`
- `_links.upgrade`
- `_links.release_notes`

## Worker Contract

The CLI advertises its worker contract only on routes that claim, read, submit, or release assignments:

- `POST /api/v3/work/assignments`
- `GET /api/v3/work/assignments/{assignment_uid}`
- `POST /api/v3/work/assignments/{assignment_uid}/submissions`
- `DELETE /api/v3/work/assignments/{assignment_uid}`

Already released older clients remain a server-side compatibility concern; this release does not call the legacy `/s@h/*` ingress.

The advertised headers are:

- `X-SAH-Task-Protocol`
- `X-SAH-Client-Capabilities`

Today the built-in CLI capabilities cover assignment affordances such as assignment-scoped submit and release links. If the server requires a newer task protocol or new client capabilities, `sah run`, `sah daemon install`, and `sah daemon start` stop before starting worker traffic. If the contract changes after the worker has already started, the worker routes return a conflict and the CLI exits with an upgrade hint instead of looping forever.

During the additive server-owned prompt rollout:

- `sah-cli` <= `v0.8.x` still consumes `instructions` and renders the prompt locally.
- `sah-cli` v0.9.x can advertise `server-agent-request` and consume `agent_request` directly.
- The API should not require `server-agent-request` until the old `instructions`-based clients are no longer part of the supported rollout window.
