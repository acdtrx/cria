# STEP 9 — surfaces, specs, and the live proof; plan closes

Status: **code and specs done (2026-08-31); the live e2e is owed** — suite green,
the surfaces and the four specs are in, and the nine-item checklist at the bottom
is staged for the user's session. The plan does not close until it is run.

## Intent

The router becomes a first-class engine in every surface, the contracts land
where they live, and the plan's real goal is proven: pi-llama-cpp driving a
cria-managed router.

## Files likely touched

- `internal/tui`: the engine toggle cycles three; the router's view — the
  included entries with child statuses, the router's own status box; the
  detail pane's composed-preset section view.
- `internal/cli`: status/list/docs surfaces cover the router engine.
- `docs/specs/CONFIG.md`, `docs/specs/SERVE.md`, `docs/specs/TUI.md`,
  `docs/ARCHITECTURE.md` — the engine model, dated; `docs/BACKLOG.md` —
  the Engines entry removed; a follow-up entry added for router-aware
  `cria validate` (out of scope here, noted honestly).

## Decisions made during planning

- The TUI treats the router as an engine tab whose rows are its included
  entries; phase vocabulary from STEP-8's mapping; the log view tails the
  router's log (children log through it — displayed raw, never parsed).
- The live e2e, user present:
  1. Router engine configured with two entries (one small, plus qwen's
     entry included at a reduced-context combo if headroom allows —
     STEP-4's data decides).
  2. pi-llama-cpp pointed at the router port: models listed, one loaded on
     demand, a swap by naming the other — the eviction watched from cria's
     view.
  3. Process engines untouched throughout on their own ports.
  4. Results recorded here; machine restored to the user's serving layout.
- Plan end mechanics: full suite green, branch rebased onto main, ff-merge,
  worktree pruned, tag after the merge per convention.

## Acceptance criteria

- Specs read as contract; the backlog entry is gone in the same commit; the
  follow-up entry exists.
- The live e2e checklist above recorded with outcomes; pi-llama-cpp
  actually swapped models through the cria-managed router.
- Plan done: suite green on main after the ff-merge.

## Outcome — the code half

### The engine toggle walks three

`prefs.next()`, `defaultPrefs()` and `decodePrefs` now read `config.Engines()`
where they read `config.Backends()`. The ruling behind it, recorded in
`docs/specs/TUI.md`: **the toggle chooses what the screen is about, not which
entries it filters.** Two of the three positions are an entry list filtered by the
backend those entries declare; the router's is a list of the models it holds,
because no entry declares the router. Rejected: a separate view with a key of its
own — the router is one of the ways this host serves models, and the key that asks
"which way" already exists.

A preferences file naming something cria does not serve is refused against the
same set, so `backend = "router"` in `ui.json` is now legal where it was refused.

### The router's view

`internal/tui/routerview.go`, drawn by `screen()` when the serve view is on the
router engine. The geometry is the entry view's exactly — list left, detail right,
stacked when narrow, list-only when too short, the picker floating over the list —
because it is the same gesture.

What a frame shows (120×40, a running router holding two of three included
models):

```
╭─ servers ──────────────────────────────────────────────────────────────────╮
│ router  running  router  pid 7788  :11437  up 1m0s                         │
╰────────────────────────────────────────────────────────────────────────────╯

╭─ serve · router ─────────────────╮╭─ model ──────────────────────────────────╮
│ ▸ gemma  not served   ggml-org/g ││ file     …/models/gemma.toml             │
│   qwen   loaded       unsloth/Qw ││ repo     ggml-org/gemma-3-4b-it-GGUF     │
│   typo   skipped                 ││ quant    Q8_0                            │
│                                  ││ client   gemma                           │
│                                  ││ state    not served                      │
│                                  ││          the running router does not     │
│                                  ││          hold it — it was included after │
│                                  ││          that router started, and the    │
│                                  ││          preset is composed at every     │
│                                  ││          start; restart the router to    │
│                                  ││          serve it                        │
│                                  ││ preset   [ggml-org/gemma-3-4b-it-GGUF:Q8_0]
│                                  ││          alias = gemma                   │
╰──────────────────────────────────╯╰──────────────────────────────────────────╯
selection p picks   server s stop · K kill · l log   global ⇥ backend · c cache …
```

- **The rows are the store's, never the running router's** — the models included
  in it, in id order, whether or not a router runs. What is being served right now
  is drawn *onto* them, so a model included since the last start reads `not served`
  with the pane explaining why, instead of vanishing from a list the operator just
  wrote. Served and skipped are one list: a dropped model in a footnote is a
  client's 404 hours later.
- **The state column is fixed width** (`len("not running")`, which also covers the
  longest state upstream publishes) so a model going from held to loading moves
  nothing beside it.
- **Upstream's word is the row's text; cria's phase is only the colour.** A row
  with no colour is normal — three of the six states have no phase (STEP-8's
  table). The four words cria writes where the router has none: `skipped`,
  `not running` (no router at all), `not served` (running router does not hold
  it), `unasked` (the listing could not be got). Each of the last three carries a
  sentence in the pane; `skipped` gets the composition's own reason.
- **The pane is the picking-and-seeing loop, router flavour**: the entry through
  the picks the *router* holds it under — file, repo, quant, client name, state,
  and the axes with the router's picks chipped — and then the **preset section** a
  start would write for it, where the entry view puts the composed command line.

### The router in the status box

The router's supervisor row joins the persistent status box whenever cria holds a
record of one, in every view. It is a server cria started, and a box that hid it
would answer "what is running here" wrongly — the same reason `cria status` now
reports it.

- `stop`, `kill`, `log` and `dismiss` reach it as ordinary box rows.
- `restart` and `bench` do not, and the bar stops drawing `r` for a box holding
  only the router (`boxTarget.replayable`, new): a restart replays the combination
  one *record* was composed with and the router was composed from no entry; a
  bench measures one model's server where the router fronts several. A key on the
  bar must do something.
- Its model-reference cell is empty, deliberately: the router serves no single
  model, and what it holds is the router view's list.

### One decision the records left open, settled here

**Where a record lives is a function of the record.** `Stop`, `Kill` and `Dismiss`
composed their path as `servers/<entry-id>.json`, so the TUI's stop key could not
reach a router whose record sits under `engines/router/`. The options were a
dispatch in the TUI (four call sites), a `KillRouter`/`DismissRouter` pair beside
`StopRouter`, or one rule in `serve`. The rule won: `recordPathOf(record)` answers
from the record's own backend, `end` no longer takes a path, and **`StopRouter` is
gone** — it had become `Stop` with extra steps. `cria router stop` calls `Stop`;
the seam and its fakes lost a method. Recorded in `docs/specs/SERVE.md`.

### Deliberate exclusions, so the next reader is not surprised

- **Include / exclude are not TUI gestures.** The brief allowed them only if they
  fell out of the picker machinery; they do not. Inclusion is a decision about
  *which entry*, taken from a different list than the one the router view shows,
  and a "toggle" key on the entry list would be a fourth meaning for the selection
  scope. The empty router names `cria router include <id>` rather than pretending
  to be the place.
- **Load / unload are not TUI gestures either.** Unload's busy gate is a refusal
  to *read* — a sentence about somebody's request being cut off, with an override
  the CLI deliberately does not print on that line (`docs/specs/SERVE.md`,
  Validate). A keypress cannot carry that without a modal, and the picker doctrine
  exists to avoid modals.
- **The picker, on the other hand, did fall out**: `p` in the router's view opens
  the same box over the same axes and writes the *router's* store. One entry, two
  combinations, each edited where it is used. The store is re-read at the keypress
  rather than carried on the frame, because `cria router include` writes it too.
- **`cria list` still lists entries only**, and `docs/specs/CLI.md` records why: a
  router column would make the tree's listing depend on cria's state to be read.
  `cria router models` is the router's listing.
- **No `--wait` on `cria router start`** (STEP-7 deferred the question to here): a
  router goes green as soon as it is routing, and what a caller waits for is a
  *model*, which loads on the request that names it. There is nothing for a wait
  to add. Recorded in `docs/specs/CLI.md`.

### `cria status` covers the router

The router's record lives outside `servers/`, so `Snapshots()` never saw it and
`cria status` was blind to the one process the whole tree shares. It now reports
it as a block of its own after the entries' — supervisor facts, the preset where a
model reference stands, and the models it holds with the router's own word for
each — and a live router alone exits 0, like any live server. An exited router is
a crash report and is not asked what it holds.

`--json` gains a `router` key, **null** on a host with no router record: one
router per host, so a list would deny the shape and a zeroed object would read as
a router with pid 0. It is the document's one nullable field, and the key is
always present.

`cria --help` and `cria docs` needed no change: the help page already names the
eight verbs and `--ignore-busy`'s second home, and `docs` walks `config.Engines()`,
so the router's example engine file is printed by construction.

### One shape change under the surfaces

`engine.Composition` gained `Sections map[string]string` and `serve.ServedModel`
gained `Section string`: the section travels with the model rather than being cut
back out of the composed preset by a surface that would have to parse cria's own
output to find it.

### Structure-test rewrites, each paired

| Rewritten | Replacement | Why |
|---|---|---|
| `tui::TestTheBackendToggleWalksEveryBackend` | `tui::TestTheEngineToggleWalksEveryEngine` — same cycle claim, over `config.Engines()` | the toggle's set changed; STEP-7 renamed it the other way with "until STEP-9 gives the router a view of its own" |
| `tui::TestBackendToggleIsWrittenDown` tail | same test, one more press: llama → mlx → router → llama | the cycle is three long now |
| `tui::TestBoxTarget` | same table, `replayable` on every case, plus a router-only case | the box grew a target a restart cannot aim at |
| `serve::TestStopsTheRouter…` call sites (3) | same tests, `manager.Stop(record)` | `StopRouter` is gone |
| `cli::fakeServers.StopRouter` | removed with the seam method | same |

### Tests added

`internal/tui/routerview_test.go` — nine frame-level tests: the toggle reaching
the router's view and it not being an entry list, the router's own word per model
(a `sleeping` with no phase beside a `loaded` with one), a model included since
the start reading `not served`, the list standing with no router running, the
empty router naming its verb, the pane carrying the router picks *and* the preset
section, a skipped model's reason with no section, the router as a box row that
`s` stops and `l` tails, restart and bench not aiming at it, and the picker in the
router's view writing `models.json` while leaving `choices.json` untouched.

`internal/cli/status_test.go` — five: the router block with its models, a live
router alone exiting 0, an exited router not being asked what it holds, the JSON
`router` object (including a `sleeping` model with an empty phase), and `router:
null` on a host with none.

### Suite

`gofmt -l .` empty; `go vet ./...` clean; `go test -count=1 ./...`:

```
ok  cria 0.464s · cria/internal/cli 3.956s · cria/internal/config 1.075s
ok  cria/internal/engine 1.665s · cria/internal/format 0.581s
ok  cria/internal/hubapi 0.995s · cria/internal/hubcache 1.908s
ok  cria/internal/picks 1.128s · cria/internal/procs 1.734s
ok  cria/internal/selfupdate 1.819s · cria/internal/serve 4.115s
ok  cria/internal/tools 2.295s · cria/internal/tui 5.024s
```

No expected reds. Nothing in the suite binds a port or touches a process
(`TRIAGE.md`), so it ran while the machine was serving.

## Outcome — the docs half

- `docs/specs/SERVE.md` — "The models under the router" (the store and its two
  refusal doctrines, the section composition with `[*]` as the engine level, the
  six per-model skip verdicts, the composition being pure) and "What the router
  says its models are doing" (upstream's six states, the partial phase map and why
  it is partial, the listing as a display fact, alias-or-reference addressing,
  load/unload, the busy-gated unload, upstream's client-driven eviction). Plus:
  where a record lives is a function of the record; `cria status` reports the
  router.
- `docs/specs/CONFIG.md` — `router_args` as verbatim argv and the one arg list
  that never becomes preset keys; which entries the router serves is not in the
  tree; a router section carries the entry's args and the picked options' but
  never the engine's; one entry, one set of axes, a combination per engine; and
  the context/parallel passthrough ruling, which had no home outside the backlog
  entry being deleted (the auto-parallel observation lands there as the reason
  cria encodes no formula, not as a feature).
- `docs/specs/TUI.md` — the toggle choosing what the screen is about; the router
  view's five contracts (rows are the store's, upstream's word with a fixed-width
  column, a skipped model is a row, the pane as the picking-and-seeing loop,
  include/exclude/load/unload staying CLI verbs); and the router as a server in
  the status box with the two keys that skip it.
