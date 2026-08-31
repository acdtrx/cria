# STEP 7 — the router engine: process lifecycle

Status: done (2026-08-31) — suite green; the hand-run smoke is deferred to
STEP-9's live session (the machine was not cleared for probe starts)

## Intent

The third Engine implementation's foundation: cria starts, observes and
stops one router process per host, composed from the router engine config
and the included entries' preset — models come in STEP-8; this step is the
supervisor itself.

## Files likely touched

- `internal/engine/router.go` (new) + tests.
- `internal/serve`: the router's record (a server record whose entry is the
  engine, not a model entry — shape to settle), preset composition into
  `~/.local/state/cria/` beside the argv-composed records, health/liveness
  over the router port.
- `docs/specs/SERVE.md` staged additions (landed in STEP-9 with the rest).

## Decisions made during planning

- **The preset file is runtime state**, composed at start into the state
  dir the way argv is composed today — the tree stays human-owned; the
  composed preset is regenerated every start, never edited.
- **State is organized per engine** (OVERVIEW ruling 2): a subfolder per
  engine under `~/.local/state/cria/` holds engine-scoped state — the
  router's folder gets the composed preset, its inclusion + router picks
  (STEP-8), and its records. The exact layout (and whether existing
  llama-engine state moves under its own folder now or stays put) is
  settled in this step's design and recorded here; feature-building mode
  applies if anything moves — loud refusal of the old location, manual fix
  named.
- **Engine config** (`engines/router.toml`): the router port, `models-max`,
  autoload, sleep, and the `[*]` block's keys. Bind rule follows the same
  host doctrine as entries.
- **One router per host** (matching cria's single-host shape); it serves on
  its own port beside process-engine entries — port collisions are the
  existing loud refusals.
- Probe facts encoded: children are upstream's concern; cria's liveness is
  the router pid + its port answering; child states come from `GET /models`
  (STEP-8), never from `ps` against children.
- llama-server capability check: the tools report gains "router mode
  present" (flag probe on the binary, same style as version checks) so a
  too-old build refuses with the honest message instead of a spawn failure.

## Acceptance criteria

- Component tests: preset composition (engine `[*]` + placeholder includes)
  byte-exact against fixtures; start/stop/record/liveness against fakes;
  the too-old-binary refusal.
- A hand-run smoke (dev Mac, spare port): `cria` starts the router from an
  engines/router.toml, health goes green, stop leaves nothing behind.
- Suite state recorded; expected reds named with clearing step.

## Outcome

### The ruling this step had to settle first: the router is an engine, not a backend

OVERVIEW ruling 2 says inclusion is router-scoped **state**, and that the same
entry can carry llama picks and router picks at once. An entry therefore cannot
declare `backend = "router"` — that key names the engine that serves *this* entry
alone, and a router-included entry is a llama entry as well. So:

- `config` declares two sets: **`Engines()`** — every engine the tree may
  configure (llama, mlx, router: engine files, records, display) — and
  **`Backends()`**, the narrower set an entry's `backend` key may name (llama,
  mlx). `engineFacts` carries `perEntry` for the narrowing and `modelFlag` for the
  flag cria composes that engine's models under (`-hf`, `--model`,
  `--models-preset`).
- `backend = "router"` is refused by the existing enum check, naming llama and mlx.
- The registry drift test becomes `TestTheEnginesAreExactlyTheOnesTheTreeKnows`
  (engines ↔ `config.Engines()`), plus a clause holding every declarable backend to
  being one of them.
- STEP-3's anticipation of `onlyBackends: {BackendLlama, BackendRouter}` on `quant`
  turned out unnecessary: a router-served model *is* a llama entry, quant and all.

Rejected: making the router a backend key — it would fork the model profiles the
plan exists to share ("the cut"), and it cannot express one entry serving under
two engines with different picks.

### The router engine config, as settled

`engines/router.toml`, four keys, all optional at load, refused in any other
engine's file:

