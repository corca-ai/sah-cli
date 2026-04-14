# Agent Guide

Read these first:

- [docs/metadoc.md](docs/metadoc.md)
- [docs/build.md](docs/build.md)
- [docs/architecture.md](docs/architecture.md)
- [docs/updates.md](docs/updates.md)

Current architecture:

- `sah` is a Go CLI that authenticates against `https://sah.borca.ai`, stores the contributor API key locally, and polls `/s@h/*` endpoints.
- The CLI never hands the contributor API key to Codex CLI, Gemini CLI, Claude Code, or Qwen Code. It fetches assignments itself, runs the local agent CLI in an empty working directory, captures stdout, and submits the parsed JSON payload itself.
- Foreground mode uses `sah run`. Daemon mode uses a per-user background service installed by `sah daemon install`: `launchd` on macOS, `systemd --user` on Linux.

Note: `CLAUDE.md` is a symlink to `AGENTS.md`.
