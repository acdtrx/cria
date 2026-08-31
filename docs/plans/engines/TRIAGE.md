# Test triage — contract vs structure

The refactor's definition of "behavior", written before the first edit (STEP-1;
discipline ruled 2026-08-27, `docs/BACKLOG.md` Engines).

- **CONTRACT** — pins what the outside sees: CLI output and exit codes, TUI
  rendered frames, files written, HTTP requests actually made. **Never red at
  any step of this plan**, and never deleted or weakened.
- **STRUCTURE** — pins the current shape: backend-string predicates, seam
  signatures, fakes, unexported helper returns. May go red mid-phase, named
  here with the step that rewrites it against the Engine interface. A deletion
  is legitimate only paired, in that step's file, with its replacement.

Classification is by what a test **asserts**, not how it is set up: a test that
injects a fake and then asserts a record file, an error a user reads, or a
rendered frame is contract. Where a file mixes both, the structure functions are
named individually — everything else in that file is contract.

## Counts

| | files | notes |
|---|---|---|
| Contract throughout | 22 | never touched by this plan |
| Mixed (contract + named structure functions) | 35 | only the named functions may go red |
| Structure throughout | 5 | `cli/main_test.go`, `hubcache/names_test.go`, `procs/ps_test.go`, `procs/lsof_test.go`, `tools/version_test.go` |
| Fixture-only (no test functions) | 3 | `serve_test.go`, `hubcache/fixture_test.go`, `hubapi/fixture_test.go` |
| **Total** | **65** | 570 test functions |

Structure functions: **≈122** of 570 (21%). Of those, **≈35** pin a per-backend
seam this plan moves; the rest pin parsers, picks plumbing, call discipline and
render helpers the Engine extraction does not touch — they are structure, but
this plan has no business making them red.

## Seams ranked by structure-test weight

| # | Seam | Structure tests | Where | Cleared by |
|---|---|---|---|---|
| 1 | hub/cache presence semantics | 6 (+4 call-discipline) | `hubcache/presence_test.go` ×3, `hubapi/hubapi_test.go` ×3 | STEP-3 |
| 2 | TUI toggle / per-backend list filter | 8 | `tui/groups_test.go` ×5, `prefs_test.go` ×1, `styles_test.go` ×1, `tui_test.go` tail ×1 | STEP-3 |
| 3 | tool composition + detection | 8 | `tools/tools_test.go` ×7, `version_test.go` ×1 | STEP-3 |
| 4 | LoadsLazily | 4 | `serve/warm_test.go` tail, `cli/start_test.go` half, `tui/lifecycle_test.go` ×2 | STEP-2 |
| 5 | command composition | 3 | `serve/command_test.go`, `serve/start_test.go` half, `tui/serveview_test.go` | STEP-2 |
| 6 | publishesSlots (+ `/slots` URL) | 2 | `serve/validate_test.go` | STEP-2 |
| 7 | record validation (backend + quant rule) | 2 subcases | `serve/record_test.go` | STEP-2 |
| 8 | health endpoint | 1 | `serve/health_test.go` (`TestProbeURL`) | STEP-2 |
| 9 | schema fields (per-backend keys/examples) | 1 (+4 schema-shape) | `config/docs_test.go` | STEP-3 / STEP-5 |
| 10 | scaffold | 0 | — | already contract-covered |
| — | managed-server program list *(not in the inventory)* | 1 | `procs/ps_test.go` (`TestIsManagedServer`) | see Findings |

## Contract twins added in this step

Six, each closing behavior that **only** a structure test guarded. All are new
tests; nothing was deleted, weakened or changed in production code.

