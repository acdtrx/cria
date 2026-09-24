# Backends — installing the servers cria drives

cria never installs anything. It starts, watches and stops programs the host
already has, and its tool check (press `t` in the TUI) reports each one as found
or missing, with what a missing one disables. That keeps cria a single binary
with no package manager inside it, and keeps each backend's install in the hands
of whoever runs the machine.

This guide is the other half: one recipe per program, so a new machine is
prepared on purpose. You need only the backends you serve with.

Every recipe ends the same way:

- If the program is not on the `PATH` cria runs with, set its absolute path in
  `~/.config/cria/config.toml` under `[tools]` (see `cria docs`).
- Confirm with `t` in the TUI: the tool shows its resolved path and no `fix` line.

Versions below marked "verified" are what was tested, and when — not
requirements. The only hard requirements are the ones cria enforces, and the
tool check names them.

## llama-server

llama.cpp's server. Serves `llama` entries, and the router.

Hard requirements (the tool check refuses otherwise):

- **Build 8498 or newer.** Older builds download models into a private
  `~/.cache/llama.cpp` that cria cannot see, instead of the Hugging Face cache.
  Check with `llama-server --version` — it prints `(build NNNN, …)`.
- **Router mode** needs a build whose `llama-server --help` lists
  `--models-preset`. Without it, llama entries still serve; only the router is
  refused.

macOS (Apple silicon):

```sh
brew install llama.cpp
```

Homebrew tracks upstream closely, so `brew upgrade llama.cpp` is the fix for an
outdated build.

Linux with an NVIDIA GPU — build from source with CUDA:

```sh
git clone https://github.com/ggml-org/llama.cpp ~/opt/llama.cpp
cmake -S ~/opt/llama.cpp -B ~/opt/llama.cpp/build -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DGGML_CUDA=ON -DCMAKE_CUDA_ARCHITECTURES=121 \
  -DCMAKE_CUDA_COMPILER=/usr/local/cuda/bin/nvcc \
  -DCUDAToolkit_ROOT=/usr/local/cuda \
  -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF
cmake --build ~/opt/llama.cpp/build --target llama-server -j
```

- `CMAKE_CUDA_ARCHITECTURES` is your GPU's compute capability without the dot
  (`121` is the DGX Spark's GB10; `native` builds for the GPU in the machine).
- Clone with **full git history** — no `--depth 1`. The build number is counted
  from git history; a shallow clone reports a low build and cria refuses it as
  outdated.
- Name the CUDA compiler and toolkit root explicitly, so headers and libraries
  from two CUDA installs never mix.
- Homebrew on Linux is not a shortcut here: its llama.cpp builds for Vulkan /
  OpenBLAS, not CUDA (checked 2026-09-14).
- Updating is `git pull` plus the build command again; stop servers using the
  build first, or build into a separate directory.

Needs `cmake`, `ninja` and the CUDA toolkit (`nvcc`) at build time only.

If not on PATH:

```toml
[tools]
llama_server = "/home/me/opt/llama.cpp/build/bin/llama-server"
```

## mlx_lm.server

mlx-lm's server. Serves `mlx` entries. Apple silicon only — its absence
elsewhere is normal and the tool check says so without alarm.

```sh
uv tool install mlx-lm
```

- `uv tool` puts `mlx_lm.server` in `~/.local/bin`; make sure that is on `PATH`,
  or set the path below.
- `pipx install mlx-lm` works the same way. Avoid installing into the system
  Python.
- cria depends on no particular mlx-lm version. Upgrade with
  `uv tool upgrade mlx-lm`.

If not on PATH:

```toml
[tools]
mlx_lm_server = "/Users/me/.local/bin/mlx_lm.server"
```

## vllm

vLLM's server. Serves `vllm` entries. For Linux hosts with an NVIDIA GPU; its
absence on a Mac is normal.

cria passes vLLM no environment of its own: a correct install runs from whatever
environment cria is started in, including a non-login SSH shell with no CUDA
tools on `PATH`. The recipe below is built for that.

Install into a pinned virtual environment with [uv](https://docs.astral.sh/uv/):

```sh
uv venv ~/vllm/.venv
uv pip install --python ~/vllm/.venv/bin/python "vllm==<version>" --torch-backend cu130
```

- `--torch-backend cu130` picks the PyTorch build for CUDA 13.0; match it to the
  CUDA your driver supports.
- Pin the vLLM version. Upgrades are deliberate: re-run the whole recipe,
  FlashInfer step included.

Then add FlashInfer's prebuilt kernels, matched to the `flashinfer-python`
version vLLM pulled in:

```sh
uv pip show --python ~/vllm/.venv/bin/python flashinfer-python   # read Version, e.g. 0.6.18.post1
uv pip install --python ~/vllm/.venv/bin/python \
  flashinfer-cubin==0.6.18.post1 "flashinfer-jit-cache==0.6.18.post1+cu130" \
  --find-links https://flashinfer.ai/whl/flashinfer-cubin/ \
  --find-links https://flashinfer.ai/whl/cu130/flashinfer-jit-cache/
```

Why this step matters:

- Without `flashinfer-cubin`, vLLM treats FlashInfer as unavailable unless it
  finds `nvcc` on `PATH`. Models whose kernels need FlashInfer then crash at
  startup — from cria, from cron, from any non-login shell.
- With both packages, no CUDA compiler is needed at runtime, and cold start is
  faster (about 30% in the verified run below), since nothing is compiled on
  first load.
- The versions must equal the installed `flashinfer-python` exactly, and the
  jit-cache suffix must match the torch backend (`+cu130`).
- Use FlashInfer's own index, as above: PyPI's `flashinfer-cubin` lags behind.

Set an **absolute** path, even if `vllm` is on your `PATH`: `~/.local/bin` and
virtual environments are often on `PATH` only in login shells, so an agent's
`ssh host cria …` would not find it.

```toml
[tools]
vllm = "/home/me/vllm/.venv/bin/vllm"
```

Confirm: the tool check shows it found (cria checks presence only — it never
runs `vllm --version`, which imports torch and takes seconds). To confirm the
install itself, run `~/vllm/.venv/bin/vllm --version` once by hand.

Verified 2026-09-24 on DGX Spark: vllm 0.30.0, torch 2.13.0+cu130, flashinfer
0.6.18.post1 — started cold with no `nvcc` on `PATH`.

## hf

The Hugging Face CLI. cria never runs it; its one job is `hf auth login`, which
stores the token cria passes to servers so gated repos download. Without it,
everything else works and gated repos fail to fetch.

macOS:

```sh
brew install hf
```

Anywhere:

```sh
uv tool install huggingface_hub
```

Then, once per machine, if you use gated repos:

```sh
hf auth login
```

cria also honours `HF_TOKEN` in its environment, ahead of the stored token.

If not on PATH:

```toml
[tools]
hf = "/home/me/.local/bin/hf"
```
