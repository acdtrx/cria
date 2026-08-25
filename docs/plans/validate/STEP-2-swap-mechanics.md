# STEP 2 — swap mechanics: hold, restore, prove

Status: not started

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
