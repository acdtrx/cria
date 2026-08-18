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
4. **The config tree is the interface** (settled 2026-08-18). One TOML file per
   launchable entry — backend, Hub reference, port, passthrough args; a quant is its
   own entry, which keeps the backends symmetric, since an MLX quant is its own repo
   anyway (resettled 2026-08-18 from folder-per-model with profiles: two files of
   ceremony for a one-param-set-per-model reality; `docs/specs/CONFIG.md` owns the
   contract, a profile layer waits in `docs/BACKLOG.md`). The TUI drives what the
   tree declares and invents no serving state of its own; UI preferences live in the
   state dir, never the tree. Editing config is a text-editor or coding-agent
   activity: files are written against `cria docs`, or seeded by `cria new` from
   the same schema-rendered example (dropped from v1 2026-08-18, reinstated
   2026-08-18 when its revisit trigger fired). cria's only writes into the tree
   are creating the root and an `AGENTS.md` on first run, and the create-only
   files `cria new` scaffolds — it never edits an existing file.
5. **Agent-operable by design** (settled 2026-08-18). The expected way a new entry
   gets written is asking a coding agent to derive one from the model provider's
   recommended parameters. So the binary documents itself: `cria docs` prints the
   config schema from the same definitions the parser uses — the docs cannot drift
   from the binary — plus a complete commented example config per backend, and the
   config root carries an `AGENTS.md` pointing agents at it. Lifecycle is scriptable
   for the same reason (settled 2026-08-18): `cria start/stop/status` work without
   the TUI, and `cria status --json` lets an agent verify that an entry it just
   wrote actually serves.
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
  (direct cache walk; per-file granularity is the point). Quant browsing happens
  here, in the cache, for deletion — pre-download repo research is the agent's job,
  on the Hub (settled 2026-08-18, `docs/BACKLOG.md`).
- **Cache surgery** — delete a single GGUF quant; delete an MLX repo; report the
  space reclaimed.
- **Serve** — pick an entry, start detached, see status (pid, port, health,
  uptime), tail the raw log, stop. Ports are fixed in config (per entry or the
  tree-wide default), servers bind `0.0.0.0` unless config says otherwise, and a
  busy port refuses loudly, naming the holder — no auto-swap, swapping is
  stop-then-start (settled 2026-08-18: entries are expected to share one port, one
  model at a time behind a stable endpoint, so the consuming agent's config never
  changes; several servers at once is just entries declaring different ports). A
  first start fetches the model; cria shows a distinct *downloading* state,
  rendering progress from on-disk cache bytes versus Hub-API sizes — filesystem
  observation, never parsing anyone's output.
- **Lifecycle subcommands** — `cria start`, `cria stop`, `cria status` (with
  `--json`) alongside the TUI, sharing the same lifecycle layer (settled 2026-08-18:
  agent validation of freshly written entries is the use case).
- **Foreign servers** — `llama-server` / `mlx_lm.server` processes cria didn't start
  are detected and shown with pid, command line and working directory, with an
  offered kill (settled 2026-08-18) — the forgotten-terminal case, and the answer to
  "who is holding my port".
- **Scaffolding** — first run creates `~/.config/cria/` and drops an `AGENTS.md`
  when missing; `cria docs` prints the schema and full example configs, and entry
  files are written by hand, by an agent, or seeded by `cria new <id>` — which
  writes the same schema-rendered example `cria docs` prints (create-only, never
  overwrites) and opens the editor on it (reinstated 2026-08-18: the backlog's
  revisit trigger — a human onboarding models without an agent — fired).
- **Tool check** — on start, report which of `hf` / `llama-server` / `mlx_lm.server`
  are present, which features their absence disables, and whether `llama-server` is
  recent enough to share the hub cache.

## Anti-goals

Not a daemon, not a proxy or auto-swapper (llama-swap exists), not a model registry
(ollama exists), not a chat client, not a metrics dashboard. Features earn their
place (`CLAUDE.md`, Scope) — this list is the standing reminder.
