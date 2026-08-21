# Step 6 — manage mode

Status: pending

## Intent

The rare operations, off the main gesture: a mode over the headings for
reorder, rename, disband. Closes phase 3 — full suite green, plan done.

## Files likely touched

- `internal/tui/managegroups.go` (new) + `internal/tui/managegroups_test.go`
  (new)
- `internal/tui/tui.go` (keymap, precedence, `groups()`)
- `internal/tui/keybar.go` (scope label)

## Decisions (from planning)

- Key: `g` (groups), global scope, serve view only, enabled when ≥1 group
  exists (creation lives in step 5's flow; with no groups there is nothing to
  manage).
- In the mode, the cursor moves over group headings — all groups, hidden ones
  included; the ungrouped heading is not a target (it isn't a group). j/k
  selects; `J`/`K` move the group down/up in the order; `r` renames via the
  name input prefilled with the current name (confirm keeps membership,
  changes the name); `d` disbands — members return to ungrouped, the group
  disappears, and the notice line reports "disbanded <name> — N entries
  ungrouped" (an outcome with information, per the notice-line contract; no
  confirm panel: nothing is destroyed, entries just unfile). esc leaves the
  mode.
- Every mutation writes prefs immediately (prune + save, alert only on
  failure) — the mode holds no unsaved state; esc after a reorder keeps the
  reorder.
- Precedence: routes beside the group pick in `press()`/`groups()`/
  `syncEscScope()`; takes no screen of its own (the list is the display).

## Acceptance criteria

- Tests: reorder persists and re-renders in the new order; rename via
  prefilled input (duplicate/empty refused per step 4); disband ungroups
  members with the notice; keybar shows the manage scope; `g` disabled with
  zero groups and in the cache view; each mutation lands in `ui.json`.
- **Phase 3 / plan end: full suite green** (`go test ./...`), `gofmt -l .`
  clean, `go vet ./...` clean; then the end-to-end verification from
  OVERVIEW.md with the user.

## Result

(recorded on completion)
