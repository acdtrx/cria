# CLI — subcommand surface

One binary, two faces: bare `cria` opens the TUI; subcommands drive the same
subsystems scriptably (`docs/cria.md`, principle 5 — agents validate what they
write). This spec is the index of the surface; each behavior's contract lives with
its subsystem.

## The whole v1 surface (settled 2026-08-18)

- `cria` — the TUI.
- `cria start <id> [choice=option ...] [--wait]` — `docs/specs/SERVE.md`. Picks
  (settled 2026-08-22): a `choice=option` argument overrides that choice for this
  start only — one-shot, never persisted; unnamed choices use the stored picks,
  else the config defaults (`docs/specs/CONFIG.md`, Choices). `=` is what tells a
  pick from the id (ids cannot contain it); unknown choices or options refuse
  naming the valid ones.
- `cria stop [<id>]` — `docs/specs/SERVE.md`.
- `cria status [--json]` — `docs/specs/SERVE.md`. It reports this host's router
  too, in a block of its own, since cria started it (amended 2026-08-31); the JSON
  document gains a `router` key, null on a host with none.
- `cria router [status|start|stop|models|include|exclude|load|unload]` (settled
  2026-08-31) — the one router process this host runs, and the models under it
  (`docs/specs/SERVE.md`, The router). A subcommand of its own rather than an id
  `cria start` takes: the router is not an entry, nothing in `models/` declares
  it, an entry could be named `router` without being it, and which models it holds
  is a question about a process the whole tree shares. A bare `cria router`
  reports — the verb that changes nothing is the one you get without typing one;
  `status` exits 0 while a router is up (serving or on its way) and non-zero when
  there is none, the same question `cria status` answers. There is no `--wait`:
  a router goes green as soon as it is routing, and what a caller waits for is a
  model, which loads on the request that names it.
  - `include <id> [choice=option ...]` is the **one** verb that settles a model's
    combination (settled 2026-08-31): re-running it on a model the router already
    holds changes what it is held under, layering the picks typed now over the
    ones it is held under, over the entry's config defaults — the same three
    layers a launch uses, with the result stored, because here the combination
    *is* the state. Two spellings for one write would be two ways to edit one
    file, so there is no separate `picks` verb.
  - `exclude <id>` never reads the config tree: an entry whose file was renamed
    away is exactly the one that needs dropping, and refusing to exclude what the
    tree no longer declares would leave that inclusion with no way out. It reports
    whether it dropped anything.
  - `models` is the composition without a router — what the next start would
    serve, with the skipped models and their reasons. It exits 0 whenever it could
    read, by `cria list`'s rule: "the router holds nothing" is a true answer.
  - `load <id>` / `unload <id> [--ignore-busy]` verify the name against the
    router's own listing before sending anything, so cria only ever addresses a
    model the router has just said it holds. `unload` refuses a model that is
    answering a request right now — the gate `cria validate` puts in front of
    displacing a busy server, spelled the same way and lifted by the same flag.
  - Every verb that takes no argument refuses one.
- **`cria list` lists entries, and the router is not one** (settled 2026-08-31).
  It answers what the config tree declares; which of those entries the router
  holds is state, and `cria router models` is its listing — with the router's own
  picks, which may differ from the ones `cria list` marks. Rejected: a router
  column on the entry listing — it would make the tree's listing depend on cria's
  state to be read, and the two answers belong to two questions.
- `cria validate <id> [choice=option ...] [--ignore-busy]` (settled 2026-08-23,
  user-designed) —
  the one blocking command that proves an entry serves on a machine already
  serving something else: it displaces the cria-started server holding the entry's
  port, starts the entry, waits green, asks it for one real completion, stops it,
  and puts the displaced server back from its own record. Picks are start's, with
  start's one-shot meaning — the combination being proved. `docs/specs/SERVE.md`,
  Validate, owns the protocol, the port scoping, the busy gate and the four exit
  codes; `--ignore-busy` is the only override and it lifts the busy gate alone.
- `cria docs` — prints the config schema and full examples; `docs/specs/CONFIG.md`.
- `cria new <id> [--llama|--mlx]` (settled 2026-08-18, reinstated from the
  backlog) — scaffolds `models/<id>.toml` from the schema-rendered example
  (create-only; an existing file refuses toward `cria edit`), opens the editor
  on it, and reports the file's verdict when the editor closes — valid names
  the start command, broken names the key and the fix. The two backend flags
  are peers; bare defaults to llama.
