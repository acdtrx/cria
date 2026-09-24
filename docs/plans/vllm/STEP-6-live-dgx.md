# STEP 6 — live on dgx: cria replaces vllm.sh

Status: not started

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

## Acceptance criteria

- `cria start <id> --wait` green (cold-cache run optional, warm required); one
  completion answers.
- `cria validate <id>` passes and restores what held the port.
- `cria bench` reports prefill/decode.
- `cria stop` and the TUI kill each leave no `VLLM::EngineCore`; memory returns.
- `cria status` / TUI box track starting → running → stopped.
- A llama entry on dgx still starts and stops unchanged.
- Plan end: suite green, branch rebased on main, ff-merged, worktree pruned,
  release tagged so dgx can `cria update`.

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
