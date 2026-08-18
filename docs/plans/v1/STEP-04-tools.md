# Step 4 — tool detection and the cache-age check

**Phase 2 · Status: not started**

## Intent

`internal/tools`: resolve `llama-server` / `mlx_lm.server` / `hf` (config
`[tools]` override, then `PATH`), run the llama-server hub-cache version check,
and produce the degradation report the CLI and TUI consume (TOOLS.md).

## Files likely touched

`internal/tools/` (+ tests), `internal/cli/` (report shown on stub commands).

## Decisions made during planning

- **Research task inside this step**: pin the first llama.cpp build that stores
  `-hf` downloads in the standard hub cache; the number lands in the code and in
  `docs/specs/TOOLS.md` in the same change.
- Version obtained from `llama-server --version`; parse the build tag only
  (`bNNNN`); anything unparseable → "unverified", treated as too old (TOOLS.md).
- Report is data (found/missing/too-old per tool, resolved paths, disabled
  features), rendering belongs to the consumers.
- `mlx_lm.server` gets a presence check only — no version contract exists to
  test.

## Acceptance criteria

- Table tests over canned `--version` outputs: modern build, pre-threshold
  build, garbage output, missing binary.
- Manual check on the dev Mac: report matches reality for the actually installed
  tools; output recorded here with the suite result.
