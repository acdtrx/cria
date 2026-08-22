# Step 7 — the picker modal

Status: not started

## Intent

The write side: a selection key on a choices entry opens the picker modal —
one row per choice, options along it, ↑/↓ between rows, ←/→ picks, every change
written to `choices.json` at the keypress, ⏎/esc closes (settled 2026-08-22,
user-designed).

## Files likely touched

- `internal/tui/choicepick.go`, `choicepick_test.go` (new).
- `internal/tui/tui.go`, `keybar.go`, `serveview.go` — mode wiring, key hint,
  esc scope.

## Decisions (from planning)

- A modal over the entry list (the user's chosen shape), rendered the way the
  existing overlays are; exact key and layout are build-time choices per the
  spec's standing rule ("exact keybinds get detailed as they are built") —
  candidate key decided in review with the user.
- Rows in the entry's choice order; each row cycles its own options with ←/→,
  clamped or wrapping — pick wrapping, it is a pick not a carry. The current
  pick is the cursor's starting position on each row.
- Every ←/→ writes through `picks.Prune` + save immediately — leaving is never
  a discard (the entry-groups doctrine); the detail pane re-renders from the
  same write, so the composed command follows each keypress even while the
  modal is up.
- ⏎ and esc both just close — there is nothing to confirm; esc scope joins
  `syncEscScope()` ahead of notices, like other modes.
- On a flat entry the key does nothing (spec: no picker offered); the keybar
  shows the hint only when the highlighted entry has choices.
- A save failure lands on the notice line and the modal stays up — the pick is
  still shown, the user decides whether to retry or bail.

## Acceptance criteria

- Component tests: open/refuse-to-open by entry kind; row/option navigation;
  a pick writes (fake store) pruned; close via ⏎ and esc; esc precedence with
  a notice up; keybar hint presence; detail pane reflects a pick made while
  the modal is up.
- Phase 4 ends here: full suite green, gofmt silent, vet clean — plan complete
  pending the OVERVIEW's live end-to-end verification with the user.
