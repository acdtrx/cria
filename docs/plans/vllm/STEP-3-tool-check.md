# STEP 3 — vllm in the tool check

Status: not started

## Intent

cria detects the `vllm` program like its other managed tools: resolved from the
`[tools]` override or `PATH`, reported found or missing with what its absence
disables and the one action that clears it. Presence is the contract.

## Files likely touched

- `internal/tools/tools.go` — `VLLM Name = "vllm"`, a `Report` field, `All()`,
  `checkVLLM` mirroring `checkMLXLMServer`.
- `internal/config/load.go`, `config.go`, `schema.go` — `[tools] vllm` key
  (`vllm = "/abs/path"`), documented in `cria docs`.
- Tool-report consumers (TUI tools rendering, CLI) — expected to follow
  `Report.All()` with no branch; confirm.
- `docs/specs/TOOLS.md`, `docs/specs/CONFIG.md` (`[tools]` row) — same edit.

## Decisions made during planning

- **Presence-only, no version probe.** Nothing cria depends on is tied to a vLLM
  version today (health, `/v1/models`, OpenAI endpoints are long-stable). A
  version gate would be added the day a real incompatibility is found, not before.
- **No probe exec at all** — `vllm --version` imports torch and takes seconds;
  the tool check sits on the TUI's startup path.
- The fix text names the install STEP-1 settles (e.g. "install vLLM so `vllm` is
  on PATH, or set `[tools] vllm`"), not a pip incantation.
- Absence is normal on the Macs, exactly like mlx_lm.server on linux.

## Acceptance criteria

- Unit: found via override, found via PATH, missing (with disables + fix).
- `cria docs` lists the `vllm` tools key.
- Suite run and recorded.
