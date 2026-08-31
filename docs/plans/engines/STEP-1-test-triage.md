# STEP 1 — test triage: contract vs structure

Status: done (2026-08-31)

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

## Outcome

The classification is `TRIAGE.md` in this directory: 65 test files, 570 test
functions — 22 files contract throughout, 35 mixed (their structure functions
named individually), 5 structure throughout, 3 fixture-only. About 122 test
functions are structure; roughly 35 of those pin a per-backend seam this plan
moves. The rest pin parsers, picks plumbing and call discipline — structure,
but nothing this plan should redden.

Seams by structure-test weight: hub/cache presence (6) · TUI toggle and the
per-backend list filter (8) · tool composition and detection (8) · LoadsLazily
(4) · command composition (3) · publishesSlots (2) · record validation (2
subcases) · health endpoint (1) · per-backend schema fields (1) · scaffold (0 —
already contract-covered).

### Notable gaps found

- **The composed argv reached no outside surface for mlx.** `TestComposedCommand`
  held the suite's only mlx argv; everything else compared against `composedFor`,
  i.e. against the seam itself.
- **Presence semantics were guarded only by two internal calls**, and the
  llama/mlx difference is invisible at every boundary in `hubcache`/`hubapi`
  (the one exception — a llama entry with no quant makes zero HTTP requests —
  was already contract-covered).
- **The TUI rendered no mlx anything**: no mlx command line, no mlx status box.
- **Endpoint literals** `/v1/completions` and `/slots` existed only in the
  structure URL tests; every other check went through the constant.
- **`backendExample` was guarded self-referentially** — an mlx example falling
  back to the shared llama values would have stayed green.
- Two findings for later steps: `procs/ps.go`'s hardcoded `serverPrograms` is an
  eleventh seam the inventory missed (STEP-3 decides its home), and both
  `fakeServers.Warm` fakes call `serve.LoadsLazily` themselves, so that
  distinction would stop being tested *silently* if the fakes are not
  regenerated.

### Contract twins added (six, new tests only)

`serve/command_test.go::TestTheRecordFileHoldsTheArgvThatWasSpawned` ·
`tui/serveview_test.go::TestTheCachedMarkReadsEachBackendsModel` ·
`tui/serveview_test.go::TestDetailPaneCarriesAnMLXEntry` ·
`serve/warm_test.go::TestTheWarmReachesTheDocumentedCompletionPath` ·
`serve/validate_test.go::TestTheGenerationGateReachesTheDocumentedSlotPath` ·
`config/docs_test.go::TestEachBackendsExampleTeachesItsOwnModel`.

Two were mutation-checked rather than assumed: swapping the expected mlx argv
reddens the record-file twin, and inverting the branch in `hubcache/presence.go`
reddens both halves of the cached-mark twin (the production file was restored;
this step changed no production code).

Health endpoints needed no twin — `TestProbingARealServer` already drives
`/health` and `/v1/models` as literals against a handler that 404s anything else.

### Suite

`go test -count=1 ./...` green, all 12 packages; `gofmt -l .` empty; `go vet
./...` clean. No expected reds — this step adds tests and changes no behavior.

```
ok  cria 0.420s · cria/internal/cli 5.319s · cria/internal/config 0.906s
ok  cria/internal/format 1.320s · cria/internal/hubapi 1.881s
ok  cria/internal/hubcache 2.243s · cria/internal/picks 1.882s
ok  cria/internal/procs 1.284s · cria/internal/selfupdate 2.766s
ok  cria/internal/serve 5.290s · cria/internal/tools 3.520s
ok  cria/internal/tui 6.418s
```
