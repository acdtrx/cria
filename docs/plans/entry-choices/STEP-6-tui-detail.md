# Step 6 — TUI detail and start

Status: not started

## Intent

The read side of the TUI: the detail pane shows a choices entry's current picks
and the command they compose; start launches the stored picks; restart-last
replays the record's own picks; the status box names the running combination.
No picker yet — picks change only via `choices.json` edits until step 7.

## Files likely touched

- `internal/tui/serveview.go`, `lifecycle.go`, `status.go`, `tui.go` + tests.

## Decisions (from planning)

- The TUI loads picks alongside prefs at launch and re-reads before use the way
  group prefs are re-read — the file is shared state, cheap, and staleness
  questions disappear (grouppick's lesson).
- Detail pane: one line per choice (`quant  q4* q6 q8`, pick marked), then the
  composed command for the current picks — the pane already carries the
  command-line truth; it now tracks the picks (spec: picking and seeing the
  command are one loop).
- Start on a choices entry: `picks.Merge(entry, stored, nil)` →
  `Manager.Start` — the TUI has no one-shot path; one-shots are CLI territory.
- Restart-last replays `Record.Selection` verbatim, even when stored picks have
  moved on — the box shows what ran, restart reproduces it
  (`docs/specs/TUI.md`, settled 2026-08-22). A selection that no longer
  resolves (the entry's choices were edited meanwhile) refuses on the notice
  line with `Resolve`'s message — never silently falls back to defaults.
- Status box: the combo joins the entry line in pick order; display state only.
- Entry list rows stay name-only — the combo lives in the detail pane and the
  status box (settled 2026-08-22 in design: the pane is where truth lives).

## Acceptance criteria

- Component tests: detail pane rendering for a choices entry (picks marked,
  composed command) and a flat entry (unchanged); start passes the merged
  stored selection; restart-last passes the record's selection, incl. the
  no-longer-resolves refusal; status box shows the combo.
- Suite green or expected reds named (mid-phase step).
