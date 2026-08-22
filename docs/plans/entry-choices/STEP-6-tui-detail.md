# Step 6 — TUI detail and start

Status: done

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

## Result

- The model holds the store (`m.stored`), read once in `Run` beside the prefs —
  the step file's "re-reads before use" note described something `loadPrefs`
  never did: prefs are read once and written through, and picks now mirror
  that exactly; correct on the merits too, since the TUI is the store's only
  writer. A broken store surfaces on the notice line (prefs' failure first when
  both break — the frame depends on prefs, only picks on the store).
- `m.picks(entry)` merges stored over defaults; the impossible merge refusal is
  left to travel as a nil selection that `config.Resolve` turns down loudly —
  never a silent fall back to defaults.
- Detail pane: one `choices` block (label-column cells truncate at nine, and
  axis names are author-chosen, so names live in the value lines) —
  `quant: q4* q6 q8` per axis, pick in factStyle + star, alternatives
  quietStyle, no new palette entries; `composedCommand(entry, selection)` takes
  the same selection that drew the block, so the two cannot disagree.
- Restart-last replays the RECORD's selection via `replayOf`, which refuses an
  unresolvable replay on the notice line **before the stop** — a fallback would
  swap the model under a swap-back, and stopping first would leave nothing for
  the refusal to protect. Found and fixed in the same stroke: the crash-report
  restart (`restartShownEntry`) previously started on current defaults — a
  silent combination swap; it now replays the exited record too, while the
  cross-session last-started id (no record behind it) starts on stored picks.
- `spelledPicks` moved to `internal/format` as `format.Picks` — shared display
  vocabulary, CLI now imports it. Status box: the combination is a real column
  after the model reference; the empty-column collapse rule generalised so a
  box of flat records renders byte-identical to today.
- Watch in live use (recorded, out of scope here): the entry list's cached
  marker and the CLI list row read the entry's *declared* repo/quant — an
  entry whose quant lives in options shows a bare repo, and cached-ness is not
  resolved through the current picks. Step 7 takes the cached-marker half.
- Suite fully green (`go test -count=1 ./...`), gofmt silent, vet clean —
  verified independently in review. Mid-phase step, no expected reds.