- `docs/specs/CLI.md` — the eight verbs with `include`'s stored picks,
  `exclude` never reading the tree, `models`' exit rule and `unload`'s gate; why
  there is no `--wait`; why `cria list` lists entries only; `cria status`'s router.
- `docs/ARCHITECTURE.md` — "an engine knows, it never acts" (the boundary as three
  implementations left it), engines-vs-backends, the per-engine state layout as a
  tree, the router's API as the fifth source of truth, and `internal/picks` added
  to the module table and the graph (it was missing; it owns both stores).
  *Noted, not fixed:* `internal/selfupdate` is still absent from that table —
  unrelated drift, left for whoever touches it.
- `docs/BACKLOG.md` — the **Engines** entry removed (it is this plan). Two entries
  carry forward what it held that is still open: **router-aware `cria validate`**
  under Serve (the follow-up the OVERVIEW promised, with its revisit trigger), and
  **the contract/structure triage as a template lesson** under a new Method
  heading — whether the discipline graduates into `CLAUDE.md` is a decision the
  plan's landing does not make. The slots-visibility entry's `(see Engines)`
  pointer now points at `docs/ARCHITECTURE.md`, and gains the `?model=` fact that
  makes the collector transfer to the router's models.

## What remains: the live e2e (user present, machine cleared)

