# Engines — engine modules, engine configs, and the router as a third engine

Cria stops being two backends if'd through the code and becomes what it
already is in practice: a llama-and-mlx runner with per-engine knowledge —
now given a named home. One Engine interface, three implementations (llama,
mlx, router), engine config files beside shared model profiles, and profile
args as ini-style keys the engines compose deterministically.

Origin: `docs/BACKLOG.md`, Engines — direction, build shape, refactor
discipline and probe findings all recorded there (2026-08-27..31); the entry
retires when this plan lands. The live router probe PASSED 2026-08-31; its
findings bind this plan and are restated where they decide something.

## Goal

- The tree's model profiles carry only what is true of the model wherever it
  runs; per-engine config owns how this machine runs each engine.
- The same profiles serve under the llama engine (process per entry) and the
  router engine (one supervised router process, models loaded on demand) —
  pi-llama-cpp gets the router it requires, driven by cria.
- Engine-specific knowledge — flag composition, endpoints, phases, stats —
  lives in engine modules by design (ruled 2026-08-27): cria is a llama-and-
  mlx runner, not a generic process manager. The tree's args stay passthrough.

## Settled rulings this plan builds on (dated in the backlog entry)

- **Engine modules know their engine by design** (2026-08-27): schema-field
  composition, endpoint knowledge, phase semantics, stats collection. The
  passthrough layer stays flag-agnostic.
- **Build shape** (2026-08-27): an Engine interface extracted from the ~10
  existing seams, behavior-preserving, llama+mlx first; router lands as the
  third implementation, never an eleventh if-site.
- **Refactor discipline** (2026-08-27): step zero classifies every test as
  contract (never red) or structure (may go red mid-phase, named, rewritten
  against the new shape); no shim survives a phase end; fakes follow the
  interface; structure-test deletions are paired with their new-shape
  replacement. Contract tests keep the full never-delete protection.
- **The cut** (2026-08-27): model profiles shared verbatim between llama and
  router engines; separate router profiles rejected (fork-and-drift).
- **Ini-style keys** (2026-08-27): key=value args; key→argv is mechanical;
  composition order-independent with exact key collisions; precedence is
  upstream's own (model section > engine `[*]`), argv composition and preset
  composition are two spellings of one merge.

## Probe findings that decide things (2026-08-31, build 10450)

- **The router is a supervisor** of child llama-server processes behind one
  port. The Engine interface's altitude is therefore make-this-entry-serve /
  stop-serving / what-is-its-state; the router engine speaks the documented
  API (`GET /models`, `POST /models/load|unload`, autoload) and never
  touches child processes.
- **Upstream canonicalizes ini keys** (short→long, echoed back via
  `GET /models`) and **refuses unknown keys loudly at startup**, naming key
  and section. Cria composes keys verbatim and owns no mapping.
- **`?model=` addressing works on `/props` and `/slots`** — the llama
  engine's probes and stats transfer to the router engine wholesale.
- **Eviction at `--models-max` is client-driven stop-one-start-another.**
- Wrinkles to verify early (STEP-4): quant-tag normalization vs section
  names (`alias` is the untested lever for entry-id naming); auto-parallel
  appearing to multiply context up while explicit `--parallel` divides.

## Rulings settled at plan review (2026-08-31, user)

1. **TOML stays the tree's syntax**; args become key=value inside it, and
   the upstream ini is composed output only — never the tree's format.
2. **Router inclusion lives in router-scoped state**, organized as a
   **subfolder per engine** under the state dir: the router's folder holds
   which profiles are active under it and their router picks, which may
   differ from the llama engine's picks for the same entries.
