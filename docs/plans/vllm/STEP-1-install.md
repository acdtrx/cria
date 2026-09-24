# STEP 1 — settle the install: can vLLM run without nvcc on PATH?

Status: done (2026-09-24) — yes, with FlashInfer's prebuilt kernels; ruling below

## Intent

One confined experiment on dgx, no cria code: decide whether cria must carry an
environment for vLLM (an `env` table, STEP-5) or whether the install can make the
inherited environment irrelevant.

## Probe record (2026-09-24, dgx.local, vLLM 0.30.0, torch 2.13.0+cu130)

All runs: the production Qwen3.8-27B NVFP4 argv, spawned detached (`setsid`),
kernel caches (`~/.cache/vllm`, `~/.cache/flashinfer`, `~/.triton`) moved aside
for a fully cold start, and restored afterwards.

| install | PATH | result |
|---|---|---|
| current venv (flashinfer-python only) | non-login (no `/usr/local/cuda/bin`) | **crash** at ~270 s: `FlashInfer backend is not available` |
| current venv | login (`/usr/local/cuda/bin` present), no `CUDA_HOME`, no venv `bin/` | green ~510 s, answers |
| + `flashinfer-cubin` + `flashinfer-jit-cache` | non-login, no nvcc anywhere | **green ~356 s**, answers; same kernels selected (FlashInfer attention, xqa decode, CUTLASS NVFP4 GEMM, GDN); two parallel 23K-token thinking requests clean; no runtime JIT in the log |

Mechanism of the crash: `vllm/utils/flashinfer.py:104` — `has_flashinfer()` is
false when `flashinfer_cubin` is not installed and `shutil.which("nvcc")` finds
nothing; this model's decode path then hits the `_missing` fallback. It is a
startup check, independent of cache warmth.

Packages that fix it (versions must match `flashinfer-python`):

- `flashinfer-cubin==0.6.18.post1` — `https://flashinfer.ai/whl/flashinfer-cubin/`
  (PyPI stops at 0.6.13)
- `flashinfer-jit-cache==0.6.18.post1+cu130` — `https://flashinfer.ai/whl/cu130/flashinfer-jit-cache/`
  (generic aarch64 wheel; an `sm121a`-only index exists but carries no 0.6.18)

## Ruling (settled 2026-09-24)

- **cria carries no environment for vLLM. STEP-5 (`env` table) is dropped.**
  The install owns its runtime needs; a correct install runs from any
  environment cria is launched in. (Features earn their place: the one need the
  table would have served is gone.)
- **The dgx install gains the two prebuilt FlashInfer packages**, pinned to the
  installed `flashinfer-python`. Side benefit: cold start ~30% faster.
- **cria finds vLLM by `[tools] vllm = "<abs path>"`** on dgx. `~/.local/bin` is
  on `PATH` only for login shells there, so an agent's non-login
  `ssh dgx cria …` would not find a PATH-installed `vllm`; the absolute
  override makes the invocation environment irrelevant for the program too.
- Install shape (the user's choice, STEP-6 prep): keep the pinned venv and add
  the two packages, or reinstall as a `uv tool` with them — cria needs only the
  absolute path of the resulting `vllm`. The uv-tool form was not itself run.

## Files likely touched

- This file and OVERVIEW.md. No repo code; the test venv was deleted afterwards.

## Acceptance criteria

- [x] Cold start with no nvcc on PATH goes green and answers.
- [x] Kernel selection unchanged versus the current install.
- [x] Parallel long-context load shows no runtime JIT.
- [x] dgx restored: original caches, the user's vLLM serving again.
