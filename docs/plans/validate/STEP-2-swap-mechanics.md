# STEP 2 — swap mechanics: hold, restore, prove

Status: done (2026-08-25) — phase 1 ends here

## Intent

The three mechanisms the protocol composes, each independently testable in
`internal/serve`:

1. **Stop keeping the record**: read the holder's record into memory, then
   stop it (ordinary `Stop` — its on-disk record removal is fine, the held
   copy is the restore source).
2. **Restore from a held record**: resolve the record against the config tree
   — its entry, its picks — and start it again. This is the replay contract
   the TUI already implements (`tui/lifecycle.go, replayOf`); the resolution
   moves to a shared home (serve or config) so the TUI and validate use one
   implementation, per CODING-RULES §2.
3. **Prove**: one small real completion against a running record — the warm's
   sender with the record's own model reference — for **any** backend. This is
   fit-proofing, not lazy-load warming; `Warm`'s mlx-only gate stays where it
   is, the request sender is what's shared.

## Files likely touched

- `internal/serve/validate.go`, `validate_test.go`
- `internal/serve/warm.go` — expose the sender for the prove path.
- `internal/tui/lifecycle.go` — `replayOf` delegates to the shared resolution.
- Possibly `internal/serve/record.go`.

## Decisions made during planning

- Restore failure modes surface as errors naming what is serving now (the
  caller composes exit 3): the record's entry no longer in the tree, its
  picks no longer valid, or the restart itself failing.
- The prove request's budget is the warm's generous one; a refusal body or
  transport error is the fit-failure evidence, quoted concisely (the warm
  already extracts a refusal reason — reuse).
- Prove success requires an answer, not a green `/health` — the whole point.

## Acceptance criteria

- Unit-tested with fakes: hold-then-stop leaves a usable held record; restore
  starts the held record's entry with the record's picks (not current stored
  picks); restore errors are loud and specific for deleted entry / stale
  picks / failed start; prove passes on an answering fake, fails with the
  refusal quoted on a refusing one, fails on a dead port.
- TUI replay behavior unchanged (existing lifecycle tests stay green against
  the shared resolution).
- Phase 1 ends here: full suite green, committed.

## What was built

Three mechanisms in `internal/serve/validate.go`, plus the shared resolution
they and the TUI now use:

- `Displace(holder Record) (Record, error)` — the ordinary `Stop`, with the
  record taken as a value first and its picks cloned out of the caller's map
  (`picksOf`): the on-disk record goes with the server, so the held copy is the
  only restore source and nothing anyone writes into that map afterwards can
  change what goes back.
- `Restore(tree *config.Tree, held Record, report tools.Report) (Record, error)`
  — `Replay` then `Start`, every failure wrapped as `cannot put <id> back on
  port <n>: …` over the three specific reasons (entry gone from the tree, picks
  that no longer resolve, a start the host refused).
- `Prove(record Record) error` — one minimal completion at the record's own
  model reference, for **every** backend: fit-proofing, not lazy-load warming.
  Success is an answer; a refusal is reported with the server's own words
  quoted (`refusal`), a dead port with the transport failure.
- `serve.Replay(tree, record) (config.Entry, config.Selection, error)` in
  `record.go` — the replay contract, now in one place: the entry the tree
  declares today under the record's name, and the record's own picks, refusing
  with `Resolve`'s message when the combination no longer exists.
  `tui.replayOf` is a one-line delegation to it (CODING-RULES §2).
- `config.Tree.Entry(id)` — the by-id lookup `Replay` needs, nil-safe so a
  frame drawn before the first load answers "no such entry" rather than
  panicking; `tui.entryNamed` delegates to it. `cli.entryNamed` still holds its
  own copy — step 3 is in that file and can delegate there.

Decisions made while implementing:

- The completion the warm sends is now shared by name as well as by code: the
  request family in `warm.go` is `completionPath` / `completionPrompt` /
  `completionTokens` / `completionBudget` / `completionRequest` / `completer` /
  `newHTTPCompletion` / `completionURL`, and the Manager seam is `complete`.
  Two purposes send this request — warming a lazy backend and proving any
  server — so naming it after one of them would have made `Prove` read as a
  warm (CODING-RULES §1). `Warm` keeps the backend gate and the health wait;
  what is shared is the request, never the gate.
- `Replay`'s refusal for a vanished entry is `<id> is not an entry cria can
  read any more`; the TUI's alert loses the old `; nothing to restart` tail on
  that one path (nothing tested it), while `restartShownEntry`'s last-started
  path still says it.

## Suite

`go test -count=1 ./...` from the worktree root: **all packages ok**, no
expected reds. `gofmt -l .` empty, `go vet ./...` clean. Eight new tests in
`internal/serve/validate_test.go` (stop-keeping-the-record, and the held picks
proof against the caller's map · restore replaying the record's combination
rather than the entry's defaults · the two tree refusals · a restart the host
refused · the proof on both backends, its refusal quoted, and a port with
nothing behind it) and one in `internal/config/load_test.go` (`Tree.Entry`,
including the unread tree). Both subtle assertions were mutation-checked —
replacing the record's picks with the entry defaults and dropping the pick
clone each fail their test.
