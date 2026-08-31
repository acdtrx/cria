# TOOLS — detection and degradation

cria orchestrates tools the host already has and installs nothing (`docs/cria.md`,
principle 1). This spec owns how tools are found, verified and reported, and what
each one's absence disables.

## Detection (settled 2026-08-18)

- Each managed tool — `llama-server`, `mlx_lm.server`, `hf` — resolves via the
  `config.toml` `[tools]` override first, `PATH` lookup otherwise
  (`docs/specs/CONFIG.md`). The check runs at every cria invocation and reports:
  found (with the resolved path) or missing (with what that disables).
- `ps` and `lsof` ship with the OS and are assumed present; their absence degrades
  only foreign-server detection and port attribution.

## Per-tool contract

- **`llama-server`** — missing: llama entries and the router stay visible but
  unstartable, marked with the reason (one program serves both, so a binary cria
  cannot use disables both). Present: the cache check below must pass for llama
  serving to be enabled, and the router mode check for the router.
- **`mlx_lm.server`** — missing: mlx entries stay visible but unstartable; normal
  on non-Apple hosts and reported without alarm.
- **`hf`** — cria never execs it in v1: its job is authentication
  (`hf auth login`); cria reads the resulting token (`HF_TOKEN` env var first,
  else the token file under the huggingface home) and exports it to servers it
  launches. Missing `hf` is advisory: gated repos will fail to fetch until the
  user authenticates.

## The llama-server cache check (settled 2026-08-18)

- Verified by querying `llama-server --version` and comparing against the first
  build that stores `-hf` downloads in the standard hub cache: **b8498**
  (2026-03-24), the release of PR
  [ggml-org/llama.cpp#20775](https://github.com/ggml-org/llama.cpp/pull/20775)
  "common : add standard Hugging Face cache support" — older builds kept a
  private `~/.cache/llama.cpp` (pinned 2026-08-18).
- A too-old build **disables llama serving entirely**, not just a warning: with a
  private download cache, launching by Hub reference would put bytes where cria's
  cache view, download progress and surgery cannot see them — breaking the
  single-source-of-truth principle. The report names the fix: upgrade llama.cpp.
- An unverifiable build still disables llama serving — loud-and-absent over
  silent-and-plausible (CODING-RULES §4) — but the report distinguishes the
  three ways of not knowing (amended 2026-08-18, after a busy machine's killed
  probe was answered with "upgrade llama.cpp" about a current build): a probe
  that could not *run* retries once and then advises retrying, not upgrading; a
  probe that ran but printed no recognizable build advises checking the banner
  by hand; only a build that was actually read and is actually old gets the
  upgrade advice.

## The llama-server router mode check (settled 2026-08-31)

- Router mode is a second question about the same binary: cria reads
  `llama-server --help` for `--models-preset`, the flag that turns it into a
  router. A build that does not name it predates router mode and is refused for
  the router alone — llama entries still serve fine on it — with what it lacks and
  the upgrade that clears it.
- **The binary's own help, not a build threshold**: the question is whether *this*
  binary takes the flag, and it is the one that can answer. The hub-cache check
  above keeps its build number because that one is about behavior a flag does not
  reveal.
- The probe runs once per invocation and only for a llama-server cria may
  otherwise use: a binary already refused is refused for the router too, and asking
  it a second question would learn nothing while costing every invocation an exec.
- The router's verdict is **derived** from the llama-server finding rather than
  listed beside it: one program to install, one row to read.

## Degradation principle

Absence disables features, never hides declared config: entries of a missing
backend remain listed, marked unstartable with the reason. The tool check's
findings surface in the TUI (display details are a `docs/specs/TUI.md` open item)
and a missing tool named at the moment of a refused start (`docs/specs/SERVE.md`).
