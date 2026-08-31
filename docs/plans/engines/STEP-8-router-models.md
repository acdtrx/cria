# STEP 8 — models under the router

Status: done (2026-08-31) — suite green; the live items are on STEP-9's list

## Intent

The router serves the *same* llama entries as its source: which entries are
included — each with its own combination of picks, independent of the llama
engine's — is router-scoped state; the preset composes one section per
included entry; load/unload and child status flow through the documented
API.

## Files likely touched

- `internal/engine/router.go`, `internal/picks` (router-scoped store beside
  entry picks — same doctrine, one level up), `internal/serve` status.
- `internal/cli`: surfaces for include/exclude and load/unload verbs
  (spelling settled here against existing CLI idioms).

## Decisions made during planning

- **Inclusion is state, edited like picks** (user ruling 2026-08-23,
  reconfirmed and refined at plan review 2026-08-31): config declares what
  can vary, state holds what is chosen. One combo per included entry, held
  in the router's per-engine state subfolder (STEP-7's layout) — router
  picks for an entry may differ from the llama engine's picks for the same
  entry, by design.
- **Section names / client naming** per STEP-4's alias ruling: entry ids
  become the `model` field if the alias lever holds; otherwise the recorded
  fallback mapping.
- Child phases derive from `GET /models` statuses
  (unloaded/loading/loaded/sleeping/failed) — cria maps them onto its phase
  vocabulary without inventing signals; stats and busy checks reuse the
  llama collector via `?model=` (probe-verified).
- Only llama-backend entries are includable (mlx has no router); the
  refusal names why.
- Autoload stays on (upstream default): pi swaps models by naming them —
  the probe's eviction behavior is the feature. Explicit
  `POST /models/load|unload` are exposed as deliberate verbs anyway
  (mechanism decoupled from trigger).

## Acceptance criteria

- Component tests: preset sections from included entries + their combos
  (collision rule across engine/entry/option levels holds in preset
  spelling too); inclusion state round-trips; status mapping from a fake
  `GET /models`; the mlx-entry refusal.
- Live smoke: two small entries included, both reachable through the router
  by their client names; a pick change for one regenerates the preset on
  next router start and the section diff shows exactly that change.
- Suite state recorded.

## Outcome

### The store: `engines/router/models.json`

`internal/picks` gained a second store — same doctrine (state holds what is
chosen), one level up (which entries, not which option of one entry):

```json
{
  "models": {
    "gemma": {},
    "qwen": { "context": "long", "quant": "q6" }
  }
}
```

- **The key is the inclusion**, the value is the combination. An entry held under
  its config defaults is `{}` rather than absent — which is exactly where this
  store parts from `choices.json`, where an entry picking nothing and an absent
  entry say the same thing.
- **A store cria cannot read refuses, it does not degrade** (settled 2026-08-31).
  `picks.Load` always answers with usable picks because the config defaults stand
  in for a stale pick; nothing stands in for "which entries are included", and an
  empty answer would start a router serving nothing and look like a successful
  start. So `picks.LoadRouter` returns the error, and `cria router start` refuses
  with the path and the one-line fix (fix the file, or delete it to start from an
  empty router). A missing file is still a fresh router, not an error.
- **Inclusion is never auto-pruned.** A stored *pick* naming an option the entry
  no longer has falls back to the config default (`picks.Merge`, unchanged), but
  an included id whose entry is gone stays in the store and is reported per model
  — a rename or a typo must not silently empty the router. `cria router exclude`
  is the way out, and it never reads the tree for that reason.
- Strict on read: unknown top-level keys, a missing/`null` models object, an entry
  held with `null` picks, empty ids, unnamed choices and empty options all refuse
  by name (`TestARouterStoreThatCannotBeReadIsRefused`).
- Path: `serve.RouterStateDir(root)` is the one spelling of the layout; the store
  takes a directory, like every other path under the state tree.

### Composition: one section per included entry

`engine.RouterPreset(defaults, models)` now returns an `engine.Composition`:

```go
type Composition struct {
	Preset  string    // the file's whole text
	Served  []string  // ids that got a section, in the order written
	Skipped []Skipped // ids that got none, each with its reason
}
```

```ini
[*]
ngl = 99
fa = on

[unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL]
alias = qwen-choices
ctx-size = 16384
n-cpu-moe = 12
```

- **The section carries the entry's args merged with the picked options', and no
  engine level** (settled 2026-08-31). `config.ResolveUnder(entry, selection,
  engineArgs)` is the new seam: `Resolve` is it under the entry's own engine file,
  and the router passes `nil` — its engine level is the `[*]` block, which
  upstream applies under every section by its own documented precedence
  (command line > model section > `[*]`). Merging `[*]` into each section would
  be cria doing upstream's job twice and would make every pick change a whole-file
  diff.
