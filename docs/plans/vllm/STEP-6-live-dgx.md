# STEP 6 — live on dgx: cria replaces vllm.sh

Status: done (2026-09-24) — every live check passed on dgx; plan-end steps below

## Intent

The real goal, proven on the machine it is for: the Qwen3.8-27B NVFP4 vLLM
config that `manage.py` runs today is served by cria from the tree, and the
script retires.

## Files likely touched

- dgx `~/.config/cria/models/<id>.toml` — the vLLM entry: repo, and args carried
  over from `~/spark-setup/vllm/config.json` (`--revision`, `--dtype`,
  `--max-model-len`, `--max-num-seqs`, `--gpu-memory-utilization`,
  `--kv-cache-dtype`, `--max-num-batched-tokens`, `--generation-config`,
  `--speculative-config`, parsers). Choices where the user wants axes (context,
  MTP on/off) — asked, not assumed.
- dgx install (prep, the user's act): `flashinfer-cubin` + `flashinfer-jit-cache`
  added at the installed `flashinfer-python` version (STEP-1), as the pinned venv
  or a uv tool.
- dgx `~/.config/cria/engines/vllm.toml` — machine-wide vLLM flags, if any
  emerge (only one vLLM entry today, so possibly none).
- dgx `~/.config/cria/config.toml` — `[tools] vllm = "<abs path>"` (STEP-1
  ruling: absolute, so non-login invocations find it).
- The tree is the user's: cria's agent writes these files with the user's
  review, as in the engines plan's extraction session.
- `docs/specs/*` only if the live run overturns a contract.

## Decisions made during planning

- A cria built from the branch (`GOOS=linux GOARCH=arm64`), copied to dgx
  beside the installed one — the installed 0.9.0 is untouched until release.
- Every check runs from a **non-login** ssh command (`ssh dgx.local '<cria> …'`),
  the environment that crashed the unfixed install — plus the TUI once in an
  interactive session.
- The live vLLM on 11434 is stopped by `vllm.sh stop` at the start and restored
  at the end (by cria if the step passes — that *is* the handover).
- `vllm.sh` / `manage.py` retirement is the user's act; this step proves it is
  safe and says what to delete.

## Result (2026-09-24, dgx.local, branch builds up to 93786e6)

Every command ran from a non-login `ssh dgx.local '~/cria-vllm-test …'`
(`PATH` without nvcc or `~/.local/bin`). The tree gained
`models/qwen38-27b-nvfp4.toml` (choices context 256k/128k, mtp 2/off) and
`[tools] vllm`; no `engines/vllm.toml` was needed.

- Foreign detection: with `vllm.sh`'s server on 11434, `cria start` refused,
  naming its pid and full command line (the working directory read
  "unreadable" — a linux lsof bug, fixed in 68fc4c8, see findings).
- `start --wait`: green in 4m31s (warm caches); completion answered `323`.
- `stop`: 0.46 s, record removed, no `VLLM::EngineCore`, memory back to 118 GB
  available. TUI `K`: same result.
- `validate`: displaced the running server, started, proved, stopped, restored
  — exit 0.
- `bench` (after the token-count fix): prefill 803 / 2722 / 2392 t/s and decode
  16.3 / 16.1 / 16.0 t/s at 106 / 4114 / 16666 prompt tokens; no early-end notes.
- `status` and the TUI box tracked starting → running → stopped throughout.
- llama regression: `qwen38-27b` started green in 6 s, answered, stopped clean.
- The run ends with vLLM serving under cria (pid 460322, context=256k mtp=2).

## Acceptance criteria

- [x] `cria start <id> --wait` green (warm); one completion answers.
- [x] `cria validate <id>` passes and restores what held the port.
- [x] `cria bench` reports prefill/decode.
- [x] `cria stop` and the TUI kill each leave no `VLLM::EngineCore`; memory returns.
- [x] `cria status` / TUI box track starting → running → stopped.
- [x] A llama entry on dgx still starts and stops unchanged.
- [ ] Plan end: suite green, branch rebased on main, ff-merged, worktree pruned,
  release tagged so dgx can `cria update` — pushing the release waits on the user.
- Open for the user: retire `~/spark-setup/vllm.sh` / `manage.py`; the
  BACKENDS.md vLLM recipe has not been followed from an empty machine (dgx's
  venv predates it and gained the FlashInfer step by hand).

## Live findings

- **2026-09-24 — `--wait` gave up on a healthy vLLM start.** `cria start
  qwen38-27b-nvfp4 --wait` failed with "it is still starting after 2m0s" while
  the server came up fine: vLLM binds its port only after load, torch.compile,
  CUDA-graph capture and memory profiling. Measured here (27B NVFP4, 256K ctx,
  MTP2): green at ~5m14s with warm kernel caches, ~6–8.5 min cold. The fixed
  2-minute window was llama/mlx knowledge; `cria validate` shares the wait and
  would have failed the same way. Fix: the start window is the engine's answer
  (`Engine.StartWithin`) — 2m for llama, mlx and the router, 15m for vLLM; the
  30m download window is unchanged (`docs/specs/SERVE.md`, Start 4, amended
  2026-09-24). Re-run `start --wait` and `validate` on dgx with the rebuilt
  binary to clear it.
- **2026-09-24 — bench counted chunks as tokens.** `cria bench` against vLLM
  (MTP2) under-reported decode ~2× and printed "the model ended its answer
  early (141 of 256 tokens)" on answers that ran the full length. Proof: a
  `/v1/completions` stream with `max_tokens: 256` delivered 133 content chunks
  while `usage.completion_tokens` was 256 — speculative decoding streams
  several accepted tokens per chunk. Fix: the generated-token count is the
  server's `usage.completion_tokens` (already requested via
  `stream_options.include_usage`); chunks only timestamp TTFT and the decode
  window, the first chunk counted as the prefill's one token
  (`docs/specs/SERVE.md`, Benchmarking, amended 2026-09-24). Re-run `cria
  bench` on dgx with the rebuilt binary to clear it; whether llama-server with
  MTP or a draft model batches tokens per chunk too is unverified.
- **2026-09-24 — linux lsof never reported a working directory.** Every
  foreign-server report on dgx said the directory was "unreadable". lsof-org
  4.99 (linux) emits only `p` and the fields asked for, and the probe asked for
  names alone, so no `fcwd` line preceded the name; macOS's lsof 4.91 adds `f`
  unasked. Fix (68fc4c8): ask for `-F fn`; the procs suite passes on dgx.
- **2026-09-24 — noted, not fixed** (backlog): vLLM's memory reads ~3.6 GiB in
  status while ~85 GB is in use — GPU allocations on the Spark's unified memory
  are in no process's RSS; the router view's key bar omits `K`, which acts
  there because stop/kill are global by design.
