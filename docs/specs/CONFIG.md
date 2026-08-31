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
├── engines/
│   └── <engine>.toml  # what this machine serves every entry of one backend with (optional)
└── models/
    └── <id>.toml      # one launchable entry per file
```

- cria's writes into the tree are create-only and never touch an existing file:
  the root, `models/` and `AGENTS.md` on first run when missing, and the entry
  file `cria new <id>` scaffolds (settled 2026-08-18, reinstated from the
  backlog) — whose content is the schema-rendered example `cria docs` prints,
  from the same definitions the parser uses, so the scaffold cannot drift from
  the binary. `engines/` is **not** scaffolded (settled 2026-08-31): a missing
  engine file is the normal state, and creating an empty directory would be a
  write that documents nothing `cria docs` does not already teach.
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
| `args`    | string[] | optional; one `"key = value"` line per flag (below)                       |

**Args are passthrough, not schema** (settled 2026-08-18). cria types only what it
must understand to do its job — backend, repo, quant, port — and hands `args` to the
server untouched. It never grows typed keys for backend flags: chasing llama.cpp's
flag surface release-by-release is the same trap as parsing its logs (`docs/cria.md`,
principle 6). Rejected: typed per-backend keys (`ctx = 16384`, …) — validatable and
prettier, but every upstream flag change would need a cria release, and
unknown-key-is-error would make new upstream flags unusable until then.

**Args are keys, in TOML** (settled 2026-08-31). Each element of an `args` list is
one `"key = value"` line: the server's own long option written without its dashes,
and the value it takes. TOML stays the tree's syntax — the servers' own ini config
format is something cria *composes*, never something a human writes here. The shape
is an array of strings rather than a `[args]` table because a table loses two things
the tree needs: the author's order (an array keeps it, and the composed command line
is diffable against the one before it) and the value as written (a TOML table would
retype `0.70` or `1e5` and hand the server a number cria reformatted). An array is
also where a comment can sit beside the value it explains — the reason context-size
arithmetic is written down at all.

- The key is spelled without dashes; cria adds them: one dash for a one-letter key,
  two for anything longer. A short alias of more than one letter (`-ngl`, `-fa`) has
  no spelling — write the long option (`gpu-layers`, `flash-attn`). cria owns no
  alias table, so a key the server does not know is refused by the server, by name,
  at startup.
- `key = true` is a flag that takes no value. Every other value is passed exactly as
  written, `false` included — a flag with an off switch takes the word the server
  itself takes for it (`flash-attn = off`).
- Values are never reinterpreted: the split is on the first `=`, both halves are
  trimmed of the spaces around them, and a value keeps any `=` of its own. A value
  reaches the server as one argument, spaces and all.
- **One list may not set a key twice** — within one list there is no more specific
  side to take, so it is a loud load error. Repeating a flag is therefore
  inexpressible; accepted as a limit (the servers' own config format shares it).
- The old shape — a token list, `["--ctx-size", "16384"]` — is refused loudly with
  the one edit that fixes it (feature-building mode: no dual-read).
- cria composes the model-reference, port and host flags itself (`-hf repo[:quant]` /
  `--model repo`, `--port N`, `--host A`); `args` restating a cria-owned key (`hf`,
  `model`, `host`, `port`) is a loud error, never a silent override. Which key spells
  a backend's model reference is declared in the schema beside the backend set; the
  engine spells the flag, and `internal/engine`'s test holds the two together.
- The bind default is `0.0.0.0` (settled 2026-08-18): servers are reachable from the
  rest of the LAN out of the box — both backends default to loopback on their own,
  so cria always passes the flag. A host that should stay private sets
  `default_host = "127.0.0.1"` or a per-entry `host`. cria probes health on
  loopback when the bind is `0.0.0.0`, on the bound address otherwise.
- Display follows the same rule: the TUI shows the files' own `args` lines and the
  full composed command line verbatim — that *is* the entry's documentation.

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
| `[[choice.option]]` `args`   | string[] | optional; merged over the entry's args when the option is picked |

- A choice needs at least one option, and the **first option is the config
  default**. A one-option choice is legal: a named, always-on block of args.
- **Composition is a merge by key** (settled 2026-08-31, replacing the 2026-08-22
  append): the levels are the engine's `args`, then the entry's, then the picked
  options' — least specific first. A key set at more than one level takes the
  value of the most specific level that sets it and keeps the place of the first
  line that mentioned it, so an override changes the value and not the order the
  file reads in. The effective `repo`/`quant` are the entry's unless a picked
  option replaces them.
- **Cross-level override is legal and explicit**; a collision *within* one level
  is loud, at load (settled 2026-08-31, amending 2026-08-22, whose "two parts of
  one launch" rule counted the entry's args and an option's as equals):
  - one `args` list setting a key twice — refused where it is written;
  - options of two **different** choices setting the same key — both are picked
    at once, so there is no winner; refused, naming the key and both options;
  - options of the **same** choice share keys freely: they are alternatives, and
    forcing the overlap apart is what keeps the axes orthogonal;
  - the entry overriding a key its engine file sets, and a picked option
    overriding a key the entry sets, are the point of the levels.
  The comparison is by key, values ignored — the same key twice with equal values
  is still two homes at one level. For the same reason `quant` may be set by only
  one choice's options, and likewise `repo`. An option restating a cria-owned key
  is refused exactly as entry `args` are.
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

## engines/&lt;engine&gt;.toml — what this machine serves a backend with (settled 2026-08-31)

An entry file says what is true of the model wherever it runs; the engine file says
how *this machine* runs one backend. Anything uniform across every entry of a
backend — how many layers to offload, which attention path, a log level — belongs
there rather than repeated in fifteen profiles.

| key    | type     | rules                                                              |
| ------ | -------- | ------------------------------------------------------------------ |
| `args` | string[] | optional; the same `"key = value"` shape entries use, and the level they override |

- One file per backend, named after it (`engines/llama.toml`, `engines/mlx.toml`).
  Both the directory and every file in it are optional: an engine with no file
  serves entries with what they declare themselves.
- Human/agent-owned like the rest of the tree; cria reads it and never writes it.
- A file named after something cria does not serve is not read at all — it is
  somebody's note, not a config cria silently obeys.
- **A broken engine file fails the load**, the way a broken `config.toml` does:
  it is tree-wide configuration that every entry of its backend resolves against,
  so there is nothing to isolate. Rejected: disabling that backend's entries one
  by one — it reports one file's mistake once per entry and points the reader at
  the wrong file.

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
  bricks the tree. The two tree-wide files are the exception, and say so: a broken
  `config.toml` or `engines/<engine>.toml` fails the load, naming the file and the
  key.
- `cria docs` output = this schema, a complete commented example entry per backend,
  an example engine file per backend, and a `config.toml` example. The examples are
  the templates agents copy from.
- **The backend set and its per-key metadata live in the schema** (settled
  2026-08-31): which backends a file may declare, which keys each of them takes,
  the value each key carries in that backend's example, and the args key that
  backend's model reference is composed from. A key states one example that holds
  under every backend or one per backend — never one backend's value standing in
  for another's, which would hand an agent a repo the backend it names cannot
  serve. `cria docs` walks that set, so a backend cria serves is a backend the
  page teaches, and the keys the page says are refused are the keys the parser
  refuses.
- **Key applicability is a set, not a single backend** (settled 2026-08-31): a key
  names the backends that take it and is refused for every other one; naming none
  means all of them. A key two of three backends take — `quant`, once a router
  engine exists — is expressible without a third state, and refusals stay total.
