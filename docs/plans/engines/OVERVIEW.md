# Engines — engine modules, engine configs, and the router as a third engine

Cria stops being two backends if'd through the code and becomes what it
already is in practice: a llama-and-mlx runner with per-engine knowledge —
now given a named home. One Engine interface, three implementations (llama,
mlx, router), engine config files beside shared model profiles, and profile
args that stay the server's own flags, composed across those levels.

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
- **Ini-style keys** (2026-08-27) — **reversed 2026-08-31, see ruling 1**. What
  survives of it: the router's preset is still upstream's ini, and precedence
  is still least-specific-first across engine → entry → picks. What is gone:
  the tree's args are argv tokens again, and there is no key dialect to
  compose from.

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

1. **TOML stays the tree's syntax, and args stay verbatim argv tokens**
   (amended 2026-08-31, user, reversing the key=value shape ruled the same
   day at plan review and built in STEP-5). `args = ["-ngl", "99", "-fa",
   "on", "-c", "262144", …]` — exactly what the server binary takes, copied
   in and out without translation. Reasons on record: `"key = value"` is a
   third dialect only cria speaks, it forces author-side translation of every
   profile, it bans the short aliases the real tree uses (`-ngl`, `-fa`) and
   it makes a repeated flag inexpressible. The router preset — the whole
   reason the key shape was attractive — needs nothing from the tree:
   upstream canonicalizes its own aliases (probe-proven 2026-08-31: `ngl`,
   `fa`, `c`, `temp`, `ctk` all echo back long), so a preset line is a flag
   group with its dashes stripped, derived at composition time in phase 3.
   The upstream ini remains composed output only — never the tree's format.
   Consequences: cross-level override works at **flag-group** granularity
   (a flag plus the tokens after it), repetition inside one list stays legal,
   and the cross-part collision refusal narrows to options of two different
   choices. STEP-6 shrinks to an extraction session.
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
4. **Repeatable flags are expressible again** (amended 2026-08-31 with
   ruling 1): a list is a command line, so a flag written twice is passed
   twice. The limit this ruling was about belongs to the router's preset
   alone, and only phase 3 has to answer for it.
5. **The router is an engine, not a backend an entry may declare** (settled
   2026-08-31 in STEP-7, following from ruling 2). Inclusion is
   router-scoped state, and the same entry carries llama picks and router
   picks — so `backend = "router"` cannot be what includes it, and is
   refused. The tree declares two sets: the engines it can configure
   (`config.Engines()`: engine files, records, display) and the narrower
   backends an entry may name (`config.Backends()`: llama, mlx). Consequences
   for the remaining steps: `cria new` scaffolds no router entry (it refuses
   with why), the TUI toggle walks the *backends* until STEP-9 gives the
   router a view of its own, and a router-included entry stays an ordinary
   llama entry — quant and all — so nothing in the entry schema widens for
   it. STEP-7's file records the full reasoning and what was rejected.

## Scope

- `internal/engine` (new): the Engine interface and the llama, mlx, router
  implementations. Serve, hubcache/hubapi, cli and tui consume engines
  through it; the import graph stays acyclic (engine imports config+tools,
  nothing imports back into it).
- `internal/config`: engine config files, the flag-group merge across the
  three levels, the narrowed collision rule (per ruling 1).
- `internal/serve`: lifecycle generalized over engines; router process +
  child records/status/phases.
- `internal/tui` + `internal/cli`: engine toggle, router view, surfaces.
- `docs/specs/`: CONFIG.md and SERVE.md updated in the same edits that
  change their contracts; ARCHITECTURE.md when the module shape lands.
- The user's real tree: the machine-wide flags extracted by hand in one
  session (STEP-6).

## Out of scope

- `cria validate` against router-included entries (it validates entries
  under the llama engine; a router-aware validate is future work, noted in
  the backlog when this plan closes).
- Slots visibility (its own backlog entry; the collector's engine home is
  decided here, the feature builds after).
- Remote hosts, new telemetry, any upstream feature not probed.

## Constraints & risks

- **The tree's args shape is unchanged** after ruling 1's reversal, so no
  profile has to be rewritten to keep loading. STEP-6 is an extraction
  session, not a migration: what moves to `engines/<engine>.toml` moves
  by hand, with the argv proven identical.
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
- STEP-5 — engine config files and the three-level merge: verbatim argv
  args, override by flag group, the narrowed collision rule; `cria docs`
  follows by construction; CONFIG.md same-edit.
- STEP-6 — the extraction session: the machine's own flags lifted out of
  ~15 profiles into engines/*.toml, user reviews; live smoke on the dev
  Mac. Phase end.

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
- Phase 2 ends with the real tree's machine-wide flags extracted and a live
  smoke: qwen serving from the edited profile with identical composed argv
  (diffed against the line captured before the edit).
- Phase 3 ends with the real goal: pi-llama-cpp driving a cria-managed
  router — models listed, loaded, swapped by request — while `cria` shows
  the router's state; llama and mlx engines still serving their entries
  unchanged.
