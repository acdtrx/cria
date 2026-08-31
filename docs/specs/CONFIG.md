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
│   └── <engine>.toml  # what this machine serves every model of one engine with (optional);
│                      # engines/router.toml also configures the router process itself
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
| `args`    | string[] | optional; the server's own flags, token for token, passed verbatim        |

**Args are passthrough, not schema** (settled 2026-08-18). cria types only what it
must understand to do its job — backend, repo, quant, port — and hands `args` to the
server untouched. It never grows typed keys for backend flags: chasing llama.cpp's
flag surface release-by-release is the same trap as parsing its logs (`docs/cria.md`,
principle 6). Rejected: typed per-backend keys (`ctx = 16384`, …) — validatable and
prettier, but every upstream flag change would need a cria release, and
unknown-key-is-error would make new upstream flags unusable until then.

**Args are verbatim argv tokens** (settled 2026-08-31, user, reversing the
`"key = value"` shape agreed on 2026-08-27 and built the same day). An `args`
element is one token of the command line the server takes — exactly what a person
would type after the program name, copy-pasteable in both directions. Rejected:
`key = value` lines (the server's long option without its dashes). It read well,
but it is a third dialect between the file and the server: every profile has to be
*translated* by its author, short aliases (`-ngl`, `-fa`) become unwritable, and a
flag passed twice becomes inexpressible — all so cria can hold a shape no server
speaks. The router's ini preset, the reason the shape was proposed, needs no help
from the tree: upstream canonicalizes its own alias zoo (probe-proven 2026-08-31 —
`ngl`, `fa`, `c`, `temp`, `ctk` all echoed back long), so a preset line is a flag
group with its dashes stripped, derived where the preset is composed.

- Tokens pass through untouched: nothing is reinterpreted, reformatted or split.
  A value with spaces is one TOML string and reaches the server as one argument.
- cria reads exactly one thing in a list: where each flag's tokens begin and end.
  A **flag group** is a flag token — leading dashes then a letter (amended
  2026-08-22: `-1` and `-0.5` are values, not flags) — plus the tokens after it
  until the next flag. It is the unit an override replaces and the unit the TUI
  draws one to a line, and `config.FlagGroups` is the one implementation of it.
- **A list may pass one flag twice.** It is a command line, and llama takes
  repeated flags (`--override-kv`); within one list there is no more specific side
  to prefer, so the list stands as written.
- cria composes the model-reference, port and host flags itself (`-hf repo[:quant]` /
  `--model repo`, `--port N`, `--host A`); `args` restating a cria-owned flag
  (`-hf`, `--model`, `--host`, `--port`, in either the separate-value or
  `--flag=value` spelling) is a loud error, never a silent override. Which flag
  carries a backend's model reference is declared in the schema beside the backend
  set; the engine passes it, and `internal/engine`'s test holds the two together.
- The bind default is `0.0.0.0` (settled 2026-08-18): servers are reachable from the
  rest of the LAN out of the box — both backends default to loopback on their own,
  so cria always passes the flag. A host that should stay private sets
  `default_host = "127.0.0.1"` or a per-entry `host`. cria probes health on
  loopback when the bind is `0.0.0.0`, on the bound address otherwise.
- Display follows the same rule: the TUI shows the files' own `args`, one flag
  group to a line, and the full composed command line verbatim — that *is* the
  entry's documentation.
- **Context and parallelism are passthrough like everything else** (settled
  2026-08-31, user, at plan review). `-c` and `--parallel` carry the literal
  values the server receives; cria computes nothing, and the human divides when
  reading — the layout comments in an entry file carry that note, beside the fit
  measurements already there. Rejected: composing the pool from `context ×
  parallel` — simplicity won, and it keeps cria's flag-agnosticism total;
  `host` and `port` stay the only fields cria composes. This matters because the
  semantics move upstream: on one build (10450, probed 2026-08-31) an explicit
  `--parallel 2` divided a `-c 262144` pool into 131072 per slot, while a child
  under auto-parallel reported the *full* `n_ctx` on each of four slots. That is a
  fact for whoever writes the profile, and precisely the kind of fact cria must
  not encode a formula against.

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
- **Composition is a merge by flag group across three levels** (settled
  2026-08-31, replacing the 2026-08-22 append): `engines/<engine>.toml`, then the
  entry's `args`, then the picked options' — least specific first. A flag written
  at a more specific level replaces **every** group carrying that flag in the
  levels beneath it, at the place the first of them stood; a flag the level
  introduces follows at the end. An override therefore changes what the server
  receives, not the order the files read in — which is what makes lifting a flag
  into the engine file a safe, argv-preserving edit. The effective `repo`/`quant`
  are the entry's unless a picked option replaces them.
- **Cross-level override is legal and silent; the one collision left is loud, at
  load** (settled 2026-08-31, narrowing 2026-08-22, whose "two parts of one
  launch" rule counted the entry's args and an option's as equals):
  - options of two **different** choices setting the same flag — both are picked
    at once, so there is no winner; refused, naming the flag and both options;
  - options of the **same** choice share flags freely: they are alternatives, and
    forcing the overlap apart is what keeps the axes orthogonal;
  - the entry overriding a flag its engine file sets, and a picked option
    overriding a flag the entry sets, are the point of the levels;
  - a list repeating a flag is the author's own command line, not a mistake.
  The comparison is by flag token, values ignored — two options setting one flag
  to the same value still have no winner, and `--ctx-size 8192` collides with
  `--ctx-size=8192`. For the same reason `quant` may be set by only one choice's
  options, and likewise `repo`. An option restating a cria-owned flag is refused
  exactly as entry `args` are.
- **Picks are state, not config** (settled 2026-08-22): the current pick per
  entry per choice lives in `~/.local/state/cria/choices.json` — cria-owned,
  strict-decoded; a broken file is reported and the config defaults used; a pick
  naming a gone choice or option is skipped on read and pruned on the next
  write. The config tree stays human-owned. The TUI picker is what writes picks;
  `cria start <id> choice=option` overrides for that one start and writes
  nothing — one-shot, so an agent's experiment never silently changes what a
  bare start launches next.
- **One entry, one set of axes, a combination per engine** (settled 2026-08-31).
  The axes are the entry's; which option is current is per engine, because the
  combination that fits a model as one server per entry is not the combination
  that fits it beside three others behind one router. `choices.json` holds the
  entry's own; the router's store holds the router's
  (`docs/specs/SERVE.md`). Both are written by the same gesture in their own
  surface — the TUI picker in the engine's view, and `cria router include` on the
  command line — and neither can see the other's.
- An entry with no choices behaves exactly as today. A running server is never
  confused by choice edits: its record carries the picks it composed and the
  full command line (`docs/specs/SERVE.md`).

## engines/&lt;engine&gt;.toml — what this machine serves a backend with (settled 2026-08-31)

An entry file says what is true of the model wherever it runs; the engine file says
how *this machine* runs one backend. Anything uniform across every entry of a
backend — how many layers to offload, which attention path, a log level — belongs
there rather than repeated in fifteen profiles.

| key           | type     | rules                                                              |
| ------------- | -------- | ------------------------------------------------------------------ |
| `args`        | string[] | optional; the same verbatim-token shape entries use, and the level they override |
| `port`        | integer  | router only; the port the router serves on — without it there is no router to start |
| `host`        | string   | router only; bind address, by the entries' own rule (`default_host`, else `0.0.0.0`) |
| `router_args` | string[] | router only; the router process's own flags, verbatim                |

- One file per engine, named after it (`engines/llama.toml`, `engines/mlx.toml`,
  `engines/router.toml`). Both the directory and every file in it are optional: an
  engine with no file serves entries with what they declare themselves.
- **An engine is not always a backend an entry may declare** (settled 2026-08-31).
  `llama` and `mlx` run one server per entry, and an entry names one of them in its
  `backend` key. The router runs **one server for the host** that serves the models
  included in it — ordinary entries — so no entry declares it: `backend = "router"`
  is refused, naming the backends that do serve an entry, and `cria new --router`
  scaffolds nothing. The tree therefore declares two sets: the engines it can
  configure (engine files, records, display) and the narrower set of backends an
  entry may name. Rejected: making the router a backend key — the same entry is
  meant to serve under llama *and* under the router, with picks of its own on each
  side (`docs/plans/engines/OVERVIEW.md`, ruling 2), which a single backend key
  cannot express.
- **The router's file carries two arg lists, because it has two command lines**
  (settled 2026-08-31). `args` is what every model it serves starts from — cria
  composes it into the `[*]` section of the preset the router reads, which is the
  same meaning `args` has in every engine file, and the model sections override it
  exactly as an entry overrides its engine. `router_args` is the supervisor's own
  flags (`--models-max`, `--models-autoload`, …), passed verbatim after the flags
  cria composes. Rejected: a schema field per router flag — `host`/`port` stay the
  only schema-composed fields (OVERVIEW ruling 3), and upstream's defaults stay
  upstream's.
- **The router takes no port from `default_port`** (settled 2026-08-31): that is
  the port the entries share, and a router bound to it would collide with every one
  of them at the first start. A tree with no `engines/router.toml`, or one that
  sets no `port`, simply has no router, and `cria router start` says so naming the
  file.
- Every flag in the router's `args` must be writable as one preset key: a flag
  written twice, a flag carrying more than one value, or tokens before any flag
  refuse the start by name (`docs/specs/SERVE.md`, The router).
- **`router_args` is verbatim argv too** (settled 2026-08-31, extending the
  verbatim-token ruling above): it is a command line — llama-server's own, for the
  supervisor process — so the same rule applies token for token, and cria composes
  `--models-preset`, `--host` and `--port` around it. It is the one arg list that
  never becomes preset keys, because it is not about any model: `--models-max`,
  `--models-autoload` and the sleep flags live here and reach the process
  directly. cria therefore carries none of upstream's defaults for them.
- **Which entries the router serves is not in the tree** (settled 2026-08-31).
  There is no key for it in any file: inclusion is router-scoped state, edited by
  `cria router include` / `exclude`, and the same entry can be held by the router
  under a different combination than the one a bare `cria start` composes
  (`docs/specs/SERVE.md`, The models under the router). An agent looking for a
  config key here will not find one, and `~/.config/cria/AGENTS.md` says so.
- **A router section carries the entry's args and the picked options', never the
  engine's** (settled 2026-08-31). The three-level merge below is the entry's own
  engine file, then the entry, then the picks; the router composes each model's
  section from the entry level down and writes `engines/router.toml`'s `args`
  once, as the preset's `[*]` block. Upstream applies that block under every
  section by its own precedence — merging it into each section would be cria doing
  that work twice, and would turn every pick change into a whole-file diff.
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
  an example engine file per **engine** (the router's included), and a `config.toml`
  example. The examples are the templates agents copy from.
- **The engine set and its per-key metadata live in the schema** (settled
  2026-08-31): which engines the tree may configure, which of them a file may
  declare as its backend, which keys each of them takes, the value each key carries
  in that engine's example, and the flag cria composes that engine's models under
  (`-hf`, `--model`, `--models-preset`) — the flag no args list may restate. A key states one example that holds
  under every backend or one per backend — never one backend's value standing in
  for another's, which would hand an agent a repo the backend it names cannot
  serve. `cria docs` walks that set, so a backend cria serves is a backend the
  page teaches, and the keys the page says are refused are the keys the parser
  refuses.
- **Key applicability is a set, not a single backend** (settled 2026-08-31): a key
  names the engines that take it and is refused for every other one; naming none
  means all of them. It is the same rule in an entry file and in an engine file —
  the refusal names the key's engines and then what this file speaks for (its
  backend key, or the engine it is named after).
