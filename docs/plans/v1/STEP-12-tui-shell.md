# Step 12 — TUI shell

**Phase 5 · Status: not started**

## Intent

`internal/tui`: the bubbletea program frame — view routing (serve/cache),
the top status box in all its display states, the grouped keybar, UI
preferences, styling. No feature screens yet; the frame every screen hangs on.

## Files likely touched

`internal/tui/` (program, status box, keybar, prefs, styles), `main.go`,
`go.mod` (bubbletea/bubbles/lipgloss v2 enter here at latest stable —
verify the `charm.land` import paths against current docs).

## Decisions made during planning

- Status box renders the step 9 snapshot struct; `stopped` display state comes
  from prefs (last-started entry) when no record is live (TUI.md).
- Keybar is one component taking (selection keys, server keys, global keys) per
  view — the grouping is structural, not per-screen strings.
- UI prefs (`active backend`, `last-started entry`) as one JSON file in the
  state dir, loaded at start, written on change; a missing/corrupt prefs file
  resets to defaults loudly (it is machine-owned state, not config).
- Status refresh on a ticker while the program runs (observation polling,
  SERVE.md); interval a named constant.

## Acceptance criteria

- Builds and runs: empty serve/cache views, ⇥ backend toggle persists across a
  restart, status box shows running/stopped correctly against a real record.
- Component tests where the TUI framework allows cheaply (prefs round-trip,
  keybar grouping); rendering verified manually and recorded here with the
  suite result.
