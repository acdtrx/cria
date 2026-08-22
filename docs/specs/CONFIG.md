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
is no model/profile *file* hierarchy — a model run in variations declares them as
`[[choice]]` axes inside its one file (settled 2026-08-22, below; the 2026-08-18
"a variant is just another entry file" ruling grew to 31 flat files, one model
spanning 11 — the backlog trigger fired). Rejected: folder-per-model with
`model.toml` + profile files — two files of ceremony where one file with axes
carries the reality.

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

## Choices — variations inside one entry (settled 2026-08-22)

An entry run in variations — quants, context sizes, slot layouts, feature toggles —
declares them as `[[choice]]` axes instead of duplicating files. Each choice is a
named pick-one axis of options; cria composes the picked options into the launch
and never interprets what their flags mean. Coupling between flags is expressed by
factoring, not by rules: flags that must vary together live inside the same
choice's options (a context size folded into each quant option, say), and cria
knows nothing about which combinations are valid — the author does, in comments
next to the options, where fit measurements already live.

| key                          | type     | rules                                                        |
| ---------------------------- | -------- | ------------------------------------------------------------ |
| `[[choice]]` `name`          | string   | required; unique within the entry; id charset                 |
| `[[choice.option]]` `name`   | string   | required; unique within its choice; id charset                |
| `[[choice.option]]` `quant`  | string   | optional; replaces the entry's `quant`; llama entries only    |
| `[[choice.option]]` `repo`   | string   | optional; replaces the entry's `repo` (an MLX quant is its own repo) |
| `[[choice.option]]` `args`   | string[] | optional; appended to the composed args when the option is picked |

- A choice needs at least one option, and the **first option is the config
  default**. A one-option choice is legal: a named, always-on block of args.
- **Composition**: the entry's `args`, then each choice's picked option's `args`,
  in the file's choice order. The effective `repo`/`quant` are the entry's unless
  a picked option replaces them.
- **Collisions are loud, at load** (settled 2026-08-22): a flag token appearing
  in two parts that could ever compose together — the entry's `args` and any
  option, or options of two *different* choices — is an error; a flag token is
  leading `-`s followed by a letter (amended 2026-08-22, found in build: a bare
  number like `-1` is a value flags commonly take, and two parts passing the
  same number fight over nothing);
  options of the same choice share tokens freely (they are alternatives, and
  forcing the overlap apart is what keeps the axes orthogonal). The comparison is
  by token, values ignored — the same flag twice with equal values is still an
  error. For the same reason `quant` may be set by only one choice's options, and
  likewise `repo`. An option restating a cria-owned flag is refused exactly as
  entry `args` are.
- **Picks are state, not config** (settled 2026-08-22): the current pick per
  entry per choice lives in `~/.local/state/cria/choices.json` — cria-owned,
  strict-decoded; a broken file is reported and the config defaults used; a pick
  naming a gone choice or option is skipped on read and pruned on the next
  write. The config tree stays human-owned. The TUI picker is what writes picks;
  `cria start <id> choice=option` overrides for that one start and writes
  nothing — one-shot, so an agent's experiment never silently changes what a
  bare start launches next.
- An entry with no choices behaves exactly as today. A running server is never
  confused by choice edits: its record carries the picks it composed and the
  full command line (`docs/specs/SERVE.md`).

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