| Twin | File | Gap it closes | Proven by |
|---|---|---|---|
| `TestTheRecordFileHoldsTheArgvThatWasSpawned` | `internal/serve/command_test.go` | The composed argv reached no outside surface for mlx at all: `TestComposedCommand` (structure) held the only mlx argv in the suite, and every other check compared against `composedFor`, i.e. against the seam itself. The twin reads the argv back off the **record file on disk** for both backends. | Mutating the expected mlx argv fails both the spawn and the file assertion. |
| `TestTheCachedMarkReadsEachBackendsModel` | `internal/tui/serveview_test.go` | Presence semantics (llama = one quant out of a repo; mlx = the repo is the quantization) was guarded **only** by `presence_test.go`'s two internal calls. The twin renders the entry list's dots over a cache where a whole GGUF repo holds the *wrong* quant and an mlx repo holds no items — so either half of the dispatch flipping changes what a user sees. | Inverting the branch in `presence.go` reddens both halves; production file restored. |
| `TestDetailPaneCarriesAnMLXEntry` | `internal/tui/serveview_test.go` | Nothing anywhere rendered or composed an mlx command line — the TUI's only command assertions are llama, and `TestDetailCommandIsTheOneStartWouldRun` compares against `ComposedCommand` self-referentially. | New rendered frame; asserts `--model …` and the absence of `-hf`. |
| `TestTheWarmReachesTheDocumentedCompletionPath` | `internal/serve/warm_test.go` | `/v1/completions` as a literal existed only in `TestWarmURL` (structure); warm, prove and bench all assert via the `completionPath` constant, so the constant could change with the suite green. | New; asserts the literal path a real server receives. |
| `TestTheGenerationGateReachesTheDocumentedSlotPath` | `internal/serve/validate_test.go` | Same for `/slots`: the literal lived only in `TestSlotsURL`. | New; asserts the literal path a real server receives. |
| `TestEachBackendsExampleTeachesItsOwnModel` | `internal/config/docs_test.go` | `backendExample` was guarded only by `TestDocsExamplesShowEveryKeyOfTheirBackend`, which is self-referential (schema metadata vs the renderer): an mlx example silently falling back to the shared llama values would stay green. Written as a relational claim, spelling no values — the file's own doctrine. | New; fails if both examples name one repo. |

Health endpoints needed **no** twin: `TestProbingARealServer` already drives
`/health` and `/v1/models` as literals against a handler that 404s anything else.

## Findings

- **`internal/cli` is a contract baseline.** 11 of 13 files are pure contract and
  never mention a backend; the only structure there is picks/`Selection`
  plumbing and `BenchSpec` parsing. The whole package should stay green through
  every step.
- **`internal/tui` is contract-heavy but had no mlx rendering at all** — no mlx
  status line, no mlx bench row, no mlx command line until this step's twin. A
  remaining gap worth closing when STEP-3 touches the toggle: no rendered
  **status box** for an mlx server (`liveStatus` fixtures are all llama). Not a
  structure-guarded behavior, so no twin was added here.
- **The seam inventory is missing one item:** `procs/ps.go` hardcodes
  `serverPrograms = ["llama-server", "mlx_lm.server"]` (pinned by
  `TestIsManagedServer`). A third engine means editing `procs` unless the
  program name becomes engine-owned — but `procs` deliberately reports what the
  OS says and lets serve judge it, so this is a decision for STEP-3, not a
  drive-by.
- **Three predicates default to llama rather than to "unknown"** — `healthPath`,
  `LoadsLazily`, `publishesSlots` — and both presence dispatches are
  `if llama {…}; return mlx`. A router engine would silently inherit llama's
  health path and MLX's whole-repo presence with the suite green. Make the
  Engine dispatch total; no fallback.
- **Fakes that reach into production dispatch.** `tui_test.go`'s `fakeServers.Warm`
  and `cli_test.go`'s equivalent both call `serve.LoadsLazily` themselves, so the
  mlx/llama warm distinction is measured by production code inside the fake. When
  `LoadsLazily` becomes an Engine method these fakes must stop consulting real
  dispatch and record every call instead — otherwise the distinction stops being
  tested **silently**, not redly.
- **Tests that must not be touched despite reading as structure:**
  `procs/ps_test.go::TestIdentitySameProcess*` (a recycled pid must not
  impersonate a dead server — a safety invariant, orthogonal to engines) and
  `hubapi_test.go::TestATotalNamesTheBlobsItsFilesLandIn` (the blob-naming rule
  is contract-grade; the cache matches unfinished downloads against exactly
  those strings).

## Real ports, processes and network

Verified across all 65 files — **nothing binds a fixed port, and nothing touches
port 11434**, so the suite is safe to run while the machine serves.

