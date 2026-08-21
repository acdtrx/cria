# Step 3 — headings in the entry list

Status: pending

## Intent

Render the sections: muted headings interleaved into the list, cursor still
over entries only, scroll window aware of heading lines. With zero groups the
output is byte-identical to today.

## Files likely touched

- `internal/tui/serveview.go` (`rows()`, `listLines()`, `window()` call site)
- `internal/tui/styles.go` (heading style in the palette table)
- `internal/tui/serveview_test.go`, `internal/tui/styles_test.go`

## Decisions (from planning)

- `rows()` becomes the concatenation of step 2's sections — entry rows only.
  Headings are **render-time lines**, never rows: `m.selected` semantics,
  `reselect`/`clamped`/`rebindContext` and the pick machinery stay untouched.
- `listLines()` builds the line list by walking sections: heading line (when
  visible), then that section's entry rows. It must map the selected row index
  to its line index and window over **lines** so the cursor stays on screen —
  `window()` itself can stay index-based if handed the line-space values.
- Heading style: muted by hue/saturation per the palette rules, added to the
  palette table so `styles_test.go` covers it. Headings render the group name
  only (no counts, no decoration); the ungrouped heading renders `ungrouped`.
- The id column stays global to the pane (today's `idColumn` over all rows) —
  alignment across groups, no per-group widths.
- Detail pane, presence marks, selection band: unchanged per row.

## Test updates (deliberate, from the explore pass)

- `serveview_test.go:68` (exact 3 lines + indexes), `:107` (broken last),
  `:125` (cursor indexes), `:162` (windowing), `:283`, `:299` — re-anchor
  against a fixture with groups where useful; keep at least one no-groups test
  asserting byte-identical legacy rendering.
- New tests: headings present in group order, hidden-heading case,
  ungrouped heading only with ≥1 group, cursor never lands on a heading,
  window keeps the selected entry visible with headings above it.

## Acceptance criteria

- No-groups rendering byte-identical to before the step (asserted).
- Grouped fixture renders headings per the spec bullet, cursor behavior
  unchanged.
- Palette test passes with the new style; full suite green (phase 2 ends
  here).

## Result

(recorded on completion)
