# Step 2 — resolution

Status: done

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

## Result

- `internal/config/resolve.go` adds `Selection` (choice → option),
  `Launch{Repo, Quant, Args}` — only what a pick can change; backend/port/host
  stay on the entry — `DefaultSelection` and `Resolve`, with refusals that name
  the valid names in file order. 24 subtests in `resolve_test.go`, incl. an
  aliasing test pinning that composition clones the entry's args (appending
  into their spare capacity would write one launch's picks into the loaded
  tree).
- Decided while implementing: refusal errors are plain errors, not `KeyError` —
  a bad pick comes from the CLI or the picker, not from a config file;
  `DefaultSelection` returns a non-nil empty map for a flat entry so callers
  layer picks over it without a nil check; unknown choice names are reported
  before unpicked choices (a misspelled `qunt=q4` must read as the misspelling,
  not as "nothing picked for quant"), sorted so two mistakes always report the
  same one.
- Phase 1 ends here: `go test -count=1 ./...` fully green, `gofmt -l .` silent,
  `go vet ./...` clean — verified independently in review. Step 3 will meet the
  two call sites that read `entry.Repo/Quant/Args` directly
  (`serve/command.go`, `serve/status.go`), both already in the plan.