- **`alias = <entry-id>` is cria's line** (STEP-4's ruling): the section name is
  the model reference, whose quant tag upstream normalizes, and the alias is
  passed through as written. A section is written by the same `presetSection`
  derivation the defaults use, so the three refusals are one rule in one place.
- **Per-entry refusals are verdicts, never fatal** (settled 2026-08-31): one
  entry whose args cannot be written as preset keys would otherwise take the whole
  router down. The verdict list is the composition result, and every surface
  prints it — a silently dropped model is a client's 404 hours later. The engine
  file's own args are the one thing that still refuses the whole composition:
  that is the router's configuration, not one of its models.
- **A fourth per-entry refusal, new here:** an entry whose args set `--alias`
  (`alias` as a preset key) is skipped, because cria writes that key itself and
  two would hand upstream two answers. Only the key cria writes is refused —
  cria owns no table of upstream's short spellings, so `-a` stays upstream's to
  canonicalize (the same doctrine that lets a preset line be a flag with its
  dashes stripped).
- **Section collisions**: two included entries resolving to the same
  `repo:quant` are one section written twice, so the later is skipped naming
  both entries and the reference. Which of the two to change is the operator's
  call, and the reason gives them both names.

### The verdicts, as `serve` hands them over

```go
type RouterModels struct {
	Preset  string           // exactly what a start writes
	Served  []ServedModel    // id, repo, quant, the combination it is held under
	Skipped []engine.Skipped // ordered by entry id
}
```

`Manager.RouterModels(tree)` is pure — no writes, no requests — so it answers the
same whether or not a router runs: what the next start would serve. `StartRouter`
now takes the tree and returns `(Record, RouterModels, error)`, so the caller
reports exactly what was written rather than composing a second time and hoping.

Four verdicts come from `serve` rather than from the composition: an id the tree
no longer declares, an id whose file no longer loads (with the offending key), an
entry served by another program, and picks that no longer resolve. They are
merged with the composition's own and sorted by id, so one list is the whole
answer.

**Only llama entries are includable**, and the rule is derived rather than
declared: `engine.RouterServes(backend)` refuses any backend whose engine runs a
different program from the router's, naming both programs
(`mlx entries are served by mlx_lm.server, and the router is llama-server in
router mode; only llama entries can be included in it`). The router itself is
refused too — no entry declares it.

### Child states: upstream's word, and a partial map onto cria's phases

`GET /models` publishes `data[].id`, `data[].aliases[]` and
`data[].status.value`; the states are `downloading`, `downloaded`, `unloaded`,
`loading`, `loaded`, `sleeping` (llama.cpp `tools/server/server-models.h`, read
2026-08-31 — note there is no `failed`, which the step's planning note assumed).

| upstream | cria's phase | why |
|---|---|---|
| `downloading` | `downloading` | its weights are being fetched — the same fact |
| `loading` | `starting` | a child is coming up and will answer |
| `loaded` | `running` | it is being served |
| `downloaded` | *(none)* | held, nothing resident |
| `unloaded` | *(none)* | held, nothing resident |
| `sleeping` | *(none)* | held, its child kept with the weights released |
| anything else | *(none)* | shown as written, never guessed at |

**Deviation, deliberate (settled 2026-08-31):** the step asked for a map onto
cria's phase vocabulary, and three of the six states have no true word in it. A
model nobody has asked for yet is not `starting` (nothing is starting) and not
`exited` (nothing was launched), and `running` would claim it is serving. So the
map is partial: those states get no phase, and every surface prints the router's
own word, which is what a reader wants anyway. Visible-and-absent beats
plausible-and-wrong (CODING-RULES §4), and a state upstream adds later lands in
the same place instead of being read as something cria knows. Rejected: widening
`serve.Phase` with a "held" value — it would reach `cria status --json`, the TUI
tone table and SERVE.md's phase contract for a state that is not a server's phase
at all.

`RouterChild` carries `Model`, `Aliases`, `State` (verbatim) and `Phase`
(possibly empty). `RouterChildren.Child(name)` matches on alias *or* reference —
that is the alias verification: cria only ever addresses a model the router has
just said it holds.

### Transports, on the Manager

Per STEP-2's boundary (engine supplies paths and knowledge, `serve` owns the
requests), the Manager gained two injected transports beside `probe`, `slots`,
`complete` and `bench`:

- `children` — `GET /models`, one look, 2s. Everything that can stand in the way
  comes back as `RouterChildren{Detail: …}`: a display fact, like a health probe,
  never an error.
- `commandRouter` — `POST /models/load` and `POST /models/unload`, body
  `{"model": "<alias>"}`, budget 15 minutes (a load is a model coming into
  memory; the same budget a warm gets).

