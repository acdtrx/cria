# STEP 4 — the vllm engine and backend

Status: done (2026-09-24) — phase 2 ends green

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

- [x] Unit: engine answers pinned; composed argv for a vllm entry is
  `<vllm> serve --model <repo> --host H --port P <args…>`; `backend = "vllm"` loads, a
  quant key on it is refused, `--model` in its args is refused.
- [x] `cria docs` shows the vllm example entry and engine file; `cria new` scaffolds
  one that loads.
- [x] Suite run and recorded.

## Result (2026-09-24)

- `internal/engine/vllm.go`: `vllm` program, `serve --model <repo>`, no quant,
  `/health`, not lazy, no slots signal. Registered after mlx, before the router
  (TOOLS.md order) — the TUI toggle walks llama → mlx → vllm → router.
- Config: `BackendVLLM`, table row `{vllm, --model, perEntry}`; per-backend
  examples for every key that has them (repo `Qwen/Qwen3-30B-A3B-FP8`, engine
  args `--gpu-memory-utilization 0.85`). The docs note naming the composed
  flags now names a shared flag once: `--model (mlx, vllm)`; `composedFlags`
  dedupes likewise.
- **One planning claim did not hold:** `internal/engine`'s model-flag test read
  the flag at `args[0]`; vllm's head opens with the `serve` subcommand. The test
  now checks the flag right before the model reference that closes the head —
  the same claim, located by the reference rather than by position.
- Composed argv, pinned in `internal/serve` `TestComposedCommand`:
  `/usr/local/bin/vllm serve --model Qwen/Qwen3-30B-A3B-FP8 --host 0.0.0.0
  --port 8000 --gpu-memory-utilization 0.85 --max-model-len 32768`.
- Tests added: engine answers; config accept/refuse (quant, `--model`); docs
  tests now walk every backend rather than naming llama and mlx; `cria new
  --vllm`; procs `python …/bin/vllm serve …` recognised; identity capture takes
  that shape on the first look; hub presence counts the whole repo for vllm.
  Tests that used `"vllm"` as the example of an unknown backend now use
  `"sglang"`.
- Touches outside engine/schema, and why: `internal/cli/help.go` (the
  hand-written help page must offer `--vllm`, enforced by a test);
  `internal/tui/styles.go` (the backend's colour — Sky, `#89dceb`: Peach is
  llama's, Green reads as a running phase). No new if-site anywhere.
- Docs: CONFIG.md (backend, model flag, revision pinning / presence follows
  `main`), SERVE.md (health, no warm, foreign scan, busy gate), CLI.md,
  TUI.md (Sky), ARCHITECTURE.md, README, CLAUDE.md tool enumerations,
  embedded AGENTS.md.
- Suite (phase-2 end): `go test ./...` — all 13 packages ok; `gofmt -l .`
  empty; `go vet ./...` clean. Phase 2 is green.
