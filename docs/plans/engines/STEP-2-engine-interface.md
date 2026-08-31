# STEP 2 — internal/engine: the interface, llama and mlx

Status: not started

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
