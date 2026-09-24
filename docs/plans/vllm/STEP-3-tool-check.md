# STEP 3 — vllm in the tool check, and the backend install guide

Status: done (2026-09-24) — the from-scratch dgx run of the vLLM recipe is deferred to STEP-6

## Intent

cria detects the `vllm` program like its other managed tools: resolved from the
`[tools]` override or `PATH`, reported found or missing with what its absence
disables and the one action that clears it. Presence is the contract.

And cria's "you bring the servers" gets a guide: `docs/BACKENDS.md`, one recipe
per tool, so a new machine is prepared deliberately rather than on the
assumption that cria makes backends work. The tool check's fix line points at
the recipe exactly when the tool is found missing.

## Files likely touched

- `internal/tools/tools.go` — `VLLM Name = "vllm"`, a `Report` field, `All()`,
  `checkVLLM` mirroring `checkMLXLMServer`.
- `internal/config/load.go`, `config.go`, `schema.go` — `[tools] vllm` key
  (`vllm = "/abs/path"`), documented in `cria docs`.
- Tool-report consumers (TUI tools rendering, CLI) — expected to follow
  `Report.All()` with no branch; confirm.
- `docs/specs/TOOLS.md`, `docs/specs/CONFIG.md` (`[tools]` row) — same edit.
- `docs/BACKENDS.md` (new) — llama.cpp (Homebrew on macOS; a CUDA source build
  on linux, from the dgx notes in `~/spark-setup/llama/`), mlx-lm, hf, vLLM (the
  STEP-1 recipe: pinned venv via uv, torch cu130, the two prebuilt FlashInfer
  packages matched to `flashinfer-python`, absolute `[tools] vllm`). Each recipe
  ends with the `[tools]` line and how to confirm cria sees it.
- `README.md` — the "you bring the servers" list links each tool to its recipe.
- `internal/tools/tools.go` — every *missing* fix names the recipe's URL.

## Decisions made during planning

- **Presence-only, no version probe.** Nothing cria depends on is tied to a vLLM
  version today (health, `/v1/models`, OpenAI endpoints are long-stable). A
  version gate would be added the day a real incompatibility is found, not before.
- **No probe exec at all** — `vllm --version` imports torch and takes seconds;
  the tool check sits on the TUI's startup path.
- The fix text names the install STEP-1 settles (e.g. "install vLLM so `vllm` is
  on PATH, or set `[tools] vllm`"), not a pip incantation.
- Absence is normal on the Macs, exactly like mlx_lm.server on linux.
- **The guide is a doc, not a command** (settled 2026-09-24, user): visible on
  GitHub before anyone installs cria, no code to maintain. Rejected: a `cria
  --backends` command — a terminal copy of prose that goes stale in two places;
  revisit only if the doc is repeatedly wanted from the terminal, and then as a
  section of `cria docs`, not a new flag.
- **Fix lines link, they do not inline.** A missing tool's fix stays one line:
  the install action plus
  `https://github.com/acdtrx/cria/blob/main/docs/BACKENDS.md#<tool>`. Outdated
  and unverified llama-server fixes keep their specific text — the guide is for
  installing, not diagnosing a build.
- The guide states versions as "what was verified, when", not as requirements —
  except the hard ones cria enforces (llama.cpp build ≥ 8498, router flag).

## Acceptance criteria

- [x] Unit: found via override, found via PATH, missing (with disables + fix).
- [x] `cria docs` lists the `vllm` tools key.
- [x] Every missing-tool fix links its recipe; a test pins that each linked anchor
  exists as a heading in `docs/BACKENDS.md`.
- [ ] The vLLM recipe, followed from scratch on dgx (STEP-6 may be where that
  happens), yields a `vllm` that starts with no nvcc on PATH. — deferred to STEP-6.
- [x] Suite run and recorded.

## Result (2026-09-24)

- `vllm` is a managed tool, presence-only and never executed (a test runs the
  exported `Check` against a `vllm` that would leave a marker if run). Missing:
  "starting vllm entries …", fix names `tools.vllm` and `BACKENDS.md#vllm`.
- `[tools] vllm` in config, schema and `cria docs`.
- The only report renderer, the TUI tools pane, follows `Report.All()`; no
  per-tool branch was needed. `All()` order: llama-server, mlx_lm.server, vllm, hf.
- `docs/BACKENDS.md` with `## llama-server`, `## mlx_lm.server`, `## vllm`,
  `## hf`. Every missing fix ends `— see …/docs/BACKENDS.md#<anchor>`; a test
  computes GitHub's slug for each `## ` heading and checks every linked anchor
  (mutation-checked: renaming a heading fails it).
- README's "You bring the servers" links each tool to its recipe; TOOLS.md and
  CONFIG.md updated in the same change.
- Suite: `go test ./...` all ok; `gofmt -l .` empty; `go vet ./...` clean.