- Real processes: `serve/detach_test.go` (re-execs the test binary, real `ps`,
  real signals to its own children; binds no port), `procs/system_test.go`
  (real `ps`/`lsof`/signals, one listener on `127.0.0.1:0`), `cli/edit_test.go`
  and `cli/new_test.go` (fork `/bin/sh` stand-in editors),
  `tools/tools_test.go::TestCheckReadsAVersionPrintedOnStderr`.
- Real HTTP: `httptest` on ephemeral loopback only — `serve` (health, warm,
  validate, bench), `hubapi`, `selfupdate`. No outbound network anywhere.
- One latent coupling: `procs/system_test.go::TestSystemFindsRealServers` runs
  `ps -A` and its result *includes the live llama-server*. It only looks up its
  own helper pids, so a serving machine does not fail it — but the assertion
  must never tighten to "exactly N".

---

## Per-package classification

### `internal/serve` (12 files) — the structure concentration

| File | Class | Structure functions and the seam each pins |
|---|---|---|
| `command_test.go` | mixed | `TestComposedCommand` — **command composition** |
| `health_test.go` | mixed | `TestProbeURL` — **health endpoint** + the bind-address rule |
| `warm_test.go` | mixed | `TestWarmURL` — completion endpoint URL; `TestALlamaServerIsNeverWarmed` tail — **LoadsLazily** |
| `validate_test.go` | mixed | `TestSlotsURL` — **publishesSlots** endpoint; `TestGeneratingNeverAsksAnMLXServer` tail — **publishesSlots**; `TestDisplaced*` ×5 — port attribution (backend-free, untouched) |
| `record_test.go` | mixed | `TestRecordsAreValidatedLoudly` subcases "a backend cria cannot launch" and "a quantization on a backend that takes none" — **record validation** |
| `start_test.go` | mixed | `TestAStartComposesAndRecordsItsPicks` half — **command composition**; `TestIdentityCapture*` ×3 — identity-capture retry loop |
| `status_test.go` | mixed | `TestPhaseMatrix` — `derivePhase`; `TestAnOrdinaryStartAsksTheHubNothing`, `TestSnapshotsWalkTheCacheOnce` half — **hub/cache** call discipline; `TestBoxTarget` n/a |
| `bench_test.go` | mixed | `TestBenchURL` — endpoint composition; `TestEveryPromptKeepsItsInstruction`, `TestBenchSpecDefaultsAndClamps` — prompt/spec internals |
| `serve_test.go` | fixture | `fakeHost`, `fakeSpawner`, `newManager`, `composedFor`, `llamaEntry`/`choicesEntry` — regenerated against the interface in STEP-2 |
| `stop_test.go` | contract | signals delivered, record files removed, refusal text |
| `detach_test.go` | contract | detachment mechanics; composed argv reaching a real process |
| `port_test.go` | contract | who a refusal names, the SIGKILL sent |

### `internal/cli` (12 files) + `main_test.go`

| File | Class | Structure functions |
|---|---|---|
| `start_test.go` | mixed | `TestStartCarriesTheEntrysDefaultPicks`, `TestStartTakesPicksFromTheCommandLine`, `TestStartLaunchesTheStoredPicks` — the `Selection` handed to `Start`; `TestStartWaitWarmsAnMLXServer` half — **LoadsLazily** |
| `validate_test.go` | mixed | `TestValidateReplaysTheHolderOwnCombination` — the `Selection` across Start/Restore |
| `bench_test.go` | mixed | `TestBenchFlags` — `BenchSpec` arg parsing |
| `main_test.go` | structure | `TestIdentified` — version-string formatting (engine-irrelevant) |
| `cli_test.go` | contract | routing, exit codes, stderr; also defines `fakeServers` (20 methods) — the package's blast radius |
| `help_test.go`, `list_test.go`, `status_test.go`, `stop_test.go`, `new_test.go`, `edit_test.go`, `update_test.go`, `wiredlimit_test.go` | contract | output text, exit codes, files written; `new_test.go` is the strongest scaffold net (byte-equality against `ExampleEntry(backend)`) |

### `internal/tui` (18 files)

