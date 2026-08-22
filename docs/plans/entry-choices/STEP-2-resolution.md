# Step 2 — resolution

Status: not started

## Intent

The pure function the whole feature pivots on: an entry plus a selection
(choice → option) yields the effective launch facts — repo, quant, composed
args — or a refusal naming what was wrong and what is valid. No caller changes
yet.

## Files likely touched

- `internal/config/resolve.go` (new) + test.

## Decisions (from planning)

- Lives in `config`: it is meaning-of-the-config-tree logic, and `serve` should
  compose commands from resolved facts without knowing choices exist.
- `Selection` is `map[string]string` (choice name → option name).
- `DefaultSelection(entry)` = each choice's first option — the config default
  the specs name.
- `Resolve(entry, selection)`: the selection must name every choice exactly
  once — resolution is total by the time it is called; layering (explicit >
  stored > default) is the *callers'* merge, not this function's. Unknown
  choice, unknown option, or a missing choice each refuse naming the valid
  names — the message is the CLI/TUI answer, so it is written for people.
- Composition order: entry `args`, then each picked option's `args` in file
  choice order. Effective quant/repo: the entry's unless one picked option
  replaces them (validation already guarantees at most one choice can).
- A flat entry resolves to itself under the empty selection; a non-empty
  selection against it refuses ("has no choices").

## Acceptance criteria

- Unit tests: default selection; composition order (three axes); quant and repo
  replacement; every refusal with its message naming the valid names; the flat
  entry cases.
- Phase 1 ends here: full suite green, `gofmt -l .` silent, `go vet ./...`
  clean.
