# STEP 3 — the seams outside serve; phase 1 closes

Status: not started

## Intent

The rest of the inventory moves behind the interface, the shims die, and
phase 1 ends green: the extraction provably changed no behavior.

## Files likely touched

- `internal/hubcache/presence.go`, `internal/hubapi/hubapi.go` — what
  "cached"/"complete" means per engine (llama: the quant's files within the
  repo; mlx: the whole repo) becomes an engine-answered question.
- `internal/cli/new.go` (scaffold choice), `internal/config/schema.go`
  (per-engine key applicability and examples — the hooks, not the schema
  change; that is STEP-5).
- `internal/tui/prefs.go` — the toggle iterates engines from one list
  instead of a hardcoded pair (display order stays a TUI decision).
- Every remaining structure test from TRIAGE.md rewritten or
  replacement-paired.

## Decisions made during planning

- Model-resolution semantics (hub presence) are engine knowledge — the
  probe reinforced it (router discovery is per-quant, mlx quants are their
  own repos). The hub packages ask the engine; they do not switch on the
  backend string.
- The phase-end shim check is mechanical and recorded here: no code whose
  only caller is an old-shape test or fixture; `grep -rn
  "BackendLlama\|BackendMLX" internal/ --include="*.go"` outside
  `internal/engine`, `internal/config` (enum + schema hooks) and display
  wiring returns nothing unexplained.
- Structure-test deletions paired in this file: each deleted test named
  beside its new-shape replacement.

## Acceptance criteria

- The inventory's ten seams all live behind the interface; the shim check
  passes; TRIAGE.md's structure list fully dispositioned (rewritten or
  paired-deleted).
- Contract tests green and unmodified across the whole phase.
- Phase 1 ends: full suite green (`go test -count=1 ./...` output recorded),
  gofmt/vet clean, committed.
