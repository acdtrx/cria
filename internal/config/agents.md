# cria config — notes for coding agents

This tree is cria's interface. You write it; cria reads it and drives what it
declares, and never rewrites a file here. Your comments and formatting survive.

## Get the schema from the binary

Run `cria docs`. It prints the whole key reference plus a complete commented
example for each backend and for `config.toml`, generated from the same
definitions cria's parser checks files against — it cannot be out of date. Do not
learn the schema from this page.

## Writing an entry

- One file per launchable thing: `models/<id>.toml`. The id is the filename minus
  `.toml`, and it is what `cria start <id>` takes.
- `cria new <id> [--llama|--mlx]` scaffolds a commented starting file (the same
  example `cria docs` prints) and opens `$EDITOR` on it — or write the file from
  scratch; both end at the same schema.
- Another model is another entry file. One model run in variations — quants,
  context sizes, feature toggles — declares them as `[[choice]]` axes inside its
  own file, one axis per thing that varies, and flags that must vary together in
  the same option.
- cria composes the model, port and host flags itself. Every other server flag
  goes in `args`, verbatim; check the server's own `--help` for what belongs
  there, since cria does not validate them.
- Take parameters from the model provider's own recommendation and note the source
  in a comment.
- Tree-wide settings live in `config.toml`: `default_port`, `default_host`, and
  absolute paths to the tools.

## Validate what you wrote

    cria start <id> --wait   # exit 0 means the server came up healthy
    cria status --json       # pid, port, phase, health, log path
    cria stop <id>

`cria start --wait` is the check: a non-zero exit means the entry does not serve.
Read `log` from `cria status --json` and tail that file to find out why.

A file cria refuses disables only itself, and the error names the file and the
offending key.
