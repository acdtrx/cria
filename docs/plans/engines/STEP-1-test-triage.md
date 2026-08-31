# STEP 1 — test triage: contract vs structure

Status: not started

## Intent

The refactor's definition of "behavior," written down before the first edit
(the discipline's step zero, backlog Engines entry, 2026-08-27): every test
in the suite classified **contract** (pins what the outside sees — CLI
output and exit codes, TUI frames, files written, HTTP requests made; never
red at any step of this plan) or **structure** (pins the current shape —
seams, fakes, signatures; may go red mid-phase, named, and is rewritten
against the Engine interface, never preserved through shims).

## Files likely touched

- `docs/plans/engines/TRIAGE.md` (new) — the classification, one line per
  test file (or per test where a file mixes both), with the structure tests
  expected to move and the interface seam each one pins.
- Possibly new tests only: where the triage exposes behavior guarded solely
  by a structure test, a contract twin is added *before* the refactor
  touches that seam.

## Decisions made during planning

- Granularity: per test file where uniform, per test function where mixed —
  the cli and tui suites are expected to be contract-heavy; `internal/serve`
  holds most of the structure tests.
- A test that asserts styled/rendered output (TUI frames, help text,
  `cria docs`) is contract even though it lives near internals: it pins what
  a user sees.
- No production code changes in this step. Adding contract twins is the only
  test-writing allowed; nothing is deleted or weakened here.

## Acceptance criteria

- TRIAGE.md covers every `*_test.go` file in the module; each structure
  entry names the seam it pins (from the OVERVIEW's inventory).
- Any behavior found guarded only by structure tests has a contract twin
  added, and the suite is green.
- Committed before STEP-2 begins.