- `cria list [--paths]` (settled 2026-08-18) — one aligned line per entry (id,
  backend, repo:quant, port; `--paths` appends the entry's file — how an agent
  locates a profile), refused files listed after with their key errors; an
  empty tree points at `cria docs` and exits 0 (an empty list is a true answer).
  An entry with choices lists them with their current picks (amended
  2026-08-22) — how an agent learns what `cria start <id> choice=option` may
  name, and what a bare start would launch.
- `cria edit <id>` (settled 2026-08-18) — opens `$VISUAL`, else `$EDITOR`, on
  the entry's file (broken entries included — that is the point) and waits;
  the user's editor writes, cria still never does. Neither variable set, an
  unknown id, or a non-zero editor exit refuse with exit 1.
- `cria --help` / `-h` / `help` — one page: the subcommands, the flags, the
  exit-code rule, and the pointers agents need (`cria docs`, `cria validate`).
- `cria bench [<id>] [--sizes 16,4096,16384] [--runs N] [--gen N] [--json]`
  (settled 2026-08-19) — measures a running server's prefill and decode rates
  per prompt size (`docs/specs/SERVE.md` owns the measurement contract). No id
  follows stop's convention (one live server measures it, several refuse naming
  them). The smallest size is 16 tokens; `--sizes 0` clamps to it with a note.
  Exit 0 when every size measured; 1 when any size had nothing to measure —
  partial results still print.
- `cria wired-limit <MB>` (settled 2026-08-18, user-designed) — generates the
  launchd plist that pins `iogpu.wired_limit_mb` at boot (Apple-silicon Macs
  reset it, and big models need the headroom): the plist to **stdout** so a
  redirect yields a clean file, the install/verify/uninstall instructions to
  **stderr**, every sudo step the user's to run — cria generates, it never
  installs (principle 1). The value is validated against the machine's memory
  (loud refusal at ≥ RAM or off macOS); the file carries its own uninstall
  steps in a comment.
- `cria update` (settled 2026-08-21) — replaces the running binary with the
  latest GitHub release: the embedded version against the newest release tag,
  the platform asset verified against the release's `checksums.txt`, then an
  atomic rename over the executable's resolved path. Equality is the whole
  comparison — a release binary at the latest tag answers "already the latest"
  on exit 0 (a true answer), and a dev build matches no tag, so a dev machine
  updates to the latest release too (deliberate: it is how a hand-deployed
  host rejoins the release train). Running servers are untouched — they hold
  the old inode; the new binary applies from the next invocation.

Nothing else: no cache operations from the CLI (`docs/BACKLOG.md`).

Flags: `--wait` on start, `--json` on status and bench, `--ignore-busy` on
validate and `router unload`, `--paths` on list, `--llama`/`--mlx` on new,
`--sizes`/`--runs`/`--gen` on bench, `--version` and `--help` on the bare binary — nothing else takes options,
and nothing outside `--json` speaks machine. `cria new` takes one flag per backend
an *entry* may declare (settled 2026-08-31): `--router` scaffolds nothing, and is
answered with why rather than "unknown flag" — the models the router serves are
ordinary entries, and what makes them the router's lives outside the tree.

## Rules

- Exit codes mean what they say: `0` = the asked-for thing is true or done
  (started and healthy with `--wait`, stopped, at least one live server for
  `status`); non-zero otherwise. Errors name the failing thing and its fix —
  never a silent failure.
- `validate` extends that rule with a code of its own (settled 2026-08-25),
  because it is the one subcommand that changes the host and has to change it
  back: `1` keeps its meaning (the entry does not serve) and adds the promise that
  the machine is as validate found it; `2` is a refusal that touched nothing —
  the same code an unroutable command line gets, deliberately, since both mean
  cria did nothing to the host; `3` says the machine was left changed and names
  what is serving now. The full contract is `docs/specs/SERVE.md`, Validate.
- `--json` exists on `status` and `bench` in v1; the other subcommands speak
  human-readable output — `validate`'s machine contract is its exit code and one
  reason line (settled 2026-08-23), not a document.