| key | type | meaning |
|---|---|---|
| `port` | integer | the port the router serves on; **required to start** — there is no default |
| `host` | string | bind address; the entries' doctrine — its own, else `default_host`, else `0.0.0.0` |
| `args` | string[] | what every model it serves starts from → the preset's `[*]` block |
| `router_args` | string[] | the router process's own flags, verbatim after the composed ones |

- **Two arg lists, because the router has two command lines.** `args` keeps the
  meaning it has in every engine file (what every model this engine serves starts
  from), which under the router is upstream's `[*]` section — and upstream's model
  sections override it exactly as an entry overrides its engine. `router_args` is
  the supervisor's own line.
- **`models-max`, autoload and sleep are `router_args`, not schema fields**
  (settled 2026-08-31, deviating from this step's planning note). OVERVIEW ruling 3
  holds `host`/`port` as the only schema-composed fields; a field per router flag
  would make cria carry upstream's defaults (`--models-max` is 4 upstream) and
  spell a second dialect for flags llama-server already documents. Pass-through
  keeps cria's flag-agnosticism total.
- **No port fallback to `default_port`** (settled 2026-08-31): that is the port the
  entries share, so a router bound to it would collide with every one of them.
  `serve.RouterPort` is the single refusal, naming `engines/router.toml`.
- A missing `engines/router.toml` is not an error at load (STEP-5's rule stands);
  it means this host has no router, and only a start says so.

### The composed preset

`engine.RouterPreset(defaults)` → the file; `presetSection(args)` is the shared
derivation STEP-8 reuses per model section. A preset line is a flag group with its
dashes stripped — legal only because upstream canonicalizes its own aliases
(probe-verified 2026-08-31), so cria owns no key mapping:

```
[*]
ngl = 99
fa = on
jinja = true
```

- `-ngl 99` → `ngl = 99`; a bare flag → `key = true`; `--ctx-size=8192` →
  `ctx-size = 8192`; the value token is passed through unread and unquoted.
- **Three loud compose-time refusals**, each naming the flag: a flag written twice
  (in either spelling), a flag carrying more than one value, and tokens written
  before any flag. Also refused: a `--flag=` with nothing after the `=`.
- The file carries **no comments**: it is upstream's format read by upstream's
  parser, which fails startup loudly on a line it does not accept — a courtesy
  line for a human reader is a line that parser has to accept. `server.json` beside
  it says what the file is.
- An empty `args` still writes `[*]` alone. Whether upstream accepts a preset whose
  only section is `[*]` is **not verified** — the STEP-9 smoke is where it is
  proved (see Deferred, below).

### The state layout

```
~/.local/state/cria/
├── servers/<entry-id>.json          (unchanged)
├── logs/<entry-id>-<stamp>.log      (unchanged)
└── engines/router/
    ├── preset.ini                   composed at every start
    ├── server.json                  the router's record
    └── logs/router-<stamp>.log      newest three, same retention
```

- **Nothing existing moves** (settled 2026-08-31): an entry's server is still an
  entry's, and moving `servers/` under `engines/llama/` would be a rename with no
  question behind it and a manual cleanup for every host. Noted for STEP-9 only if
  the router view makes the asymmetry felt; nothing depends on it.
- The router's logs live under its own folder rather than beside the entries' so an
  entry named `router` can neither prune them nor be pruned by them.

### The record

One `serve.Record`, with one field added and one rule widened:

- `preset` (omitempty) — the composed preset this server serves from.
- **A record says what its server was started to serve, and there are two
  answers**: an entry's names a model (`repo`, `quant`); an engine's own names a
  `preset` and carries the engine's id in `entry_id` — the name the CLI takes
  (`cria router stop`). Exactly one of the two is set; both or neither is refused
  on read.
- Rejected: a second record type. The purpose is identical — what cria spawned, so
  a later invocation can find, judge and stop it — and a parallel type would
  duplicate liveness, the stop escalation and the JSON rules for a field.
- `Stop`/`end` now take the record file's path (an entry's, or the engine's), which
  is the only change that touched the entry lifecycle.

### Observation

`RouterSnapshot` reuses `derivePhase` with `cached: true`: the router has no model
to download, so `downloading` cannot apply — it is starting, running, unhealthy or
exited, read from its own `/health` and the pid's identity. `LoadsLazily()` is
false and the router is never warmed: a green router is routing, which is the whole
of what it does; a model's load happens on the request that needs it.

### The tools capability check

- `tools`' one exec seam widened from `versionRunner(path)` to
  `probeRunner(path, flag)` — every `check(...)` call site in the tests is
  byte-identical, only the fakes gained the flag argument.
- After a llama-server passes the hub-cache check, cria reads its `--help` for
  `--models-preset` (`tools.RouterFlag`, which `internal/engine` composes from) and
  records `Tool.Router`. **The binary's own help, not a build threshold**: the
  question is whether *this* binary takes the flag, and no build number for router
  mode was verifiable offline.
- `Report.RouterMode()` derives the router's verdict from that one finding: an
  unusable llama-server is the router's answer too (same program, same fix — its
  `Disables` now names "llama entries and the router"), and a usable build without
  the flag is `StatusOutdated` for the router alone, naming the flag and the
  upgrade. No fourth row in `Report.All()`: one program to install, one row to read.
- Cost: one extra ~40ms exec per invocation, and only for a binary cria may
  otherwise use.

### The CLI verb

**`cria router [start|stop|status]`** (settled 2026-08-31), a subcommand of its
own rather than an id `cria start` takes: the router is not an entry, an entry
could be named `router` without being it, and the verbs this grows next (which
models it holds, loading them) are about a process the whole tree shares. A bare
`cria router` reports — the verb that changes nothing is the one you get without
typing one. `status` exits 0 while a router is up, non-zero when there is none.

- Start refuses in the entry start's own order: no port → already running → tool
  gate → port holder (both refusals are the entries', naming a managed holder to
  stop or a foreign process with pid, argv and working directory).
- **No `--wait`** in this step: `await` observes through `Snapshot`, which would
  walk the cache for a record with no model. The wait belongs with the router view
  (STEP-9); `cria router status` answers meanwhile.

### The two surfaces STEP-3 promised would redden — and how they were satisfied

- **Hue**: `backendTones[router] = pink` (Catppuccin Mocha Pink `#f5c2e7`), added to
  the palette table and held to the AA floor like every other colour (13.7:1 on the
  terminal's ground, below ink's 14.5:1, so the hierarchy test stays true). Pink is
  neither engine's hue and neither alarm's — lighter and more violet than the
  maroon of the key bar and the red of a failure. Teal and mauve were unavailable:
  each already means exactly one thing (`docs/specs/TUI.md`).
- **Scaffold**: `cria new`'s flags now come from `config.Backends()`, so there is no
  `--router` to scaffold with — and `--router` is *recognised* to be refused with
  the reason ("the `router` engine serves ordinary entries … configure it in
  engines/router.toml"), never "unknown flag". `TestEveryEngineHasAScaffoldFlagOnTheHelpPage`
  → `TestEveryBackendHasAScaffoldFlagOnTheHelpPage`, paired with the new
  `TestScaffoldingAnEngineThatHasNoEntryFileSaysWhy`.

### Structure-test rewrites, each paired

| Deleted / rewritten | Replacement | Why |
|---|---|---|
| `engine::TestTheEnginesAreExactlyTheBackendsTheTreeMayDeclare` | `TestTheEnginesAreExactlyTheOnesTheTreeKnows` (+ its declarable-backend clause) | the registry split into engines and declarable backends |
| `engine::TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses` (one-way) | same name, now bidirectional, + `TestTheRouterServesFromThePresetTheTreeRefuses` | composing no per-entry model args and not being declarable are one fact |
| `cli::TestEveryEngineHasAScaffoldFlagOnTheHelpPage` | `TestEveryBackendHasAScaffoldFlagOnTheHelpPage` + `TestScaffoldingAnEngineThatHasNoEntryFileSaysWhy` | an engine with no entry file scaffolds nothing |
| `tui::TestTheBackendToggleWalksEveryEngine` | `TestTheBackendToggleWalksEveryBackend` | the toggle changes which entries the lists show; the router has no list of its own until STEP-9 |
| `config::TestEveryBackendNamesItsModelFlag` | `TestEveryEngineNamesItsModelFlag` + `TestAnEntryMayNotDeclareTheRouter` | every engine composes its models under a flag; only some may be declared |
| `config` docs/schema walks over `Backends()` | the same walks, over the registry each schema is read for, skipping keys an id does not take | engine files are read per engine now |

One contract test changed a literal: `cli::TestRouting`'s subcommand list gained
`router`. The surface grew a subcommand on purpose; the test asserts the list is
complete, and it still does.

### Mutation checks

- `presetSection` losing the written-twice refusal: reddens
  `engine::TestThePresetRefusesWhatItCannotWrite` and
  `serve::TestArgsThatCannotBecomeAPresetRefuseTheStart`.
- `Report.RouterMode` returning the llama finding unchanged: reddens
  `tools::TestTheRouterVerdictReadsTheBinarysOwnHelp`,
  `engine::TestTheGateRefusesALlamaServerWithoutRouterMode`,
  `serve::TestARouterlessLlamaServerRefusesTheStart`,
  `cli::TestRouterStartRefusesALlamaServerWithoutRouterMode`.
- The router marked `perEntry` (declarable by an entry): reddens 4 `cli`, 3
  `config` and `engine::TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses`.
- The router's record path pointing at `servers/router.json`: reddens
  `serve::TestTheRoutersStateLivesUnderItsEngine`.

Every production file was restored and the suite re-run green.

### Suite

`gofmt -l .` empty; `go vet ./...` clean; `go test -count=1 ./...`:

```
ok  cria 0.422s · cria/internal/cli 5.403s · cria/internal/config 1.037s
ok  cria/internal/engine 0.681s · cria/internal/format 2.009s
ok  cria/internal/hubapi 1.489s · cria/internal/hubcache 2.812s
ok  cria/internal/picks 2.956s · cria/internal/procs 2.481s
ok  cria/internal/selfupdate 1.500s · cria/internal/serve 5.241s
ok  cria/internal/tools 3.728s · cria/internal/tui 7.423s
```

No expected reds.

### Deferred: the hand-run smoke

The acceptance criteria's live smoke (start the router on a spare port, watch
health go green, stop it) is **deferred to STEP-9's live session** — the serving
machine was not cleared for probe starts during this step. What it must prove,
beyond "it comes up":

1. A preset whose only section is `[*]` is accepted by llama-server's parser (the
   one unverified assumption in the composition).
