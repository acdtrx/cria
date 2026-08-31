# STEP 6 — the migration session; phase 2 closes

Status: not started

## Intent

The real tree moves to the new shape in one sitting: every profile's args
rewritten as keys, the stable llama params extracted to
`engines/llama.toml`, mlx's to `engines/mlx.toml`, and the whole thing
proven live by argv diff. Feature-building mode: no tooling, one honest
rewrite with eyes on it.

## Files likely touched

- The user's tree (agent-editable by project rule): ~15 profiles under
  `models/`, new `engines/llama.toml` + `engines/mlx.toml`.
- This file: the migration record — what moved where, the survey result on
  repeatable flags, the live diff.

## Decisions made during planning

- **Survey first**: every profile's args listed, repeated-flag check run;
  the engine-config extraction list agreed with the user before editing
  (candidates from the backlog's original entry: `ngl = 99`, `fa = on`;
  what else is uniform emerges from the survey — extraction is only for
  params that are genuinely machine-stable, not a dedup crusade).
- **Context stays literal** (OVERVIEW ruling 3): profiles keep the exact
  `c` and `parallel` values llama receives (qwen: `c = 262144`,
  `parallel = 1` as it stands today); layout comments remain the human's
  division notes.
- **Proof is a diff**: for every profile, the composed command line before
  vs after migration (captured pre-edit) — identical argv, no exceptions.
- **Live smoke**: qwen started from the migrated profile, `/slots` layout
  verified, one real completion; one mlx entry likewise (its first
  engine-config read). Backups of the pre-migration tree go to the user's
  own habits (the tree versions as its own git repo per their note) — cria
  and this session do not invent a backup scheme.
- User present: this step edits their daily driver and wants their eyes at
  review points (serving-machine care; collaboration rhythm).

## Acceptance criteria

- Every profile parses under the new schema; the old shape's refusal is
  never seen again on this tree.
- The argv diffs recorded here, all clean; the two live smokes green.
- Phase 2 ends: full suite green, committed; the machine serving from the
  migrated tree.