Nothing below has been run. The router has never been started by this code
against a real llama-server — every rule in it is exercised against fakes.
Router on a spare port (11437); qwen untouched on 11434.

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
   unverified request shape in this plan**: confirm `{"model": …}` is what
   upstream reads (taken from `tools/server/server-models.cpp`, not from a live
   run), and that the states move as `cria router status` reports. If the shape is
   wrong it is a one-line change in `newHTTPRouterCommand`.
6. `cria router unload <small-a>` *while* a completion streams — the busy gate
   refuses; `--ignore-busy` goes through.
7. Change a pick (`cria router include <small-a> <choice>=<other>`), restart the
   router, `diff` the two presets — exactly that section's line changes.
8. pi-llama-cpp pointed at the router: models listed by entry id, swapped by
   naming them (the plan's end-to-end goal).
9. Meanwhile: `cria start`/`cria status` on a llama entry and an mlx entry still
   behave exactly as before.

**And the TUI's own half, new to this list** — the surfaces above have been read
only in test frames:

10. Open `cria`, press ⇥ twice: the router's view lists the models with the
    router's real words, and the box shows the supervisor with real health.
11. `p` on a model with axes, roll a pick: `models.json` changes, `choices.json`
    does not, and the pane's preset section follows on the next tick.
12. `l` on the router's box row tails its real log; `s` stops it and the record
    goes; `r` is absent from the bar while the router is the only server.
13. `cria status --json | jq .router` against a running router — the models array
    carries what `GET /models` published.

Record the outcomes here, restore the machine to the user's serving layout, then
close the plan: suite green, branch rebased onto main, ff-merge, worktree pruned,
tag after the merge.
