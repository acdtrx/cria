# Tech Stack

> Decisions and the reasoning behind them. Specs reference this file for anything
> stack-concrete. When the project deviates later, edit the section in place and date
> the ruling (`(settled YYYY-MM-DD)`, see `CLAUDE.md`, Documentation) — this file
> describes the project's actual stack, not a menu.
>
> This is the **only** doc that names libraries and frameworks — no spec file names a
> specific technology unless it is inherent to the domain. As dependencies accumulate,
> grow the dependency inventory below: every dependency, what it is for, why it was
> chosen.

## Language & runtime (settled 2026-08-18)

- **Go, latest stable toolchain. One module, one static binary, no cgo.** Everything
  cria does is exec + filesystem + HTTP, and the Go standard library covers all three.
  A static binary is the whole distribution story: build on one Mac, `scp` to another,
  run — the Go toolchain ad-hoc-signs darwin/arm64 binaries and scp sets no quarantine
  xattr, so no developer certificate is needed.
- Primary target `darwin/arm64`. `linux/amd64` must keep **compiling** (a friend or a
  future Linux box — `docs/BACKLOG.md`) but is untested and unsupported for now; any
  platform-specific code goes behind a build-tagged seam.
- Rejected: **Rust + ratatui** — same single-binary result, slower iteration, no
  capability this project needs that Go lacks. **Node (Ink or webapp)** — the
  predecessor llama-runner was a Node webapp; a runtime dependency on every target
  host defeats copy-a-binary distribution.

## TUI (settled 2026-08-18)

- **bubbletea v2** with its companions **bubbles v2** (widgets) and **lipgloss v2**
  (styling) — the maintained de-facto standard for Go TUIs; the Elm-style
  model/update/view loop composes well as screens accumulate. The v2 line is current
  and stable (verified 2026-08-18; note the `charm.land/...` import paths). Exact
  versions are whatever `go get` resolves as latest stable at scaffold time
  (CODING-RULES §3).
- The binary carries subcommands besides the TUI: `cria new <model>` scaffolds a model
  folder, `cria docs` prints the config schema. Whether lifecycle operations
  (start/stop) also become subcommands is an open question in `docs/cria.md`.

## Configuration: TOML, read-only (settled 2026-08-18)

- Config tree at `~/.config/cria/` — the **same path on macOS and Linux** (one set of
  docs and muscle memory; rejected: platform-native dirs like
  `~/Library/Application Support`).
- **TOML**, parsed with **pelletier/go-toml/v2** (actively maintained, strict decoding).
  TOML over YAML/JSON: comments survive in scaffolded files, no indentation traps, and
  deleting an unneeded key degrades gracefully — the tree is meant to be edited by
  hand and by coding agents.
- cria **reads** config; it never rewrites a config file. New files come only from
  `cria new`, which scaffolds commented templates embedded in the binary (`go:embed`).
- Config hygiene: unknown keys or wrong types are rejected loudly at load — a typo
  must fail, never silently default.

## Runtime state (settled 2026-08-18)

- `~/.local/state/cria/` — pidfiles, one JSON record per managed server (model,
  profile, resolved flags, port, log path, start time), and the server log files.
- State records are how the TUI re-attaches after restart: scan records, verify the
  pid is alive and the port answers, flag the rest stale. `encoding/json` — this is
  machine-owned state; comments are not needed and cria owns the format.

## External tools (the contract)

cria orchestrates tools the host already has; it installs nothing.

- **`hf`** — Hub auth (`hf auth login`); cria reads the token file and exports
  `HF_TOKEN` to servers it launches so gated repos work. cria moves no model bytes
  itself — servers fetch their own models (below), and cache surgery is direct
  filesystem work.
- **`llama-server`** (llama.cpp) — GGUF serving, launched by Hub reference
  (`-hf org/repo:QUANT`); it fetches missing models into the standard HF hub cache
  (2026+ behavior — older builds kept a private `~/.cache/llama.cpp`, which the tool
  check must flag). Status via its documented HTTP endpoints, never its log format.
- **`mlx_lm.server`** (mlx-lm) — MLX serving, Apple silicon only, launched with
  `--model org/repo`; fetches via huggingface_hub into the same cache.
- Each found on `PATH`, overridable in `config.toml`. Missing tools degrade features
  and are reported; `mlx_lm.server` absent on Linux is normal, not an error.

## HTTP

- **`net/http`** only — server health checks and the Hugging Face Hub API (repo file
  listings with sizes, for browsing quants before downloading). No HTTP client
  library.

## Testing (lean)

- Built-in `go test`; table tests for cache-walking and config-parsing logic — that is
  where the risk lives. TUI-level testing: decide when the UI takes shape.

## Dependency inventory

- `charm.land/bubbletea/v2` — TUI framework. Maintained standard, Elm architecture.
- `charm.land/bubbles/v2` — TUI widgets (lists, tables, spinners). First-party
  companion to bubbletea.
- `charm.land/lipgloss/v2` — terminal styling and layout. First-party companion.
- `github.com/pelletier/go-toml/v2` — TOML parsing. Maintained, strict decoding
  supports the config-hygiene rule.

Everything else is standard library. A new dependency needs a yes from the user first
(`CLAUDE.md`, Project Facts), enters at its latest stable version verified maintained
(CODING-RULES §3), and gets a line here.
