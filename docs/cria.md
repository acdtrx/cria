# cria — Philosophy

*A cría is a baby llama: something you raise, feed, and keep track of.*

cria is a single-binary TUI that manages local LLM serving on one host: it starts,
watches and stops llama.cpp's `llama-server` and mlx-lm's `mlx_lm.server`, and it
manages the models they serve — downloads, quant-by-quant disk usage, and the cache
surgery the `hf` CLI cannot do. It succeeds llama-runner (a Node webapp with the same
goal) and inherits exactly one lesson from it: stay simple, and parse nothing you
don't own.

## Principles

1. **Manage the tools you already have; never replace them.** The host provides
   `llama-server`, `mlx_lm.server` and `hf`; cria orchestrates them. No bundled
   runtimes, no private model registry, no downloader of its own. A missing tool
   disables its features and is reported — nothing gets installed.
2. **The Hugging Face cache is the single source of truth** (settled 2026-08-18).
   Every model on disk lives in the HF hub cache, and servers launch **by Hub
   reference** (`-hf org/repo:QUANT` for llama-server, `--model org/repo` for
   mlx_lm.server), fetching anything missing into that same cache themselves — cria
   moves no model bytes of its own, so an agent can write a model + profile and the
   first start does the rest. This relies on 2026+ llama.cpp, which stores `-hf`
   downloads in the standard hub cache (older builds kept a private
   `~/.cache/llama.cpp`; the tool check must flag those). cria adds the one operation
   the ecosystem lacks: deleting a single quant from a multi-quant GGUF repo
   (snapshot symlink + blob, minding blobs shared across snapshots, sharded
   `-NNNNN-of-NNNNN` files, and `.incomplete` leftovers). The MLX side is asymmetric
   by nature — each MLX quant is its own repo, so repo-level deletion already works
   there; cria's job is making both sides visible in one place with true disk sizes.
3. **Servers outlive the TUI; there is no daemon** (settled 2026-08-18). cria spawns
   servers detached, records them in runtime state, and re-attaches on next launch.
   The TUI exists to start, check and stop — closing it stops nothing.
4. **The config tree is the interface** (settled 2026-08-18). A folder per model
   holding a `model.toml` and one TOML file per launch profile. The TUI drives what
   the tree declares and invents no state of its own. Editing config is a text-editor
   or coding-agent activity; `cria new <model>` scaffolds the folder with commented
   templates so there is always something to copy and trim.
5. **Agent-operable by design** (settled 2026-08-18). The expected way a new profile
   gets written is asking a coding agent to derive one from the model provider's
   recommended parameters. So the binary documents itself: `cria docs` prints the
   config schema from the same definitions the parser uses — the docs cannot drift
   from the binary — and the config root carries an `AGENTS.md` pointing agents at it.
6. **Status over telemetry** (settled 2026-08-18). cria reports what stable
   interfaces expose: process alive, port answering, health endpoint green, raw log
   tail. It never parses log streams for stats — that was llama-runner's mistake, and
   it broke on every llama.cpp release. Richer telemetry waits for a maintained
   upstream API (`docs/BACKLOG.md`).
7. **One host, one binary** (settled 2026-08-18). cria manages the machine it runs
   on; the remote story is SSH-ing there and running it. macOS (two Macs) is the
   lived-on platform; Linux keeps compiling but earns support only when someone
   actually runs it.

## v1 surface (the seed scope)

- **Models view** — every model in the HF cache with its quants and true disk sizes
  (direct cache walk; per-file granularity is the point).
- **Pick a quant** — given a repo, list its files with sizes via the Hub API; the
  pick lands in config (via `cria new` or an agent edit). Nothing is fetched until
  first start.
- **Cache surgery** — delete a single GGUF quant; delete an MLX repo; report the
  space reclaimed.
- **Serve** — pick model + profile, start detached, see status (pid, port, health,
  uptime), tail the raw log, stop. A first start fetches the model; cria shows a
  distinct *downloading* state, rendering progress from on-disk cache bytes versus
  Hub-API sizes — filesystem observation, never parsing anyone's output.
- **Scaffolding** — `cria new <model>` creates the model folder with a commented
  `model.toml` and a starter profile; `cria docs` prints the schema.
- **Tool check** — on start, report which of `hf` / `llama-server` / `mlx_lm.server`
  are present, which features their absence disables, and whether `llama-server` is
  recent enough to share the hub cache.

## Anti-goals

Not a daemon, not a proxy or auto-swapper (llama-swap exists), not a model registry
(ollama exists), not a chat client, not a metrics dashboard. Features earn their
place (`CLAUDE.md`, Scope) — this list is the standing reminder.

## Open questions for the first planning session

- Simultaneous servers (llama + mlx side by side seems obviously wanted) and port
  allocation: fixed per profile, or a managed range with collision checks?
- `cria new` exact UX: how backend and repo are given, what the starter profile
  contains, how a second profile is added (`cria new <model> <profile>`?).
- How MLX models are found and browsed (search `mlx-community`? paste a repo id?).
- State-record details: staleness detection, crash cleanup, whether logs rotate.
- Whether start/stop also become plain subcommands (scriptability vs surface creep).