`engine` supplies `RouterModelsPath()`, `RouterLoadPath()`, `RouterUnloadPath()`,
the state constants, and `RouterModelQuery(path, id)` — `?model=<id>`, url-escaped
— which is how `/slots` (and `/props`, unused by cria today) transfer to a child.
`Manager.RouterGenerating(record, id)` is the llama busy check addressed at one
child.

**Unverified upstream detail, named:** the README documents `GET /models` and the
existence of `POST /models/load|unload` but not their request shape. The body
field is read from llama.cpp's own handler (`json_value(body, "model", …)` in
`tools/server/server-models.cpp`, read 2026-08-31), not from a live run. **STEP-9's
live session must send one of each**; if the shape is wrong it is a one-line
change in `newHTTPRouterCommand`.

### The CLI, as settled

```
cria router [status|start|stop|models|include|exclude|load|unload]
  cria router include <id> [choice=option ...]
  cria router exclude <id>
  cria router load <id>
  cria router unload <id> [--ignore-busy]
```

- **`include` is the one verb that settles a model's combination** (settled
  2026-08-31): no separate `picks` verb. Re-running it on an entry the router
  already holds changes the combination, layering the picks typed now over the
  ones it is held under, over the entry's config defaults — the same three layers
  a launch uses, with the result stored because here the combination *is* the
  state. Two spellings for one write would be two ways to edit one file.
- `exclude` never reads the tree, so an entry whose file was renamed away can
  still be dropped. It reports whether it dropped anything.
- `models` is the composition without a router: what the next start would serve,
  with skipped models and their reasons. It exits 0 whenever it could read —
  `cria list`'s rule, since "the router holds nothing" is a true answer.
