# STEP 4 — the vllm engine and backend

Status: not started

## Intent

`backend = "vllm"` is a backend an entry may declare, served by a new engine in
`internal/engine`. Everything the lifecycle asks per backend it asks the engine;
no new if-site outside the engine and the schema.

## Files likely touched

- `internal/engine/vllm.go` (+ test), `engine.go` — registered in `engines`.
- `internal/config/config.go` — `BackendVLLM`, its row in the backend table
  (per-entry, its model flag — see decisions).
- `internal/config/schema.go` — per-key examples for vllm (repo, name, args,
  engine-file args); `cria docs` follows by construction.
- `internal/cli/new.go` — `cria new --vllm` (or whatever form the backend flag
  takes) scaffolds a vllm entry.
- `internal/tui/styles.go` — the backend's color.
- `internal/config/agents.md` — if it names the backends.
- `docs/specs/CONFIG.md`, `docs/specs/SERVE.md` (phases, warm), TOOLS.md order.

## Decisions made during planning

- **Engine answers** (from the 2026-09-24 probe):
  - `Program()` = `vllm`. The process table shows `python …/bin/vllm serve …`;
    `isManagedServer` already accepts a script's path in argv[1], and identity
    capture settles on containment of `vllm`.
  - `ModelArgs` = `serve --model <repo>`. `vllm serve` takes the model either
    as positional `model_tag` or as `--model` (both in 0.30.0's `--help=all`);
    cria composes the flag, so the backend has a model flag like every other
    and `internal/engine`'s test tying `config` model flags to what engines
    compose holds unchanged. cria composes `--host`/`--port` after it.
  - `TakesQuant()` = false — a vLLM quantization is its own repo (NVFP4, AWQ…),
    the mlx rule; hub presence then counts the whole repo.
  - `HealthPath()` = `/health`; `LoadsLazily()` = false (port opens only when
    the engine is ready — no warm); `SlotsPath()` = none.
- **The model flag no args list may restate** is `--model` (the backend table
  row in `internal/config/config.go`), so an entry's args cannot name a second
  model. A stray positional token in args is not specially refused — args are
  verbatim, and vLLM itself rejects a second model tag.
- **Revision pinning stays in args** (`--revision <sha>`) — passthrough, as with
  every server flag. cria's presence check follows `main`; a pinned revision
  that differs is a known, accepted imprecision, noted in CONFIG.md.
- **`--served-model-name` is left to the entry.** vLLM's default is the repo,
  which is what cria's completion probes (validate, bench, warm) send.
- The `downloading` phase works unchanged: vLLM fetches through
  `huggingface_hub` into the shared cache, and cria watches the cache.

## Acceptance criteria

- Unit: engine answers pinned; composed argv for a vllm entry is
  `<vllm> serve --model <repo> --host H --port P <args…>`; `backend = "vllm"` loads, a
  quant key on it is refused, `--model` in its args is refused.
- `cria docs` shows the vllm example entry and engine file; `cria new` scaffolds
  one that loads.
- Suite run and recorded.
