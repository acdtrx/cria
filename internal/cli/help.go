package cli

// helpPage is what `cria --help`, `cria -h` and `cria help` print. It is the
// whole surface on one screen: what each subcommand is for, what the flags do,
// what the exit codes mean, and where the config schema lives — the page someone
// meeting cria reads before they read anything else (docs/specs/CLI.md).
//
// It does not restate the schema. `cria docs` prints that from the definitions
// the parser uses, so this page points at it rather than growing a second copy
// that drifts (CLAUDE.md: schema and docs are one source).
//
// The last block is addressed to a coding agent, because one is the expected
// reader: it names the two commands that prove a freshly written entry actually
// serves (docs/cria.md, principle 5).
const helpPage = `cria — local LLM servers from a config tree

USAGE
  cria                      open the TUI
  cria <subcommand> [flags]

SUBCOMMANDS
  start <id> [--wait]       start the entry <id>; --wait blocks until it serves or fails
  stop [<id>]               stop a running server; the id is required when several run
  status [--json]           what every server cria started is doing right now
  bench [<id>] [flags]      measure a running server: prefill and decode tokens/second
  list [--paths]            the entries the config tree declares
  new <id> [--llama|--mlx]  scaffold an entry file and open your editor on it
  edit <id>                 open an entry's file in $VISUAL, else $EDITOR
  docs                      print the config schema and a complete example of every file
  wired-limit <MB>          generate the launchd plist that pins iogpu.wired_limit_mb
  update                    replace this binary with the latest GitHub release
  help                      this page

FLAGS
  --wait      start: block until the server answers its health endpoint, or fails
  --json      status, bench: emit the same facts as one JSON document
  --sizes     bench: prompt sizes in tokens, comma-separated (default 16,4096,16384)
  --runs      bench: measured runs per size (default 3)
  --gen       bench: tokens each run asks the server to generate (default 256)
  --paths     list: add each entry's file path
  --llama     new: scaffold a llama entry — the backend a bare cria new takes
  --mlx       new: scaffold an mlx entry
  --version   print the version of this binary
  --help, -h  this page

EXIT CODES
  0 the asked-for thing is true or done, 1 it is not, 2 the command line could not be routed.

CONFIG
  The tree is ~/.config/cria: you write it, cria reads it and drives what it declares.
  config schema and examples: cria docs

  agents: cria docs prints everything needed to write entries; validate with
  cria start <id> --wait and cria status --json
`
