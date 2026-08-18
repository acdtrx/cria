# Step 10 — CLI lifecycle commands

**Phase 3 · Status: not started**

## Intent

`internal/cli`: real `start` (validation, tool gate, port check with holder
attribution, spawn, `--wait`), `stop` (no-arg single-server form), `status`
(human + `--json`, exit codes) — the whole CLI.md surface over the phase-3
layers.

## Files likely touched

`internal/cli/`, `main.go`.

## Decisions made during planning

- Port check order: live-record holder → "stop <entry> first"; else lsof
  attribution → foreign holder details; else spawn (SERVE.md). The CLI only
  reports foreign holders — kill is a TUI offer.
- `--wait` polls the phase snapshot until `running` (exit 0) or
  `exited`/`unhealthy` (exit 1, log path printed); interval and timeout named
  constants.
- `status --json`: one document, fields mirroring the snapshot struct; humans
  and machines read the same facts (CLI.md).
- Errors name the failing thing and its fix — the strings are part of review.

## Acceptance criteria

- Component tests for argument handling, exit codes, and the port-refusal
  branches (fake procs/serve).
- **Phase 3 ends here — the first real e2e**: on the dev Mac, with a small real
  model: `start --wait` → green, `status --json` sane, second entry on the same
  port refused with the right message, `stop`, foreign drill (hand-started
  llama-server detected and attributed). Transcript recorded here; suite green,
  committed.
