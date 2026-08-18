# cria

*A cría is a baby llama: something you raise, feed, and keep track of.*

cria is a single-binary TUI for serving local LLMs on one machine. It starts,
watches and stops llama.cpp's `llama-server` and mlx-lm's `mlx_lm.server`, driven
by a plain TOML config tree meant to be written by humans and coding agents
alike — and it shows the Hugging Face cache as it really is on disk, down to
deleting a single quant from a multi-quant GGUF repo, which nothing else does.

No daemon: servers are spawned detached and outlive the TUI; closing it stops
nothing, and the next launch re-attaches. No downloader and no model registry of
its own: servers fetch their models by Hub reference into the standard Hugging
Face cache, and cria watches the bytes arrive.

## Install

Apple silicon macOS (the lived-on platform), or Linux x64/arm64 (builds provided,
untested):

```sh
curl -fsSL https://raw.githubusercontent.com/acdtrx/cria/main/install.sh | sh
```

Or grab a tarball from the [latest release](https://github.com/acdtrx/cria/releases/latest),
or build from source (Go, no cgo): `git clone … && cd cria && go build -o ~/.local/bin/cria .`

You bring the servers — cria orchestrates and never installs:

- [`llama-server`](https://github.com/ggml-org/llama.cpp) — build b8498 or newer
  (older builds keep a private model cache cria can't see; the tool check tells you)
- [`mlx_lm.server`](https://github.com/ml-explore/mlx-lm) — optional, Apple silicon
- [`hf`](https://huggingface.co/docs/huggingface_hub/en/guides/cli) — only for
  `hf auth login` if you use gated repos

## Quick start

```sh
cria docs                 # the whole config schema, with complete examples
$EDITOR ~/.config/cria/models/qwen3-30b.toml
```

```toml
backend = "llama"
repo = "unsloth/Qwen3-30B-A3B-GGUF"
quant = "UD-Q4_K_XL"       # spelled exactly as the repo names it
port = 8080
args = ["--ctx-size", "16384", "--jinja"]
```

```sh
cria start qwen3-30b --wait   # first start downloads, then serves; exit 0 = healthy
cria status --json            # machine-readable truth
cria                          # the TUI: serve view, cache view, logs, surgery
```

Point any OpenAI-compatible client at `http://localhost:8080/v1`. Swapping models
behind that port is `cria stop` + `cria start <other>` — the endpoint never changes.

## Built for coding agents

The expected way a model entry gets written is asking an agent to derive one from
the model card. Everything supports that loop:

- `cria docs` prints the schema and examples **from the same definitions the
  parser uses** — it cannot drift from the binary; the config root carries an
  `AGENTS.md` pointing agents at it
- `cria start <id> --wait` exits 0 only when the server actually serves (for MLX
  that includes loading the weights); `cria status --json` is the stable machine
  contract; `cria list --paths` locates entry files
- a broken entry file disables only itself, and the error names the file, the
  key, and the fix

## What it deliberately is not

Not a daemon, not a proxy or auto-swapper, not a model registry, not a chat
client, not a metrics dashboard — and it never parses server logs (logs are
shown raw; information comes from documented HTTP endpoints and the filesystem).
The philosophy lives in [docs/cria.md](docs/cria.md); the per-subsystem contracts
in [docs/specs/](docs/specs/).