- `status` keeps the supervisor block and gains the per-child lines (name, the
  router's word, the reference), or why they could not be asked for.
- `load` / `unload` verify the name against the router's own listing first, then
  send. **`unload` refuses a model that is answering a request right now**, with
  `--ignore-busy` as the override — the gate `cria validate` puts in front of
  displacing a busy server, spelled the same way. That refusal is also what gives
  the `?model=` addressing a real caller rather than an unused method.
- Every verb that takes no argument refuses one, and a bare `cria router` is
  still `status`.

### Docs touched here

Specs stay with STEP-9, with one exception: `internal/config/agents.md` — the
embedded file cria writes into the config root — now says that which entries the
router serves is *not in the tree*, and names `include` / `exclude` / `models`.
An agent reading the old text would go looking for a config key that does not
exist. `cria docs`, and the schema behind it, are unchanged: nothing here adds a
config key.

### Structure-test rewrites, each paired

| Rewritten | Replacement | Why |
|---|---|---|
| `engine::TestThePresetIsTheEngineFilesArgs…` / `…RefusesWhatItCannotWrite` call sites | same tests, `RouterPreset(defaults, nil)` reading `composed.Preset` | the composition grew models and a verdict list |
| `serve::startRouter` fixture and its four direct `StartRouter` calls | same tests over `routerTree(router, entries…)` | the router is started from the tree, since what it serves is the tree's entries |
| `cli::fakeServers.StartRouter` | new signature + five new fake methods | the `servers` seam grew the models surface |

### Mutation checks

- The section stops writing `alias`: reddens `engine::TestThePresetCarriesOne
  SectionPerIncludedModel`, `serve::TestTheRouterServesTheModelsItsStoreHolds`,
  `serve::TestModelsTheRouterCannotCarryAreSkippedNotFatal`.
- Two models allowed to share a section: reddens
  `engine::TestTwoModelsCannotShareOneSection`.
- `RouterServes` accepting any program: reddens
  `engine::TestOnlyTheEntriesTheRoutersProgramServesCanBeIncluded` and
  `serve::TestModelsTheRouterCannotCarryAreSkippedNotFatal`.
- Sections composed with `config.Resolve` (the llama engine's args leaking in):
  reddens `serve::TestTheRouterServesTheModelsItsStoreHolds`.
- `ResolveUnder` ignoring the engine level it was given and reading
  `entry.EngineArgs`: reddens
  `config::TestResolveUnderTakesTheEngineLevelFromTheCaller` and
  `serve::TestTheRouterServesTheModelsItsStoreHolds`.
- `unloaded`/`sleeping` mapped to `starting`: reddens
  `serve::TestTheChildStatesMapOntoTheirPhases`.
- A broken store degrading into an empty router: reddens
  `picks::TestARouterStoreThatCannotBeReadIsRefused` and
  `serve::TestARouterStoreThatCannotBeReadRefusesTheStart`.

Every production file was restored and the suite re-run green.

### Suite

`gofmt -l .` empty; `go vet ./...` clean; `go test -count=1 ./...`:

```
ok  cria 0.189s · cria/internal/cli 3.837s · cria/internal/config 0.542s
ok  cria/internal/engine 0.698s · cria/internal/format 0.573s
ok  cria/internal/hubapi 1.507s · cria/internal/hubcache 1.079s
ok  cria/internal/picks 1.378s · cria/internal/procs 1.277s
ok  cria/internal/selfupdate 0.855s · cria/internal/serve 4.647s
ok  cria/internal/tools 2.151s · cria/internal/tui 5.660s
```

No expected reds.

A hand-run of the built binary against a throwaway `HOME` (no port, no process
touched) walked the new surface end to end: include with a pick, include a flat
entry, the mlx refusal, `models`, the store on disk, and a pick naming an option
the entry does not have.

### The pick-change fixture

`serve::TestAPickChangeRegeneratesTheSectionAtTheNextStart` is the step's
acceptance fixture, run against fakes: start the router with `context=short`,
stop it, change the router's stored pick to `long`, start again — and the two
composed presets differ by exactly `cache-type-k = q8_0` → `cache-type-k = f16`.
Nothing else in the file moves, which is what the `[*]`-stays-out-of-sections
decision buys. The live half of it stays on STEP-9's list.

### What STEP-9 must know

1. **The TUI has no router surface yet.** `internal/tui` still knows the router
   only as a colour (`backendTones[router]`, pink). The view has to reach:
   `RouterServer`, `RouterSnapshot`, `StartRouter`, `StopRouter`,
   `RouterModels`, `RouterChildren`, `RouterGenerating`, `RouterLoad`,
   `RouterUnload` — and the TUI's own `servers` seam plus its fakes grow with it.
2. **The engine toggle** currently walks `config.Backends()`
   (`tui::TestTheBackendToggleWalksEveryBackend`). The router's view is a list of
   the models it holds, not a filtered entry list, so the toggle becomes a walk
   over `config.Engines()` with the router's third position rendering that view.
3. **Inclusion needs a keypress** — the picker doctrine applies (the TUI is the
   only writer of `choices.json`; here the CLI already writes, so both may).
   `picks.LoadRouter` / `picks.SaveRouter` take a directory:
   `serve.RouterStateDir(serve.Root())`.
4. **A child has no phase for three of its six states** (table above). The TUI's
   tone map must key off `RouterChild.State`, not off `Phase` — a `Phase` of ""
   is normal, not missing data.
5. **Specs owed** (none were written here): `docs/specs/SERVE.md` — the models
   store, the composition and its per-model verdicts, the child states and the
   partial phase map, the two per-model requests; `docs/specs/CONFIG.md` — that
   the router's sections carry entry + picked args with `[*]` as the engine level
   (`config.ResolveUnder`), and that inclusion is state rather than a key;
   `docs/specs/CLI.md` — the eight verbs, `include`'s stored picks, `unload`'s
   busy gate and `--ignore-busy`'s second home; `docs/ARCHITECTURE.md` — the
   router store beside the preset and the record.
6. **`docs/BACKLOG.md`** — the Engines entry retires when the plan lands, and the
   router-aware `cria validate` goes in as future work (OVERVIEW, out of scope).

### Live e2e checklist for STEP-9 (as it would be run)

Machine cleared; router on a spare port (11437), qwen untouched on 11434.

1. `cria router include <small-a>` and `cria router include <small-b> quant=<q>`
   — the store is written; `cria router models` lists both with their references.
2. `cria router start` — the composed `preset.ini` carries `[*]` plus one section
   per model, each with `alias = <entry-id>`; `cria router status` goes green and
   lists both children with upstream's word for each.
3. **STEP-7's deferred smoke, still owed**: a preset whose only section is `[*]`
   is accepted; `/health` green with no model loaded on a cria-composed launch;
   `cria router stop` leaves the port free and no record behind.
4. A chat completion naming `<small-a>` routes and answers; `GET /models` shows
   it `loaded` and `cria router status` says so.
5. `cria router load <small-b>` then `cria router unload <small-b>` — **the one
   unverified request shape in this step**: confirm the `{"model": …}` body is
   what upstream reads, and that the states move as `cria router status` reports.
6. `cria router unload <small-a>` *while* a completion streams — the busy gate
   refuses; `--ignore-busy` goes through.
7. Change a pick (`cria router include <small-a> <choice>=<other>`), restart the
   router, `diff` the two presets — exactly that section's line changes.
8. pi-llama-cpp pointed at the router: models listed by entry id, swapped by
   naming them (the plan's end-to-end goal).
9. Meanwhile: `cria start`/`cria status` on a llama entry and an mlx entry still
   behave exactly as before.