| File | Class | Structure functions |
|---|---|---|
| `groups_test.go` | mixed | `TestSectionsLayOutTheEntryList`, `TestAGroupOfRefusedFilesHidesItsHeading`, `TestTheUngroupedHeadingNeedsSomethingUnderIt`, `TestEntryRowsAreTheSectionsConcatenated`, `TestWithoutGroupsTheRowsAreTheListAsItWas` — **TUI toggle** (the `entry.Backend == backend` filter); `TestAnUnreadTree*`, `TestPrune*` ×4 — prune internals (backend-free) |
| `prefs_test.go` | mixed | `TestBackendToggleAlternates` — **TUI toggle** (`prefs.other()`, two-valued) |
| `styles_test.go` | mixed | `TestStylesDrawFromThePalette` — enumerates exactly two `backendTone`s |
| `tui_test.go` | mixed | `TestBackendToggleReportsItselfInTheTitle` tail — `backendTone` comparison; `TestRefreshTickObservesAndRearms`, `TestFrameDrawsBeforeTheFirstResize` — tick/width internals. Also defines `fakeServers`, whose `Warm` calls `serve.LoadsLazily` |
| `serveview_test.go` | mixed | `TestDetailCommandIsTheOneStartWouldRun` — **command composition**; `TestCacheIsWalkedOnlyWhereItIsRead`, `TestTheToolCheckRunsOnce` — call discipline |
| `lifecycle_test.go` | mixed | `TestAnMLXStartLoadsTheWeightsItself`, `TestALlamaStartLoadsNothing` — **LoadsLazily**; `TestStartCarriesThe*Picks`, `TestRestart*` ×4, `TestServerKeysActOnTheStatusBox`, `TestStartOfARunningEntryConsultsNothing` — selection plumbing and gate order |
| `status_test.go` | mixed | `TestPhaseToneMapping`, `TestBoxTarget` — raw returns |
| `benchpane_test.go` | mixed | `TestATickWithTheBenchPaneUpDoesNotWalkTheCache` — `walksTheCache()` |
| `cacheview_test.go` | mixed | `TestCacheSelectionWalksTheUnits` — `selectedCacheRow()` |
| `surgery_test.go` | mixed | `TestDeletePlansTheSelectedUnit`, `TestConfirmedDeleteExecutesAgainstFreshServingState`, `TestDeleteRefreshesTheWalk` — plan dispatch and seam args |
| `pick_test.go` | mixed | `TestEnterRunsTheArmedKeyOnThePickedServer`, `TestPickedDismissClearsTheRecordItLandedOn` — seam call logs |
| `choicepick_test.go` | mixed | `TestThePickerWalksItsRowsAndWrapsItsOptions`, `TestAPickIsWhatTheNextStartLaunches` — cursor index, `Start`'s selection |
| `logview_test.go` | mixed | `TestLogFollowsTheTicker`, `TestTailLinesReadsWholeLinesOffTheEnd`, `TestTailOfAnEmptyLogIsEmpty` |
| `toolspane_test.go` | mixed | `TestToolsPaneChecksTheHostWhenItOpens` — check call count |
| `managegroups_test.go` | mixed | `TestACarriedGroupRidesTheCarryBand` — `headingCursor().held` |
| `keybar_test.go`, `naming_test.go`, `grouppick_test.go` | contract | rendered bar, notice-line input, group filing |

### `internal/config` (5 files)

| File | Class | Structure functions |
|---|---|---|
| `docs_test.go` | mixed | `TestDocsExamplesShowEveryKeyOfTheirBackend` — **schema fields**, per-backend; `TestDocsNamesEveryDefinedKey`, `TestDocsSettingsExampleShowsEveryTreeKey`, `TestDocsFollowsTheDefinitions` — schema-driven rendering (STEP-5) |
| `schema_test.go` | mixed | `TestEntryRulesAccept`, `TestSettingsAccept` — parsed struct shape (STEP-5 rewrites the args half); `TestSchemaDefinitionsCarryTheirDocs`, `TestKindNames` |
| `load_test.go` | mixed | `TestLoadRecordsEntryPath`, `TestTreeEntryFindsWhatTheTreeDeclares` |
| `resolve_test.go` | mixed | `TestDefaultSelectionPicksTheFirstOption`, `TestResolveUnderTheDefaultSelection`, `TestResolveComposes`, `TestResolveLeavesTheEntryUntouched` — `Launch` shape and args order (STEP-5) |
| `scaffold_test.go` | contract | every function — files written, create-only, AGENTS.md contents |

