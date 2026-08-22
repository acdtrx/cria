# Step 4 — the picks store

Status: not started

## Intent

`~/.local/state/cria/choices.json`: the stored pick per entry per choice, read
by CLI and TUI, written only by the TUI picker. A small package of its own —
picks are lifecycle defaults, not TUI preferences, so they cannot live in
`ui.json` (the CLI must read them without reaching into `internal/tui`).

## Files likely touched

- `internal/picks/picks.go`, `picks_test.go` (new package).

## Decisions (from planning)

- Shape: `{"<entry-id>": {"<choice>": "<option>"}}`, strict decode, atomic
  write via temp + rename — the `ui.json` doctrine verbatim (`prefs.go` is the
  model): missing file = empty picks silently; unreadable/corrupt = loud error
  return alongside usable empty picks, the next write repairs.
- `Merge(entry, stored, explicit)` builds the total selection `Resolve` needs:
  config defaults, overlaid by stored picks, overlaid by explicit picks. A
  stored pick naming a gone choice or option is *skipped here* (it falls back
  to the default) — never an error, it is cria's own stale state; an *explicit*
  pick that names nothing refuses, that one is the caller's mistake. The
  skip-vs-refuse asymmetry is the whole point of the function.
- `Prune(picks, tree)` drops picks whose entry, choice or option no longer
  exists; called by the TUI at every write (same pattern as `pruneGroups`,
  including the nil-tree-prunes-nothing lesson from entry-groups step 2).
- The CLI never writes this file (one-shot picks, settled 2026-08-22); the
  package still owns Save because the TUI needs it next phase.

## Acceptance criteria

- Unit tests: round-trip; missing/corrupt/unknown-key files; merge layering
  (default < stored < explicit); stale stored pick skipped, bad explicit pick
  refused; pruning incl. nil tree.
- Phase 2 ends here: full suite green, gofmt silent, vet clean.
