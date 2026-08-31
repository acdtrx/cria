# STEP 6 — the extraction session; phase 2 closes

Status: not started

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
