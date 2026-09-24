# vLLM — a fourth engine

cria gains vLLM as an engine an entry can declare (`backend = "vllm"`), so the
DGX Spark's vLLM serving moves from `~/spark-setup/vllm.sh` (a hand-written
Python lifecycle manager) into cria: start, stop, status, logs, validate and
bench, with the same guarantees every other engine has.

Origin: user request 2026-09-24, after running vLLM 0.30.0 by script on dgx.local
(Qwen3.8-27B NVFP4, MTP2, 256K context). The probes below were run live that day
and bind this plan.

## Goal

- An entry with `backend = "vllm"` serves on dgx through cria exactly as a llama
  entry does: `cria start <id> --wait`, `cria stop`, `cria validate`, `cria
  bench`, the TUI's box and log tail.
- Stopping a vLLM server never leaks its engine process, whether or not the
  grace period runs out.
- `vllm.sh` / `manage.py` retire on dgx: nothing they do is left undone.

## Probe findings that decide things (2026-09-24, vLLM 0.30.0, dgx.local)

- **vLLM is a two-process server.** `vllm serve` (the API server, what cria
  spawns) starts a `VLLM::EngineCore` child that holds the weights and KV cache
  (~84 GB on this config). Both sit in the process group of the spawned pid.
- **SIGTERM is clean and fast**: API server and EngineCore both gone in ~1 s,
  memory fully returned. cria's 10 s grace is enough.
- **SIGKILL on the API server pid alone orphans EngineCore**: 20 s later it was
  still alive, reparented to init, ~84 GB held, no record pointing at it.
  SIGTERM to the process group cleared it in 8 s. cria's escalation path is
  therefore a real leak for vLLM today.
- **vLLM opens its port only after the engine is ready**: `/health` is the
  readiness signal; there is nothing to warm (not lazy, unlike mlx).
- **As installed, `nvcc` on `PATH` is required at startup** (vLLM declares
  FlashInfer unavailable without it unless `flashinfer_cubin` is installed,
  `vllm/utils/flashinfer.py:104`), and cria inherits whatever environment
  launched it — a non-login `ssh dgx cria …` has no nvcc. **Installing
  FlashInfer's prebuilt kernels removes the requirement entirely** (STEP-1:
  cold start with no nvcc anywhere, green in ~356 s vs ~510 s, same kernels,
  no runtime JIT under parallel 23K-token load).
- The model is read from the standard Hugging Face cache (`HF_HOME` default):
  the cache stays the single source of truth; `/v1/models` names the repo.
- `/metrics` (Prometheus) publishes `vllm:num_requests_running` and
  `vllm:kv_cache_usage_perc` — a candidate slots signal, out of scope here.

## Rulings settled at plan review (2026-09-24, user)

1. **No GPU-competition check.** `manage.py` refuses to start while another GPU
   process runs; cria will not. Which servers share a machine is the user's call,
   the same as running two llama-servers on different ports. Not backlogged.
2. **The dgx install is not a constraint.** If a different install makes vLLM
   simpler to run under cria, the install changes (STEP-1 decides).
3. **No `env` table** (settled 2026-09-24 by STEP-1's experiment): the install
   owns its runtime needs — dgx gains `flashinfer-cubin` + `flashinfer-jit-cache`
   — and cria finds vLLM by an absolute `[tools] vllm` path, so neither the
   program nor its kernels depend on how cria was launched. The table was
   acceptable to the user; it was dropped because nothing needs it.

4. **Backend install guidance is a doc** (settled 2026-09-24, user):
   `docs/BACKENDS.md`, visible on GitHub so people prepare a machine instead of
   assuming cria makes backends work; cria's missing-tool fix lines point at
   it. Rejected: a `cria --backends` command (see STEP-3).

## Scope

- `internal/engine`: the vllm engine (program, model args, health, not lazy, no
  slots signal).
- `internal/tools` + `config.toml [tools]`: `vllm` detection, presence-only.
- `docs/BACKENDS.md`: install recipes for every tool cria drives, linked from
  the README and from the tool check's missing-tool fix lines (ruling 4).
- `internal/config`: `vllm` as a backend (schema examples, `cria docs`, `cria new`).
- `internal/procs` + `internal/serve`: stop signals the spawned server's process
  group.
- `internal/tui`: the backend's color; nothing else is expected to need a branch.
- Specs: TOOLS.md, CONFIG.md, SERVE.md, updated in the same edits.
- dgx: install shape, the tree's vLLM entry and engine file, retiring `vllm.sh`.

## Out of scope

- Slots / concurrency visibility from `/metrics` — joins the existing slots
  backlog entry as a second engine's signal.
- vLLM under the router, or any multi-model vLLM mode.
- Docker/NGC containers (rejected: a container is not a detached process cria
  can identify, signal and record).
- GPU-competition checks (ruling 1).

## Constraints & risks

- **No backwards compatibility** — new backend, new keys; nothing to migrate.
- **Group signalling touches every engine's stop.** Only records cria spawned
  are signalled by group (they are session leaders, so pgid = pid); a foreign
  port holder the TUI offers to kill stays pid-only, because cria does not know
  it leads a group. llama-server's router children share its group — the step
  verifies the router still stops cleanly (expected: better, not worse).
- **Dependencies**: none new in Go.
- **Serving-machine care**: dgx serves the live vLLM model; every live step there
  is announced and restores the serving state it found. The dev Mac's own model
  session is untouched (linux-only engine; the suite is fake-backed).
- The first cold start of a new vLLM config compiles for minutes (~8 min here):
  `--wait` has no start budget today, which is correct; the TUI must read that
  period as `starting`/`downloading`, never stuck.

## Phases and steps

**Phase 1 — settle the install** (step 1; no cria code). Done 2026-09-24.

- STEP-1 — install experiment on dgx: prebuilt FlashInfer kernels remove the
  `nvcc` requirement; ruling recorded (no `env` table, STEP-5 dropped).

**Phase 2 — the engine** (steps 2–4).

- STEP-2 — stop signals the process group of servers cria spawned. SERVE.md Stop.
- STEP-3 — `vllm` in the tool check and `[tools]`; `docs/BACKENDS.md` and the
  fix lines that link it. TOOLS.md, CONFIG.md, README.
- STEP-4 — the vllm engine and backend: engine, schema, `cria docs`, `cria new`,
  TUI color. CONFIG.md, SERVE.md.
  Phase end: suite green, committed.
- (STEP-5, the `env` table, was dropped by STEP-1's ruling; the number is not
  reused.)

**Phase 3 — live on dgx** (step 6).

- STEP-6 — the dgx tree gains the vLLM entry (+ engine file), cria replaces
  `vllm.sh`: start/wait, validate, bench, stop and kill, TUI. Plan end: branch
  rebased, ff-merged, worktree pruned, release cut for dgx.

## End-to-end verification

- Phase 2: full suite green; `cria docs` shows the vllm backend and example.
- Phase 3, on dgx with a cria built from the branch, launched from a
  **non-login** ssh command (the environment that failed in the probe):
  `cria start <vllm-entry> --wait` green; one completion answers; `cria
  validate` passes and restores; `cria bench` reports; `cria stop` and the TUI
  kill both leave no `VLLM::EngineCore` and return the memory; `cria status`
  and the TUI box agree throughout. The llama entries on dgx still start and
  stop unchanged.
