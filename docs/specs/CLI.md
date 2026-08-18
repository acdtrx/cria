# CLI — subcommand surface

One binary, two faces: bare `cria` opens the TUI; subcommands drive the same
subsystems scriptably (`docs/cria.md`, principle 5 — agents validate what they
write). This spec is the index of the surface; each behavior's contract lives with
its subsystem.

## The whole v1 surface (settled 2026-08-18)

- `cria` — the TUI.
- `cria start <id> [--wait]` — `docs/specs/SERVE.md`.
- `cria stop [<id>]` — `docs/specs/SERVE.md`.
- `cria status [--json]` — `docs/specs/SERVE.md`.
- `cria docs` — prints the config schema and full examples; `docs/specs/CONFIG.md`.
- `cria wired-limit <MB>` (settled 2026-08-18, user-designed) — generates the
  launchd plist that pins `iogpu.wired_limit_mb` at boot (Apple-silicon Macs
  reset it, and big models need the headroom): the plist to **stdout** so a
  redirect yields a clean file, the install/verify/uninstall instructions to
  **stderr**, every sudo step the user's to run — cria generates, it never
  installs (principle 1). The value is validated against the machine's memory
  (loud refusal at ≥ RAM or off macOS); the file carries its own uninstall
  steps in a comment.

Nothing else: no cache operations, no scaffolding (`docs/BACKLOG.md`).

## Rules

- Exit codes mean what they say: `0` = the asked-for thing is true or done
  (started and healthy with `--wait`, stopped, at least one live server for
  `status`); non-zero otherwise. Errors name the failing thing and its fix —
  never a silent failure.
- `--json` exists on `status` only in v1; the other subcommands speak
  human-readable output.
