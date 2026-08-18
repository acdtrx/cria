# Step 4 — tool detection and the cache-age check

**Phase 2 · Status: done (2026-08-18)** — suite green, all gates pass.
Threshold pinned: **b8498** (PR ggml-org/llama.cpp#20775, verified via GitHub
tags), recorded in TOOLS.md in the same change. Planning correction found
during implementation: `llama-server --version` never prints `bNNNN` — the
binary prints `version: 8498 (…)` (old shape) or
`version: X (build 10450, commit …)` (current shape), on **stderr**; the parser
reads only those two documented positions, anything else is "unverified".
Real-Mac check: llama-server b10450 (`/opt/homebrew/bin`, hub cache ok),
mlx_lm.server found (`~/.local/bin`), hf found. CLI rendering deliberately not
added (CLI.md defines none; the report routes to the TUI pane and refused
starts).

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
