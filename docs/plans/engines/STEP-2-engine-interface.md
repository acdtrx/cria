# STEP 2 — internal/engine: the interface, llama and mlx

Status: done (2026-08-31)

## Intent

The Engine interface designed from the seams inventory and introduced with
its first two implementations; `internal/serve`'s engine knowledge delegates
to it. Behavior-preserving: contract tests untouched and green throughout.

## Files likely touched

- `internal/engine/engine.go` (new) — the interface; `llama.go`, `mlx.go`
  (new) — the implementations; `engine_test.go` and friends (new).
- `internal/serve/command.go`, `health.go`, `warm.go`, `validate.go`,
  `record.go`, `serve.go` — seams delegate.
- Structure tests in `internal/serve` rewritten per TRIAGE.md.

## Decisions made during planning

- **Home**: a new `internal/engine` package. It imports `config` and
  `tools` only; serve (and later hub/cli/tui) import it; nothing imports
  back into it — the graph stays acyclic.
- **Altitude** (probe-settled 2026-08-31): the interface speaks
  make-this-entry-serve / stop-serving / what-is-its-state — NOT
  "give me argv". For process engines (llama, mlx) serving means spawn and
  stopping means signal, exactly today's serve mechanics; the router engine
  (phase 3) implements the same verbs as API calls. Phase 1 must not bake a
  process assumption into the interface: the process-specific machinery
  (spawn, identity capture, pidfiles) stays in serve as the *process
  engine's* substrate, reached through the interface.