3. **Context and parallel stay passthrough keys.** The profile carries the
   literal values llama receives (`c`, `parallel`); cria computes nothing
   and the human divides when reading — layout comments carry that note, as
   qwen's already does. Rejected: engine-composed pool (`context ×
   parallel`, the 2026-08-27 recommendation) — simplicity won, and this
   keeps cria's flag-agnosticism total; `host`/`port` remain the only
   schema-composed fields. The auto-parallel probe wrinkle is thereby
   informational for profile authors, not load-bearing for cria.
4. **Repeatable flags are inexpressible as keys** — accepted as a limit
   (upstream's preset shares it) unless STEP-6's migration survey finds a
   real profile needing repetition.

## Scope

- `internal/engine` (new): the Engine interface and the llama, mlx, router
  implementations. Serve, hubcache/hubapi, cli and tui consume engines
  through it; the import graph stays acyclic (engine imports config+tools,
  nothing imports back into it).
- `internal/config`: ini-keyed args/options, engine config files, the
  key-exact collision rule, context/parallel schema fields (per ruling).
- `internal/serve`: lifecycle generalized over engines; router process +
  child records/status/phases.
- `internal/tui` + `internal/cli`: engine toggle, router view, surfaces.
- `docs/specs/`: CONFIG.md and SERVE.md updated in the same edits that
  change their contracts; ARCHITECTURE.md when the module shape lands.
- The user's real tree: migrated by hand in one session (STEP-6).

## Out of scope

- `cria validate` against router-included entries (it validates entries
  under the llama engine; a router-aware validate is future work, noted in
  the backlog when this plan closes).
- Slots visibility (its own backlog entry; the collector's engine home is
  decided here, the feature builds after).
- Remote hosts, new telemetry, any upstream feature not probed.

## Constraints & risks

- **Biggest schema change since choices.** Feature-building mode: no
  dual-read of the old args shape — old profiles fail loudly with the
  manual fix named; the migration session (STEP-6) is part of the plan.
- **The refactor discipline binds phase 1** (and its spirit binds the rest).
- Upstream context semantics are moving (unified KV, auto-parallel): the
  engine module contains the blast radius; STEP-4 verifies before STEP-5
  encodes anything.
- llama-server minimum: router mode requires a recent build (probed on
  10450). Tools detection reports capability honestly; no auto-installs.
- Serving-machine care: build/test bursts and STEP-4/6/9 live runs need the
  machine cleared, as for validate's live proof.

## Phases and steps

**Phase 1 — the Engine interface, behavior-preserving** (steps 1–3; llama +
mlx only, suite validates the extraction).

- STEP-1 — test triage: every test classified contract/structure, recorded
  in this directory; thin contract coverage strengthened where the triage
  exposes it.
- STEP-2 — `internal/engine`: the interface designed from the seams
  inventory; llama + mlx implementations; serve's seams delegate (command
  and tool composition, health, lazy-load, slots capability, record quant
  rule).
- STEP-3 — the seams outside serve (hub presence semantics, scaffold,
  schema examples' engine hooks); shims gone; structure tests rewritten.
  Phase end: green, committed.

**Phase 2 — the config cut** (steps 4–6).

- STEP-4 — settle by experiment: the `alias` lever, one confined live
  probe; outcome recorded as a ruling. (The context experiment was dropped
  2026-08-31 with ruling 3 — passthrough needs no emit rule verified.)
- STEP-5 — ini-keyed profiles and engine configs: schema, key-exact
  collisions, upstream precedence; `cria docs` follows by construction;
  CONFIG.md same-edit.
- STEP-6 — the migration session: the real tree rewritten (~15 profiles +
  engines/*.toml), user reviews; live smoke on the dev Mac. Phase end.

**Phase 3 — the router engine** (steps 7–9).

- STEP-7 — router lifecycle: engine config, preset composed into the state
  dir, router process start/stop/records/health.
- STEP-8 — models under the router: inclusion + per-entry combos in
  router-scoped state, load/unload, child phases and status, stats via
  `?model=`.
- STEP-9 — TUI engine toggle and router view; specs; live e2e with
  pi-llama-cpp against the cria-managed router; backlog entry removed.
  Plan end: suite green, branch rebased, ff-merged, worktree pruned.

## End-to-end verification

- Full suite green at each phase end; phase 1 additionally proves the
  extraction changed no behavior (contract tests untouched and green
  throughout).
- Phase 2 ends with the real tree migrated and a live smoke: qwen serving
  from the migrated profile with identical composed argv (diffed against
  pre-migration).
- Phase 3 ends with the real goal: pi-llama-cpp driving a cria-managed
  router — models listed, loaded, swapped by request — while `cria` shows
  the router's state; llama and mlx engines still serving their entries
  unchanged.
