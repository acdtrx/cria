# Step 3 — `cria docs` and first-run scaffold

**Phase 1 · Status: not started**

## Intent

The agent interface: `cria docs` renders the full schema with a complete
commented example entry per backend and a `config.toml` example, generated from
step 2's definitions. First run creates the config root, `models/` and
`AGENTS.md` (embedded) when missing — cria's only writes into the tree.

## Files likely touched

`internal/config/` (docs rendering, scaffold, embedded `AGENTS.md`),
`internal/cli/` (wire `docs`), `main.go`.

## Decisions made during planning

- `AGENTS.md` embedded via `go:embed`; it points agents at `cria docs` and the
  lifecycle subcommands (`start --wait`, `status --json`) — the validation loop
  in one short page.
- Scaffold runs on every invocation, creates only what is missing, never edits
  an existing file; it is a plain function (mechanism), invoked at startup
  (trigger) — CODING-RULES §7.
- Examples in the docs output are the templates agents copy: llama example uses
  a real multi-quant repo shape; mlx example a real `mlx-community` repo shape.

## Acceptance criteria

- `cria docs` output names every schema key with its rules and both examples;
  generated from the step 2 definitions (change a definition → docs change, shown
  in a test).
- Scaffold test (temp home): fresh tree created; existing files untouched on a
  second run.
- **Phase 1 ends here**: suite green, committed; an agent-written entry file
  parses and shows up in `cria docs`-validated form. Result recorded here.
