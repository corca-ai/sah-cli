# Agent Guide

Read these first:

- [docs/metadoc.md](docs/metadoc.md)
- [docs/build.md](docs/build.md)
- [docs/architecture.md](docs/architecture.md)

Current architecture:

- `sah` is a Go CLI that authenticates against `https://sah.borca.ai`, stores the contributor API key locally, and polls `/s@h/*` endpoints.
- The CLI never hands the contributor API key to Codex CLI, Gemini CLI, or Claude Code. It fetches assignments itself, runs the local agent CLI in an empty working directory, captures stdout, and submits the parsed JSON payload itself.
- Foreground mode uses `sah run`. Daemon mode uses a per-user macOS `launchd` agent installed by `sah daemon install`.

Note: `CLAUDE.md` is a symlink to `AGENTS.md`.
