# Agent Guide

Read these first:

- [docs/metadoc.md](docs/metadoc.md)
- [docs/build.md](docs/build.md)
- [docs/testing.md](docs/testing.md)
- [docs/architecture.md](docs/architecture.md)
- [docs/updates.md](docs/updates.md)

Current architecture:

- `sah` is a Go CLI that authenticates against `https://sah.borca.ai`, prefers an OAuth 2.0 Device Authorization Grant token set stored locally, still accepts an already-stored legacy contributor API key for backward compatibility, discovers CLI navigation from `/s@h` plus `/s@h/navigation`, and uses `/s@h/*` assignment and account endpoints.
- The CLI never hands the contributor credential to Codex CLI, Gemini CLI, Claude Code, or Qwen Code. It fetches assignments itself, runs the local agent CLI in an empty working directory, captures stdout, and submits the parsed JSON payload itself.
- Foreground mode uses `sah run`. Daemon mode uses a per-user background service installed by `sah daemon install`: `launchd` on macOS, `systemd --user` on Linux.

Note: `CLAUDE.md` is a symlink to `AGENTS.md`.
