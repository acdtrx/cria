# CLI — subcommand surface

One binary, two faces: bare `cria` opens the TUI; subcommands drive the same
subsystems scriptably (`docs/cria.md`, principle 5 — agents validate what they
write). This spec is the index of the surface; each behavior's contract lives with
its subsystem.

## The whole v1 surface (settled 2026-08-18)

- `cria` — the TUI.
- `cria start <id> [--wait]` — `docs/specs/SERVE.md`.
- `cria stop [<id>]` — `docs/specs/SERVE.md`.
- `cria status [--json]` — `docs/specs/SERVE.md`.
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
- `cria edit <id>` (settled 2026-08-18) — opens `$VISUAL`, else `$EDITOR`, on
  the entry's file (broken entries included — that is the point) and waits;
  the user's editor writes, cria still never does. Neither variable set, an
  unknown id, or a non-zero editor exit refuse with exit 1.
- `cria --help` / `-h` / `help` — one page: the subcommands, the flags, the
  exit-code rule, and the pointers agents need (`cria docs`, the validate loop).
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

Flags: `--wait` on start, `--json` on status and bench, `--paths` on list,
`--llama`/`--mlx` on new, `--sizes`/`--runs`/`--gen` on bench, `--version` and
`--help` on the bare binary — nothing else speaks machine or takes options.

## Rules

- Exit codes mean what they say: `0` = the asked-for thing is true or done
  (started and healthy with `--wait`, stopped, at least one live server for
  `status`); non-zero otherwise. Errors name the failing thing and its fix —
  never a silent failure.
- `--json` exists on `status` only in v1; the other subcommands speak
  human-readable output.
