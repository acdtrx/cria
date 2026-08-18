# Step 2 — config schema and strict loader

**Phase 1 · Status: not started**

## Intent

`internal/config`: the schema definitions and the loader implementing the whole
CONFIG.md parsing contract. The definitions must be structured so step 3 can
generate `cria docs` from the same source the parser uses — that coupling is the
point of this step's design.

## Files likely touched

`internal/config/` (schema definitions, loader, validation, tests + fixtures).

## Decisions made during planning

- go-toml/v2 strict decoding (unknown keys/wrong types error) enters here.
- Schema-as-code: one table of key definitions (name, type, rules, doc line,
  example) drives decode, validation, and docs rendering — not parallel lists.
- Validation rules from CONFIG.md, all covered by table tests: backend enum;
  `quant` llama-only; port/host resolution (entry → `default_port`/`default_host`
  → `0.0.0.0` for host, loud error for missing port); id charset; `args`
  restating a cria-owned flag (`-hf`, `--model`, `--port`, `--host`) is an error.
- Per-entry isolation: an invalid entry reports file + key and disables only
  itself; the loader returns valid entries plus a list of broken ones.
- Config root path is injectable (for tests); resolved from `os.UserHomeDir` in
  production. No env-var override — not specced.

## Acceptance criteria

- Table tests cover every rule above, valid and invalid fixtures both, including
  one broken entry alongside valid ones.
- `go test ./...` green; suite result recorded here.