### `internal/hubcache` (5), `internal/hubapi` (4), `internal/procs` (3)

| File | Class | Structure functions |
|---|---|---|
| `hubcache/presence_test.go` | mixed | `TestPresenceOfALlamaEntry`, `TestPresenceOfAnMLXEntry`, `TestPresenceOfAnUncachedMLXEntry` — **hub/cache presence semantics** |
| `hubcache/names_test.go` | structure | 8 functions — quant naming (`quantLabel`, `MatchQuant`, shard series). Stays in hubcache; the llama engine calls it |
| `hubcache/walk_test.go`, `delete_test.go` | contract | real temp trees: bytes vs du, deletion outcomes, refusal types |
| `hubapi/hubapi_test.go` | mixed | `TestTotalOfALlamaEntry`, `TestTotalOfAnMLXEntry`, `TestATotalNamesTheBlobsItsFilesLandIn` — **hub/cache presence semantics** (Hub side) |
| `hubapi/tree_test.go` | mixed | `TestRelNextReadsTheLinkHeader`, `TestARelativeNextPageLinkResolvesAgainstTheHub` — Link-header parsing |
| `hubapi/token_test.go` | contract | where the credential is read from, in what order |
| `procs/ps_test.go` | structure | parsers ×4; `TestIsManagedServer` — the two-program list; `TestIdentitySameProcess*` ×3 — **do not touch** |
| `procs/lsof_test.go` | structure | `-F` parsers |
| `procs/system_test.go` | contract | real `ps`/`lsof`/signals |

### Single-file packages

| File | Class |
|---|---|
| `internal/format/format_test.go` | contract — literal rendered strings |
| `internal/selfupdate/selfupdate_test.go` | contract — binary replaced atomically, refusal text |
| `internal/tools/tools_test.go` | mixed — 7 structure (tool detection/composition, `Report`'s three named fields, the llama-only build gate); `TestCheckRefusesAnUnusableOverride` is contract |
| `internal/tools/version_test.go` | structure — `parseBuild` over both banner shapes |
| `internal/picks/picks_test.go` | mixed — `TestMergeLayers`, `TestPrune`, and the two "leaves its inputs alone" tests are structure; the rest pin `choices.json`'s exact bytes |

## Interface feedback for STEP-2 (found while reading)

1. **`LaunchTool` maps a backend onto a named field of `tools.Report`** and errors
   for anything else. The router engine has **no external tool** — the interface
   needs a first-class "this engine needs no tool" answer, not an error.
2. **Keep the Engine to paths, predicates and composition; leave the transports
   on `Manager`.** `probe`, `complete`, `slots`, `bench`, `spawn` are func-typed
   Manager fields that ~30 contract tests inject into. Moving them inside the
   Engine regenerates every fake for no gain.
3. **`ComposedCommand` is a per-backend head plus a shared tail** (`--host`,
   `--port`, `launch.Args`). Have the Engine return only the model reference
   args, or the tail duplicates per engine.
4. **Record validation runs on a raw file before any Manager exists**, so engine
   lookup must be package-level (`engineFor(backend)`), not a Manager field —
   otherwise a router record is refused when its own state file is read.
5. **Do not put completion or stream parsing on the Engine.**
   `TestProveTakesOneRealCompletionFromEitherBackend` and
   `TestBenchReadsTheMLXStreamShape` deliberately pin that one request shape and
   one reader serve both backends.
6. **Only the *path* is per-engine, never the address rule.** `serverURL` /
   `probeTarget` (wildcard → loopback) is shared by probe, warm and bench.
7. **`PhaseDownloading` assumes one model per server** — progress comes from the
   record's single repo/quant. A router fronting many entries either reports no
   download phase or needs a shape change beyond this extraction (phase 3).
8. **Import direction blocks a schema move.** `config` is the bottom of the
   graph; an engine package contributing schema keys cannot import `config`
   back. Keep per-engine schema metadata in `config` as a registry engines
   register into.
9. **`Docs()` is a positional format string with two hardcoded example
   sections** — a third engine cannot be added without making it iterate.
10. **`backendTone` defaults to amber for anything not MLX**, so a third engine
    renders as llama with the suite green — make it a registry lookup and drive
    `TestStylesDrawFromThePalette` off the engine list.
