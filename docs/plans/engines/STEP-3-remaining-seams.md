# STEP 3 — the seams outside serve; phase 1 closes

Status: done (2026-08-31) — phase 1 closed, suite green

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

## Outcome

### What moved where

| Seam | Was | Is |
|---|---|---|
| Hub cache presence | `presence.go`: `if entry.Backend == BackendLlama` | `engine.For(entry.Backend)`, then `TakesQuant()`; the quant branch is `quantPresence` (was `llamaPresence`). A backend no engine claims answers the zero Presence |
| Hub totals | `hubapi.Total`: two `== BackendLlama` branches | one `engine.For`, then `TakesQuant()` twice; a backend no engine claims becomes `unknown(reason)` — the refusal itself is the reason the display shows |
| Scaffold choice | `parseNew`: two flag constants, `llama && mlx`, llama by default | `backendFlag(id) = "--" + id` over `engine.All()`; a bare invocation takes the first engine; `newUsage` is composed from the same list |
| TUI toggle | `prefs.other()`, two-valued | `prefs.next()` walks `engine.All()` and wraps; `defaultPrefs()` is the first engine; `decodePrefs` refuses through `engine.For` and lists `engine.IDs()` |
| Backend colour | `backendTone`: `if MLX {blue}; return amber` | `backendTones` table keyed by engine id; an engine with no hue is drawn as plain ink, never another engine's colour |
| Schema metadata | `example` + `backendExample` overlay (mlx overriding llama's value) | `example` (one value under every backend) **or** `examples` (one per backend, total). `checkBackend` reads the registry |
| `Docs()` | a positional format string with two hardcoded EXAMPLE sections | `entryExamples()` walks `config.Backends()`. The rendered page is byte-identical (diffed) |

`internal/config` cannot import `internal/engine` (engine imports config), so the
backend set lives in `config` as `backends`/`Backends()` and the engines are held
to it by `engine_test.go::TestTheEnginesAreExactlyTheBackendsTheTreeMayDeclare` —
a backend declared on one side only reddens the suite instead of documenting a
backend nothing serves. Both files carry the constraint as a comment; the
decision is recorded in `docs/ARCHITECTURE.md` and `docs/specs/CONFIG.md`.

### The shim check

`grep -rn "BackendLlama\|BackendMLX" internal/ --include="*.go"`, production
files — **21 lines, all inside the three allowed homes**:

| File | Lines | Justification |
|---|---|---|
| `config/config.go` | 3 | the enum itself (2) and the registry `backends` (1) — the allowed home |
| `config/schema.go` | 14 | per-backend schema metadata: 14 `examples` values across 12 lines, 2 `onlyBackend: BackendLlama` (`quant`, `choice.option.quant`) — the allowed home |
| `tui/styles.go` | 2 | `backendTones`, the display wiring: which hue each engine's name is drawn in |
| `engine/llama.go`, `engine/mlx.go` | 2 | each engine naming the id it claims |

Nothing in `serve`, `cli`, `hubcache`, `hubapi`, `procs`, `tools` or the rest of
`tui`. Test files hold 137 further lines across 27 files; they are tests *about* a
specific engine's behaviour (a llama entry's presence rule, an mlx server's
status line, the two examples `cria docs` prints), which is what a test of a
per-engine rule is supposed to name. The one class worth flagging for phase 3:
`config/docs_test.go`'s two contract tests (`TestDocsExamplesCarryTheAxisCommentedOut`,
`TestDocsExamplesLoadAsAConfigTree`) enumerate the pair by hand, so a third
backend is not automatically covered by them — they were left unmodified because
they are contract.

No production code is left whose only caller is a test: every symbol introduced
or renamed here (`Backends`, `entryExamples`, `backendFlag`/`backendFlags`/
`backendNamedBy`, `newUsage`, `quantPresence`, `prefs.next`, `backendTones`) is
called from production. The two `cria new` flag constants went the other way —
see the deviations.

### TRIAGE dispositions

| # | Seam | Disposition |
|---|---|---|
| 1 | hub/cache presence (6 + 4 call-discipline) | production moved to `TakesQuant`; all six tests **unchanged** — they assert per-backend outcomes, which is exactly what the engine now answers. Mutation-proved below |
| 2 | TUI toggle / list filter (8) | `groups_test` ×5 **unchanged** (the filter takes an engine id, never a pair); `prefs_test::TestBackendToggleAlternates` **rewritten** as `TestTheBackendToggleWalksEveryEngine`; `styles_test::TestStylesDrawFromThePalette`'s two `backendTone` literals **deleted, paired** with the new `TestEveryEngineIsSpelledInItsOwnTone`; `tui_test` tail **unchanged** |
| 3 | tool composition + detection (8) | **no change needed, and this is the disposition**: composition was cleared in STEP-2 by `Engine.Tool`/`LaunchTool`; what remains in `tools` is per-program detection of the three programs `docs/specs/TOOLS.md` names — nothing there selects on a backend. All 8 green and unmodified |
| 4–8 | LoadsLazily, command composition, publishesSlots, record validation, health | cleared in STEP-2 |
| 9 | schema fields (1 + 4 schema-shape) | `TestDocsExamplesShowEveryKeyOfTheirBackend` **rewritten** to walk `Backends()`; `TestSchemaDefinitionsCarryTheirDocs`'s example clause **rewritten** to `exampleFor` per backend; new `TestEveryKeyExamplesEveryBackend` holds the declaration rule (one shared example or a total per-backend table — never a fallback) |
| 10 | scaffold (0 structure tests) | production generalized; new `TestEveryEngineHasAScaffoldFlagOnTheHelpPage` ties the derived flags to the hand-written help page |
| — | procs program list | cleared in STEP-2 |

