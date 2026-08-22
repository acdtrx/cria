# Step 3 — serve carries a selection

Status: done

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

## Result

- `Manager.Start(entry, selection, report)` resolves via `config.Resolve` first —
  before the tool gate (inside `ComposedCommand`) and the live-record check — so
  every frontend refuses a bad pick identically, with the valid names.
  `ComposedCommand(entry, launch, report)` composes from the resolved facts and
  stays choice-blind; `hubReference` reads the launch.
- `Record.Selection config.Selection` (`omitempty`), and `Repo`/`Quant` store
  the **resolved** values — the record says which combination runs, and
  `Record.entry()` needed no change: cache presence and Hub totals follow the
  picked model through the fields they already read. `picksOf` clones and
  normalises empty → nil so a flat entry's record carries no key.
- CLI: `a.start` split into parsing and `a.startEntry(tree, entry, selection,
  wait)` — the gate sequence with resolution up front, ahead of already-running
  (pure map reads exec nothing, so the "record read before gates that exec"
  reasoning holds); `a.start` passes `config.DefaultSelection`. The split is the
  seam step 5's `choice=option` parsing lands in, and the seam the ordering test
  drives a bad selection through. TUI: threading only, plus `m.picks(entry)` as
  the one place a frame answers "which picks does a start use" — the detail pane
  resolves through it before composing, so the shown line is the spawned line.
- Decided while implementing: the CLI resolves twice (up front for ordering,
  serve again for composition) — `Resolve` is pure, and moving the refusal out
  of serve would fork the message; resolution errors return unwrapped from
  `Start` (they already name the entry); `Record.validate` gets no selection
  rule — nothing in serve acts on the field, and replaying it through `Resolve`
  refuses loudly on its own.
- Suite fully green (`go test -count=1 ./...`), gofmt silent, vet clean —
  verified independently in review. Mid-phase step, no expected reds.
