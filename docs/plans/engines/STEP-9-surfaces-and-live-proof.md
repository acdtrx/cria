# STEP 9 — surfaces, specs, and the live proof; plan closes

Status: not started

## Intent

The router becomes a first-class engine in every surface, the contracts
land where they live, and the plan's real goal is proven: pi-llama-cpp
driving a cria-managed router.

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
