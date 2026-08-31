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
  own file, one axis per thing that varies, and keys that must vary together in
  the same option.
- Server flags are keys: each element of `args` is one `"key = value"` line,
  where the key is the server's own long option written without its dashes
  (`"ctx-size = 16384"`, `"jinja = true"` for a flag that takes no value). cria
  splits on the first `=` and passes both halves on untouched, so check the
  server's own `--help` for what belongs there.
- cria composes the model reference, the host and the port itself, so `args` may
  not set those keys.
- What this machine serves *every* entry of one backend with goes in
  `engines/<backend>.toml` — the same `args` shape. An entry's own keys override
  it, and a picked option's override both.
- Take parameters from the model provider's own recommendation and note the source
  in a comment.
- Tree-wide settings live in `config.toml`: `default_port`, `default_host`, and
  absolute paths to the tools.

## Validate what you wrote

    cria validate <id> [choice=option ...]

One blocking command, and it leaves the machine as it found it: cria stops
whatever server holds the entry's port, starts the entry, waits until it serves,
asks it for one real completion, stops it, and puts the displaced server back
under its own picks. Servers on other ports are never touched.

    0  it serves
    1  it does not; the last line says what failed
    2  cria refused and touched nothing — unknown entry or pick, a missing tool,
       or a port held by something it must not stop
    3  the swap was left half done; the last line says what is serving now

Read `log` from `cria status --json` and tail that file to find out why a
validation failed.

`cria start <id> [--wait]`, `cria status --json` and `cria stop <id>` are still
the manual verbs, for driving a server you want left running.

A file cria refuses disables only itself, and the error names the file and the
offending key.
