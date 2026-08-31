# STEP 6 — the extraction session; phase 2 closes

Status: done (2026-08-31) — extraction complete, composition-proven; live
smokes deferred by user ruling (record below)

## Session record (2026-08-31, user present and ruling)

Rulings: `engines/llama.toml` extracts **`-ngl 99` only** — the true
intersection, all 13 llama profiles; `-fa on` stays per-profile (12/13 set
it; llama's default verified `auto` via `--help`, so pinning `on` remains
meaningful, and the embedding profile setting neither must not inherit it —
override cannot unset). `engines/mlx.toml` extracts
**`--prompt-cache-size 10 --prompt-cache-bytes 8G` only** — host-RAM
policy; `--max-tokens` ruled model-specific and stays per-profile.

Method and proof:
1. Pre-extraction capture: every entry's `Resolve`d args under config
   defaults, via a temporary test in the worktree (deleted after).
2. Engine files written; the extracted tokens removed from all 15 profiles
   by surgical text edit, each file re-parsed and its args verified equal
   to expected removal — comments and formatting untouched.
3. Post-extraction capture, fresh (`-count=1` — the first comparison hit
   go's test cache replaying pre-edit output; caught because "identical
   order" was too good to be true, the merge puts engine args first).
4. All 15 entries flag-group-equivalent, the only difference the expected
   one: extracted flags lead the composed line. FAILURES: 0.

Live smokes: **deferred by user ruling** — the serving qwen38-27 is not to
be restarted, and both mlx profiles are too large to load beside it. The
engine files' first live read happens on the next natural start; the
composition-level proof above is the phase's verification. Suite green at
phase end (code unchanged since 3b2d712; config package re-run fresh).

## Intent

The real tree gains its engine files in one sitting: the flags that are true of
*this machine* rather than of a model — llama's `-ngl`, `-fa` and whatever else
the survey finds uniform — move out of ~15 profiles into
`engines/llama.toml` and `engines/mlx.toml`, and every profile's composed argv
is proven unchanged.

Nothing has to be rewritten to keep loading (OVERVIEW ruling 1, amended
2026-08-31): `args` is the same verbatim-token list it always was, so this step
is an extraction, not a migration. Feature-building mode: no tooling, one honest
edit with eyes on it.

## Files likely touched

- The user's tree (agent-editable by project rule): ~15 profiles under
  `models/`, new `engines/llama.toml` + `engines/mlx.toml`.
- This file: the extraction record — what moved where, and the argv diffs.

## Decisions made during planning

- **Survey first**: every profile's args listed side by side; the extraction
  list agreed with the user before a file is touched (candidates from the
  backlog's original entry: `-ngl 99`, `-fa on`; what else is uniform emerges
  from the survey — extraction is only for flags that are genuinely
  machine-stable, not a dedup crusade).
- **A flag that is uniform except in one profile still extracts**: that profile
  keeps its own value and overrides the engine's, which is what the levels are
  for. It is the case worth having in the record.
- **Context stays literal** (OVERVIEW ruling 3): profiles keep the exact `-c`
  and `--parallel` values llama receives (qwen: `-c 262144`, `--parallel 1` as
  it stands today); layout comments remain the human's division notes.
- **Proof is a diff**: for every profile, the composed command line before vs
  after (captured pre-edit) — identical, no exceptions. Where an extracted flag
  moves to the front of the line because the engine level composes first, the
  comparison is by flag/value pair rather than by string, and that profile is
  named here.
- **Live smoke**: qwen started from the edited profile, `/slots` layout
  verified, one real completion; one mlx entry likewise (its first engine-config
  read). Backups of the pre-edit tree go to the user's own habits (the tree
  versions as its own git repo per their note) — cria and this session do not
  invent a backup scheme.
- User present: this step edits their daily driver and wants their eyes at
  review points (serving-machine care; collaboration rhythm).

## Acceptance criteria

- Every profile still parses, and the tree loads with the two engine files in
  place.
- The argv diffs recorded here, all clean; the two live smokes green.
- Phase 2 ends: full suite green, committed; the machine serving from the
  edited tree.