- **Surface, drawn from the inventory** (exact signatures are the
  implementer's, reviewed against this list): identity; tool selection from
  a `tools.Report`; launch composition for an entry+launch; health probe
  knowledge; lazy-load (warm) capability; slots/stats capability (the
  `publishesSlots` predicate generalized); record validation hooks (the
  quant-only-on-llama rule).
- Display concerns (TUI toggle order, `backendTone`) stay TUI-side, keyed
  by engine id — an engine module renders nothing.
- `config.Backend` remains the id type; the enum's members now name
  engines. No schema change in this phase.
- Mid-phase reds allowed for structure tests only, each named in this file
  with the step that clears it (STEP-3 at the latest).

## Acceptance criteria

- Every serve-side seam from the inventory reaches llama/mlx behavior only
  through the interface; `grep -rn "BackendLlama\|BackendMLX"
  internal/serve/` matches nothing outside the engine wiring.
- Contract tests green and unmodified (fixture rewiring only, where a fake
  implements a changed constructor).
- New fakes implement the Engine interface; awkwardness found while writing
  them is reported as interface feedback, not shimmed around.
- Suite result recorded here; expected reds named with their clearing step.

## Outcome

### The interface as settled

`internal/engine`, imported by serve, cli, tui and procs; it imports
`config` and `tools` only.

```go
type Engine interface {
	ID() config.Backend                                    // the value an entry's backend key carries
	Program() tools.Name                                   // the server program; empty for an engine that runs none
	Tool(report tools.Report) (tool tools.Tool, needed bool) // this engine's finding in a tool check
	ModelArgs(launch config.Launch) []string               // how this engine names a model on a command line
	TakesQuant() bool                                      // whether a model reference is qualified by a quantization
	HealthPath() string                                    // the documented endpoint a phase is read from
	LoadsLazily() bool                                     // green before the weights are read?
	SlotsPath() (path string, published bool)              // the per-slot signal, or that there is none
}

func For(backend config.Backend) (Engine, error)  // total: an unclaimed id is refused, never defaulted
func All() []Engine
func IDs() []string
func Programs() []tools.Name
func LaunchTool(engine Engine, report tools.Report) (tools.Tool, error) // the start gate
```

Two implementations, `llama{}` and `mlx{}`, holding what used to be
per-backend branches: the `-hf repo:quant` / `--model repo` composition,
`/health` vs `/v1/models`, `/slots` vs nothing, the lazy-load rule, and the
quant rule.

### How each STEP-1 feedback item landed

1. **Total dispatch.** `For` is the only lookup and it returns an error
   naming the ids cria has. `healthPath`, `LoadsLazily`, `publishesSlots`
   and their llama-shaped defaults are gone; nothing falls back.
2. **"Needs no tool" is first-class.** `Tool` answers `(tool, needed)` and
   `LaunchTool` opens on `needed=false` with the zero Tool and no error —
   tested in-package against a `programless` test engine, so phase 3's
   router does not have to invent an error path.
3. **Lookup is package-level**, not Manager-bound: `Record.validate` calls
   `engine.For` on a raw file before any Manager exists.
4. **Transports stayed on the Manager.** `probe`, `complete`, `slots`,
   `bench`, `spawn` are untouched Manager fields; no contract-test fake was
   regenerated for them.
5. **Completion/stream parsing stayed in serve.** `completionPath`,
   `completionRequest` and the bench reader are one shape for every engine.
6. **Only the path is per-engine.** `serverURL`/`probeTarget` (the
   wildcard→loopback rule) stayed in serve; `probeURL(engine, record)` and
   the gate's URL compose the engine's path into it.
7. **The eleventh seam (procs).** `procs/ps.go` no longer holds a program
   list: `isServerProgram` asks `engine.Programs()`. **Import-graph choice:**
   `procs → engine` directly, which does not cycle (engine imports only
   config+tools, and neither imports procs). The alternative — passing the
   list in through a seam serve wires — would have changed
   `procs.Host.Servers()`'s signature, and that method is called by a
   *contract* test (`procs/system_test.go::TestSystemFindsRealServers`),
   which must stay unmodified. procs still judges nothing: it asks which
   programs to look for and reports what `ps` said.
8. **The warm-fake trap is closed.** Both `fakeServers.Warm` fakes stopped
   consulting production dispatch; each now records every call it receives,
   so the mlx/llama distinction is measured on cria's own gate. Proven by
   mutation: flipping `mlx.LoadsLazily()` to false reddens
   `cli/start_test.go` ×3 and `tui/lifecycle_test.go` ×2, where before the
   change those tests would have stayed green.

### Deviations and judgement calls

- **The warm gate stays keyed on the record, not the entry.** Threading the
  entry's engine into `loadRefusal`/`noteLazyLoad` reddened three cli tests
  that set a record backend differing from the tree's entry; records are
  self-contained by design (docs/specs/SERVE.md), so both call sites resolve
  `engine.For(record.Backend)` themselves. `await`/`awaitGreen` therefore
  kept their signatures.
- **`ComposedCommand` and its refusal text are unchanged**, so
  `TestComposedCommand` (structure, seam #5) needed no rewrite at all. The
  refusal for an unknown backend now comes from `engine.For` and names every
  id cria has.
- **Record validation wording changed** for a quant on an engine that takes
  none ("*quant is "X", but a "mlx" server takes no quantization*") and for
  an unknown backend (`For`'s refusal). Both subcases are structure per
  TRIAGE and both still pass unmodified — they assert the key that is wrong,
  not the sentence.
- **Endpoint literals moved into the test fixture.** `mlxHealthPath` and
  `slotsPath` were unexported serve constants that seven *contract* test
  lines route and assert on. They are now `const` literals in
  `serve/serve_test.go` (the fixture file), so every contract test is
  byte-identical and now pins the documented path rather than the
  production constant — strictly stronger, and drift between an engine and
  the spec reddens them.
- **No unreachable branch was added** for composing a command line for an
  engine that runs no program. Phase 3 owes that: a router entry is not
  spawned per entry at all, so `ComposedCommand` will be asked a different
  question rather than handed an empty `tool.Path`.
- **Two structure tails were deleted, paired with their replacements**
  (below), rather than rewritten in place: they asserted predicates that no
  longer exist in serve.

### Structure tests: rewritten, replaced, unchanged

| Test | What happened |
|---|---|
| `serve/health_test.go::TestProbeURL` | rewritten against `probeURL(engine, record)` |
| `serve/validate_test.go::TestSlotsURL` | rewritten as `TestTheGateAsksWhereTheServerListens` — same table, now driving `Generating` through a capturing `slots` seam (the URL that reaches the wire) |
| `serve/warm_test.go::TestALlamaServerIsNeverWarmed` tail | `LoadsLazily(...)` assertion deleted; replaced by `engine/llama_test.go::TestALlamaServerIsReadyWhenItAnswers` and `engine/mlx_test.go::TestAnMLXServerLoadsOnItsFirstRequest`. The test's own behavior half (no request reaches a llama server) is untouched |
| `serve/validate_test.go::TestGeneratingNeverAsksAnMLXServer` tail | `publishesSlots(...)` assertion deleted; replaced by the same two engine tests (`SlotsPath`) |
| `serve/command_test.go::TestComposedCommand` | unchanged — the seam's signature did not move |
| `serve/record_test.go` validation subcases | unchanged — the wording changed, the assertions did not |
| `cli/start_test.go::TestStartWaitWarmsAnMLXServer`, `tui/lifecycle_test.go` ×2 | unchanged; they now measure production dispatch because their fakes stopped answering for it |
| `procs/ps_test.go::TestIsManagedServer` | unchanged — the list is the engines', and it is the same list |

New tests: `engine/engine_test.go` (registry totality, the refusal, every
engine answers every question, the gate's refusal and its no-program answer,
the scan's program list), `engine/llama_test.go`, `engine/mlx_test.go`.

### Mutation checks

- `mlx.LoadsLazily()` → false: reddens `serve` ×7, `cli` ×3, `tui` ×2.
- `llamaHealthPath` → `/v1/models`: reddens `serve/TestProbeURL`,
  `TestProbingARealServer`, `TestAServerIsStartingUntilItHasAnsweredGreenOnce`
  and `engine/TestALlamaServerIsReadyWhenItAnswers`.

Both production files were restored and the suite re-run green.

### Suite

`go test -count=1 ./...` green, all 13 packages; `gofmt -l .` empty;
`go vet ./...` clean. **No expected reds** — nothing is left for STEP-3 to
clear from this step. Seams 1, 2, 3, 9 and 10 of TRIAGE.md (hub/cache
presence, the TUI toggle and tone, tool composition, per-backend schema
fields, `Docs()`) are untouched and still STEP-3's.

```
ok  cria 0.401s · cria/internal/cli 4.358s · cria/internal/config 0.819s
ok  cria/internal/engine 1.867s · cria/internal/format 1.738s
ok  cria/internal/hubapi 1.876s · cria/internal/hubcache 1.288s
ok  cria/internal/picks 2.006s · cria/internal/procs 1.477s
ok  cria/internal/selfupdate 1.457s · cria/internal/serve 3.715s
ok  cria/internal/tools 2.644s · cria/internal/tui 6.287s
```

### Docs updated in this step

- `docs/ARCHITECTURE.md` — `internal/engine` in the module table and the
  graph, `procs → engine`, and the boundary rule (engines know, serve does).
- `docs/specs/SERVE.md` — per-engine knowledge is not the lifecycle's
  (settled 2026-08-31); the lazy-load rule's home corrected.

### What STEP-3 picks up

- The seams outside serve: hub/cache presence semantics
  (`hubcache/presence.go`, `hubapi/hubapi.go` — `TakesQuant` is the
  predicate they need), the TUI toggle and `backendTone` (drive both off
  `engine.All()`; `prefs.other()` is two-valued today), `cli/new.go`'s
  backend guess, and `config`'s per-backend schema metadata + `Docs()`'s two
  hardcoded example sections (TRIAGE feedback 8–10: keep the schema registry
  in `config`, since `engine` cannot import it back).
- `tools.Report`'s three named fields are still the reason `Engine.Tool`
  exists as a per-engine selector; if STEP-3 gives `tools` a lookup, that
  selector collapses.
- The remaining TUI gap STEP-1 named: no rendered status box for an mlx
  server.
