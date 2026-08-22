# Step 1 — `[[choice]]` schema

Status: not started

## Intent

Entries parse `[[choice]]`/`[[choice.option]]` strictly, every validation and
collision rule from `docs/specs/CONFIG.md` (Choices) enforced at load, and
`cria docs` + the `cria new` scaffold document the schema by construction.
Nothing consumes choices yet.

## Files likely touched

- `internal/config/config.go` — `Choice`/`ChoiceOption` types on `Entry`.
- `internal/config/load.go` — decoding + validation.
- `internal/config/schema.go`, `internal/config/docs.go` — schema table,
  example rendering.
- Matching tests.

## Decisions (from planning)

- Choice keys: `name` (required, unique within the entry, id charset). Option
  keys: `name` (required, unique within its choice, id charset), `quant`
  (llama entries only — same rule as the entry key), `repo`, `args`. Anything
  else is an unknown-key error.
- A choice needs ≥1 option; the first option is the config default. One option
  = a named always-on bundle, legal by design (settled 2026-08-22).
- Collisions checked pairwise, never by enumerating combinations: a leading-`-`
  token shared between the entry's `args` and any option's, or between options
  of two *different* choices, is a load error naming both homes. Same-choice
  overlap is free. Comparison is by token; values ignored.
- `quant` may be set by options of at most one choice; `repo` likewise. The
  cria-owned-flag refusal extends to option `args` verbatim.
- The `cria docs` example per backend gains one commented-out `[[choice]]` axis
  with a pointer sentence — choices are opt-in structure, and the scaffold
  `cria new` writes stays a launchable flat entry.
- All of this is per-entry validation: a bad choice breaks only its own file,
  reported with file and offending key, as ever.

## Acceptance criteria

- Unit tests: round-trip of a choices entry; every refusal above (dup names,
  bad charset, zero options, cross-choice and base-vs-option token collision,
  same-choice overlap allowed, quant-on-mlx, quant/repo in two choices,
  cria-owned flag in option args); a flat entry unchanged.
- `cria docs` output contains the choices schema and the commented example;
  the schema test keeps docs and parser in one source.
- Suite green or expected reds named (mid-phase step).
