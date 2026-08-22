# Step 1 — `[[choice]]` schema

Status: done

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

## Result

- `internal/config` gains `Choice`/`ChoiceOption` on `Entry`, a `kindTableArray`
  schema kind (checked per element under the same dotted key), `choiceSchema` +
  `choiceOptionSchema` rendered into `cria docs` and the examples from the
  parser's own definitions, and `resolveChoices`/`refuseFlagCollisions` in
  `load.go` for the rules that need a whole entry in view. Collisions compare
  homes pairwise (entry args + every option), never enumerating combinations.
- All acceptance-criteria tests present: 5 accept cases, 18 refusals, the
  broken-choice isolation test, and a docs test proving the commented example
  axis loads when uncommented while the scaffold stays a launchable flat entry.
- `go test ./...` fully green (no expected reds), `gofmt -l .` silent,
  `go vet ./...` clean — verified independently in review.
- Decided while implementing: a **flag token is leading `-`s followed by a
  letter** — a bare `-1` is a value, not a flag, so two parts passing the same
  number never collide (`docs/specs/CONFIG.md` amended in this step); error
  keys are dotted and index-free (`choice.option.args`), the *reason* naming
  the offending choice/option by name; `option = []` is refused as "nothing to
  pick between" while a missing option table hits the required-key path; the
  docs example shows one commented option table with a note to repeat it per
  pick.
- The `cria docs` LAYOUT paragraph and the embedded `agents.md` still carried
  "another quant is another entry file" — replaced with the choices rule in the
  same edit: both render on the page that documents the new schema, and a
  self-contradiction there is an agent trap.