STEP-1's finding that "both presence dispatches are `if llama {…}; return mlx`"
is closed with the rest: no dispatch in the codebase falls through to an engine
now.

### The mlx gap STEP-1 named: closed

`tui/status_test.go::TestAnMLXServerFillsTheStatusBox` renders the status box for
an mlx server — its own backend word and a model reference that stops at the repo
(a quantization there is its own repo), alongside the pid, port, uptime and cost
columns. It was cheap: `liveStatus` needed only its record fields changed.

### Mutation checks

- `llama.TakesQuant()` → false: reddens `hubcache::TestPresenceOfALlamaEntry`,
  `hubapi` ×3, `engine` ×1, `serve` ×2.
- `mlx.TakesQuant()` → true: reddens `hubcache::TestPresenceOfAnMLXEntry`,
  `hubapi` ×5, `serve::TestRecordsAreValidatedLoudly`, and — the one that matters
  — `tui::TestTheCachedMarkReadsEachBackendsModel`, STEP-1's rendered-frame twin.
- `backendTones[mlx]` → amber: reddens `TestEveryEngineIsSpelledInItsOwnTone` and
  `tui_test::TestBackendToggleReportsItselfInTheTitle`.
- engine order reversed (`{mlx, llama}`): reddens `cli` ×6 (the bare `cria new`
  default) and `tui` ×12 (the first-launch backend) — the "first engine" wirings
  are measured, not incidental.
- `config.backends` minus mlx: reddens the drift test with "the \"mlx\" engine
  serves a backend no entry may declare", plus four `config` tests.

Every production file was restored and the suite re-run green.

### Deviations and judgement calls

- **`llamaFlag`/`mlxFlag` moved out of production into the cli test fixture**
  (`internal/cli/backendflags_test.go`). Once `parseNew` derives its flags from
  the engine ids, those constants had no production caller left — a shim by the
  step's own rule. Deleting them outright would have forced edits to two contract
  test files, so they went where STEP-2 put the endpoint literals: into the test
  side, as the documented spellings. Every contract test naming them is
  byte-identical and now pins `--llama`/`--mlx` rather than agreeing with
  whatever production spells.
- **The help page's FLAGS block stays hand-written prose.** Generating two of its
  lines would turn a readable const page into a format string for a phase-3
  benefit. Instead the new test holds the page to naming every engine's flag, so
  a third engine reddens the suite rather than shipping a flag nothing documents.
- **`onlyBackend` kept as it is.** It is already total (empty means every
  backend) and extensible; a key taken by more than one backend but not all needs
  a shape change that belongs with the ini-key cut, not here.
- **`hubcache.Presence` kept its signature.** A backend no engine claims answers
  the zero Presence — nothing known to be on disk — rather than growing an error
  path through two call sites (a `switch` case in `serveview.go`, a snapshot in
  `serve/status.go`) that a validated tree cannot reach. `hubapi.Total` had the
  natural home for the refusal and carries its text.
- **`Docs()` still restates engine knowledge in one prose line** ("cria composes
  the model, port and host flags itself: `-hf repo:quant` for llama, `--model
  repo` for mlx"). It duplicates `Engine.ModelArgs` in the package that cannot
  import engine. Left as prose; handed to STEP-5, which rewrites that page's
  contract anyway.

### Suite

`go test -count=1 ./...` green, all 13 packages; `gofmt -l .` empty; `go vet
./...` clean. No expected reds — phase 1 ends green and committed.

```
ok  cria 0.303s · cria/internal/cli 5.038s · cria/internal/config 0.935s
ok  cria/internal/engine 0.717s · cria/internal/format 1.795s
ok  cria/internal/hubapi 0.865s · cria/internal/hubcache 1.655s
ok  cria/internal/picks 1.403s · cria/internal/procs 2.265s
ok  cria/internal/selfupdate 1.141s · cria/internal/serve 5.222s
ok  cria/internal/tools 3.173s · cria/internal/tui 6.222s
```

`cria docs` was diffed byte-for-byte before and after the `Docs()` rewrite:
identical.

### Docs updated in this step

- `docs/ARCHITECTURE.md` — `hubcache`/`hubapi` → `engine` in the table and the
  graph; presence semantics named as engine knowledge; the config-side registry
  and the display-side mirror recorded as a boundary rule (settled 2026-08-31).
- `docs/specs/CONFIG.md` — the backend set and its per-key metadata live in the
  schema; a key states one example or one per backend, never a fallback.
- `docs/specs/TUI.md` — the toggle walks the engines and wraps; display order and
  colour are the TUI's, keyed by engine id.

### What phase 2 picks up

- **STEP-5's registry is `config.Backends()`** plus the two per-key fields
  (`onlyBackend`, `examples`). Adding a backend there without an engine — or an
  engine without a backend — reddens `engine`'s drift test, which is the intended
  way to feel the import direction rather than discover it.
- A router entry takes a quant, so `TakesQuant()` is true for it and the hub
  packages need nothing further; `onlyBackend: BackendLlama` on the two `quant`
  keys is the one place that will refuse a router entry's quant, and it is the
  key applicability STEP-5 reshapes.
- The `Docs()` prose line naming `-hf`/`--model` is the last engine knowledge
  spelled inside `config`.
- **Phase 3 gets three things for free and owes one**: `cria new --router` parses
  by existing, `cria docs` renders its example section by existing, and the
  process scan already looks for its program; it owes the router a hue in
  `backendTones` and a line in the help page's FLAGS block — both held by tests
  that redden the moment the engine is registered.
