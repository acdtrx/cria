# Tech Stack

> What cria is built with, and why: a Go binary, here a local tool (a TUI plus scriptable
> subcommands). When the project deviates, edit the section in place (`AGENTS.md`,
> Documentation): this file describes the project's actual stack, not a menu.

## Language & runtime (settled 2026-08-18)

- **Go current stable, one module, one static cgo-free binary**, so it cross-compiles
  with a `GOOS`/`GOARCH` pair. Add only dependencies that keep it static and cgo-free.
- **Why Go**: everything cria does is exec, the filesystem and HTTP, and the standard
  library covers all three. A static binary is the whole distribution story: build on one
  Mac, copy to another, run (`AGENTS.md`, Deployability). Rejected: **Rust + ratatui**
  (the same single-binary result, slower iteration, nothing this project needs that Go
  lacks); **Node, Ink or a webapp** (a runtime on every host defeats copy-a-binary
  distribution; the predecessor, llama-runner, was a Node webapp).
- **Standard library first**: `net/http`, `encoding/json`, `flag`, `embed`.
- The binary carries its version: `var version = "dev"` in `main`, stamped at build with
  `-ldflags "-X main.version=…"`.
- **Platforms**: `darwin/arm64` is the one lived on. `linux/amd64` and `linux/arm64`
  keep compiling and ship as release builds, but are untested and unsupported
  (`docs/BACKLOG.md`, Platform & distribution).

## TUI (settled 2026-08-18)

- **bubbletea v2**, with its companions **bubbles v2** (widgets) and **lipgloss v2**
  (styling): the maintained standard for Go TUIs, and its Elm-style model/update/view
  loop composes well as screens accumulate. Its import paths are `charm.land/...`.

## Configuration format & CLI (settled 2026-08-18)

- **TOML** via **pelletier/go-toml/v2** with strict decoding, for the config tree
  (`docs/specs/CONFIG.md`): people and coding agents edit it by hand, comments survive,
  there are no indentation traps, and deleting an unneeded key degrades gracefully.
  Rejected: YAML and JSON for the tree.
- **JSON** (`encoding/json`) for cria's own state: machine-owned, so comments aren't
  needed and cria owns the format (`docs/ARCHITECTURE.md`, State).
- CLI: stdlib `flag`, subcommands dispatched by hand, no CLI framework. Bare `cria` opens
  the TUI; the subcommands are `docs/specs/CLI.md`.

## External tools (the contract)

cria orchestrates tools the host already has; it installs nothing (`docs/BACKENDS.md`
says how to install each).

- **`hf`**: Hub auth (`hf auth login`). cria reads the token file and exports `HF_TOKEN`
  to the servers it launches, so gated repos work. cria moves no model bytes itself:
  servers fetch their own models, and cache surgery is direct filesystem work.
- **`llama-server`** (llama.cpp): GGUF serving, launched by Hub reference
  (`-hf org/repo:QUANT`); it fetches missing models into the standard HF hub cache. Older
  builds kept a private `~/.cache/llama.cpp`, which the tool check flags.
- **`mlx_lm.server`** (mlx-lm): MLX serving, Apple silicon only, launched with
  `--model org/repo`; fetches via huggingface_hub into the same cache.
- **`vllm`** (vLLM): safetensors serving on NVIDIA GPU hosts, launched as
  `vllm serve --model org/repo`; fetches via huggingface_hub into the same cache. Its
  install carries FlashInfer's prebuilt kernels, so it runs without `nvcc` on `PATH`
  (settled 2026-09-24, `docs/BACKENDS.md`). Rejected: Docker or NGC containers, since a
  container is not a detached process cria can identify, signal and record.
- **`ps` / `lsof`** (settled 2026-08-18): foreign-process detection, finding the servers
  cria didn't start and attributing a busy port (pid, command line, working directory,
  an offered kill). Both ship with macOS. Exec them with explicit field selectors
  (`ps -o ...`, `lsof -F`), never parsing the human-format tables. macOS has no
  `/proc`, so exec is the only cgo-free route (CODING-RULES §7).
- Find each on `PATH`, overridable in `config.toml`. A missing tool disables features and
  is reported; `mlx_lm.server` absent on Linux, or `vllm` on a Mac, is normal, not an
  error.

## HTTP

- **`net/http`** only, as a client: server health checks, the router's API, and the
  Hugging Face Hub API (repo file listings with sizes). cria serves no HTTP of its own.

## Build & lint

- Plain `go` commands; no Makefile.
- **Lint**: `gofmt -l` (fail on output) and `go vet ./...`, as CI runs them
  (`.github/workflows/ci.yml`).
- **Releases**: a v-prefixed tag pushed to GitHub runs `.github/workflows/release.yml`,
  which tests and builds the release binaries (`AGENTS.md`, Deployability).

## Testing (lean)

- `go test ./...`; **table tests** where the risk lives: the cache walk, config parsing,
  command composition.
- The TUI is tested by driving its model's update loop against in-repo fakes of the
  lifecycle; the subcommands, through their dispatcher.
- Tests that drive real processes skip under `-short`, which CI uses off macOS.

## Dependencies

Rules: `CODING-RULES.md` §3. Each line: what it is for, and what it saves.

### Inventory

- `charm.land/bubbletea/v2`: the TUI framework. The maintained standard, Elm
  architecture.
- `charm.land/bubbles/v2`: TUI widgets (lists, tables, spinners). bubbletea's
  first-party companion.
- `charm.land/lipgloss/v2`: terminal styling and layout. First-party companion.
- `github.com/pelletier/go-toml/v2`: TOML parsing. Maintained; its strict decoding is
  what makes config hygiene one setting (CODING-RULES §4).

Everything else is the standard library.