2. `/health` answers green with no model loaded, on a cria-composed launch — the
   probe proved it for a hand-written preset, not for this argv.
3. `cria router stop` leaves the port free and no record behind.

### Docs updated in this step

- `docs/specs/SERVE.md` — "The router" (one per host, engine-scoped state layout,
  the preset as regenerated state, liveness/phases, the tool gate) and the record's
  two shapes.
- `docs/specs/CONFIG.md` — the engine/backend split, the router's keys, its two arg
  lists, the port rule, key applicability across engine files.
- `docs/specs/TOOLS.md` — the router mode check and what an unusable llama-server
  disables.
- `docs/specs/CLI.md` — `cria router`, and `cria new`'s flags being per declarable
  backend.
- `docs/ARCHITECTURE.md` — the engine state subfolder, the engine row.
- `internal/config/agents.md` — engines/router.toml and its lifecycle verbs.

### What STEP-8 must know

1. **`presetSection` is the derivation to reuse** per model section; `RouterPreset`
   grows a second argument for them. Section names stay the model reference, with
   `alias = <entry-id>` (STEP-4's ruling).
2. **Inclusion has nowhere to live yet.** `engines/router/` is the folder for it;
   the record and the preset are already there, and inclusion + router picks join
   them as their own file.
3. **A router-included entry is an ordinary llama entry** — quant and all. Nothing
   in the schema needs widening for it.
4. **The router's args may not be composed per section blindly**: an entry's args
   go through the same three refusals, and an entry whose args cannot be one key
   per flag has to be reported against that entry rather than failing the whole
   preset silently.
5. `RouterSnapshot` reports the supervisor only. Child states come from
   `GET /models`, and the `?model=` stats transfer (probe-verified).
