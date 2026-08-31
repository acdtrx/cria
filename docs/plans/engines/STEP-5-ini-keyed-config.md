# STEP 5 — ini-keyed profiles and engine configs

Status: not started

## Intent

The config cut itself: profile `args` (and choice option `args`) become
key=value maps; per-engine config files appear; composition merges engine
`[*]` + entry + picked options with upstream's precedence, spelled as argv
for process engines and as preset ini for the router. `cria docs` follows by
construction.

## Files likely touched

- `internal/config/`: schema (args as ordered key=value pairs; the
  key-exact collision rule replacing token heuristics; `context`/`parallel`
  schema fields per STEP-4's ruling), loading of `engines/<engine>.toml`,
  docs generation.
- `internal/engine/`: composition — merged keys → argv (llama, mlx) with
  the boolean rule mirroring upstream (`key = true` → bare flag), and the
  same merge → preset text (router, consumed in phase 3).
- `internal/serve/command.go` successor wiring; `docs/specs/CONFIG.md` in
  the same edit.

## Decisions made during planning

- **Format**: TOML stays the tree's syntax — args become a TOML
  array-of-strings of `"key = value"` lines or an inline table; settle for
  whichever preserves author order and comments best (author order matters
  only for readability now — composition is order-independent by key).
  The upstream *ini* file is an output cria composes, never the tree's
  input format: humans keep TOML, engines get their native spelling.
- **Precedence**: engine `[*]` < entry keys < picked option keys — the
  entry overriding its engine defaults is now legal and explicit (upstream's
  own model-section-over-global rule); a key colliding *within* one level
  stays a loud load error, and options of one choice still share keys
  freely.
- **Engine config files** live in the tree (`engines/llama.toml`, `mlx`,
  `router`): human/agent-owned like everything there; a missing file means
  empty defaults, never an error. cria's only tree writes remain root +
  AGENTS.md.
- **Booleans**: `true` → bare flag, mirroring upstream's documented rule;
  no other coercions. Values pass verbatim otherwise.
- **Repeatable flags**: inexpressible by design; the schema refuses a
  duplicate key in one part loudly. STEP-6's survey confirms no real
  profile needs repetition (none known at planning time).
- `host`/`port` stay schema fields as today; `context`/`parallel` join them
  exactly as STEP-4 ruled.
- Feature-building mode: the old `args` token-list shape is refused loudly
  with the manual fix named ("rewrite args as key = value lines; see
  cria docs"), no dual-read.

## Acceptance criteria

- Component tests: merge precedence, key collisions (within-level loud,
  cross-level override), boolean spelling, context/parallel composition per
  engine, engine-config absence, old-shape refusal message.
- `cria docs` and the schema render from one source, showing the new shape
  with both engines' examples.
- The composed argv for a representative migrated profile is byte-identical
  to its pre-cut composition (fixture-level proof staged for STEP-6's live
  diff).
- Suite state recorded; expected reds named (the real tree is still
  old-shape until STEP-6 — cria's own tests use fixtures, so none are
  expected from that).
