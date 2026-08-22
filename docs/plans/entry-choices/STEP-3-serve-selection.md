# Step 3 — serve carries a selection

Status: not started

## Intent

A start is (entry, selection): the manager resolves through `config.Resolve`,
composes the command from the resolved facts, records the picks in the state
record, and status carries them back out. Callers pass the default selection
for now — behavior is unchanged until the CLI and TUI steps.

## Files likely touched

- `internal/serve/start.go`, `command.go`, `record.go`, `status.go` + tests.
- `internal/cli/cli.go` (the `servers` interface), `start.go`, and the TUI's
  start call sites — signature threading only this step.

## Decisions (from planning)

- `Manager.Start(entry, selection, report)`; resolution happens inside serve so
  every frontend gets the same refusals. The composed command is built from the
  resolved repo/quant/args — `command.go` itself stays choice-blind.
- `Record.Selection map[string]string`, `omitempty`: a record without it is a
  flat entry's, read fine — records are transient, no migration
  (feature-building mode). Strict decode otherwise unchanged.
- `serve.Status` exposes the record's selection; display formatting (ordering
  picks by the entry's choice order where a tree is at hand, `choice=option`
  spelling) belongs to the frontends.
- Liveness, stop, logs, warm, bench: untouched — they act on records, and the
  record is already self-contained.

## Acceptance criteria

- Component tests: a start with a selection records it and composes the picked
  args (fake-backed, no process); a flat entry records no selection; an
  unresolvable selection refuses before any port/tool gate runs — wait: the
  spec orders resolution *with* entry validation, before tool and port gates
  (`docs/specs/SERVE.md`, Start 1) — test exactly that order.
- Existing start/stop/status tests still pass with the default selection
  threaded through.
- Suite green or expected reds named (mid-phase step).
