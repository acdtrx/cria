# Step 7 — the picker modal

Status: done

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

## Result

- `internal/tui/choicepick.go`: the picker mode — `p` opens it on a choices
  entry (free everywhere, mnemonic across the surface: `picks`, the starred
  option, `choices.json`), ↑/↓/j/k walk the axes clamped, ←/→/h/l roll the pick
  wrapping, every roll writes through `picks.Prune` + `Save` at the keypress
  (only the axis that moved is stored — an axis on its config default stores
  nothing, which is what an absent key means to the store), ⏎ and esc both just
  close, a failed save notices and the picker stays up showing the pick.
- Deviation accepted from the step file's "rendered the way the existing
  overlays are": every existing overlay is a full-screen pane, which would
  cover the detail pane this step requires to keep re-composing — the spec's
  own wording ("a small modal over the list") settles it, so the picker stands
  in the entry list's pane and the detail pane stays live beside it.
- The mode is held by entry id and re-read from the tree every frame;
  `closeStalePicker` drops it when the file loses its axes (the
  managegroups rule). The frame's `press()`/`screen()`/`syncEscScope()` chains
  gain the mode; the keybar offers `p` only on an unbroken entry with choices
  and swaps to the picker's own keys while it stands.
- Folded in from step 6's watch list: the entry list's cached marker (and the
  `cached` detail word, which is the same fact spelled out) now resolve
  through `m.picks(entry)` — "serve now vs download first" answers for the
  combination a start would actually launch, and flips on the same frame a
  pick changes it.
- Found, left alone, filed in `docs/BACKLOG.md`: the cache view's
  `entriesUsing` attribution still matches declared repo/quant.
- Phase 4 and the plan's build end here: `go test -count=1 ./...` fully green,
  `gofmt -l .` silent, `go vet ./...` clean — verified independently in
  review. Live end-to-end verification (OVERVIEW) remains, with the user.
