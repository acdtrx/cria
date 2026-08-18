# CONFIG — the config tree

The config tree at `~/.config/cria/` is cria's interface (`docs/cria.md`, principle
4): humans and coding agents write it, cria reads and drives it. This spec owns the
tree's shape and the parsing contract. `cria docs` prints this schema from the same
definitions the parser uses — a schema change updates the docs in the same edit by
construction.

## Layout

```
~/.config/cria/
├── AGENTS.md          # scaffolded on first run when missing; points agents at `cria docs`
├── config.toml        # tree-wide settings (file itself optional)
└── models/
    └── <id>.toml      # one launchable entry per file
```

- cria's writes into the tree are create-only and never touch an existing file:
  the root, `models/` and `AGENTS.md` on first run when missing, and the entry
  file `cria new <id>` scaffolds (settled 2026-08-18, reinstated from the
  backlog) — whose content is the schema-rendered example `cria docs` prints,
  from the same definitions the parser uses, so the scaffold cannot drift from
  the binary.
- An entry's **id** is its filename minus `.toml`; ids appear in the TUI and as CLI
  arguments (`cria start <id>`). Allowed: letters, digits, `-`, `_`, `.` — anything
  else is rejected loudly.

## The entry contract (settled 2026-08-18)

One TOML file = one launchable thing: backend + Hub reference + params + port. There
is no model/profile hierarchy — a quant is its own entry, which keeps both backends
symmetric (an MLX quant is its own repo already), and a param variant is simply
another entry file. Rejected: folder-per-model with `model.toml` + profile files —
two files of ceremony for a one-param-set-per-model reality; a profile layer returns
only if entry duplication becomes felt friction (`docs/BACKLOG.md`).

| key       | type     | rules                                                                    |
| --------- | -------- | ------------------------------------------------------------------------ |
| `backend` | string   | required; `"llama"` or `"mlx"`                                            |
| `repo`    | string   | required; Hugging Face repo id (`org/name`)                               |
| `quant`   | string   | llama only (error on mlx); omitted → the server picks the repo's default  |
| `port`    | integer  | optional when `config.toml` sets `default_port`, required otherwise       |
| `host`    | string   | optional; bind address, default `0.0.0.0` (via `config.toml` `default_host` if set) |
| `name`    | string   | optional display name; defaults to the id                                 |
| `args`    | string[] | optional; extra CLI flags passed to the server verbatim                   |

**Args are passthrough, not schema** (settled 2026-08-18). cria types only what it
must understand to do its job — backend, repo, quant, port — and hands `args` to the
server untouched. It never grows typed keys for backend flags: chasing llama.cpp's
flag surface release-by-release is the same trap as parsing its logs (`docs/cria.md`,
principle 6). Rejected: typed per-backend keys (`ctx = 16384`, …) — validatable and
prettier, but every upstream flag change would need a cria release, and
unknown-key-is-error would make new upstream flags unusable until then.

- cria composes the model-reference, port and host flags itself (`-hf repo[:quant]` /
  `--model repo`, `--port N`, `--host A`); `args` restating a cria-owned flag is a
  loud error, never a silent override.
- The bind default is `0.0.0.0` (settled 2026-08-18): servers are reachable from the
  rest of the LAN out of the box — both backends default to loopback on their own,
  so cria always passes the flag. A host that should stay private sets
  `default_host = "127.0.0.1"` or a per-entry `host`. cria probes health on
  loopback when the bind is `0.0.0.0`, on the bound address otherwise.
- Display follows the same rule: the TUI shows `args` and the full composed command
  line verbatim — that *is* the entry's documentation.

## config.toml

| key            | type    | rules                                                              |
| -------------- | ------- | ------------------------------------------------------------------ |
| `default_port` | integer | optional; the port for entries that declare none                   |
| `default_host` | string  | optional; the bind address for entries that declare none; `0.0.0.0` when absent |
| `[tools]`      | table   | optional; `llama_server`, `mlx_lm_server`, `hf` — absolute paths overriding `PATH` lookup |

`default_port` exists because entries are expected to share one port — a stable
endpoint the consuming agent never reconfigures; swapping models is stop-then-start
on the same port (settled 2026-08-18, `docs/cria.md`, v1 surface).

## Parsing contract

- Strict decoding: unknown keys and wrong types are errors, never silent defaults —
  a typo must fail (`docs/TECH-STACK.md`).
- Validation runs at load, before any lifecycle action. An invalid entry is reported
  with its file and offending key, and disables only itself — one broken file never
  bricks the tree.
- `cria docs` output = this schema, a complete commented example entry per backend,
  and a `config.toml` example. The examples are the templates agents copy from.
