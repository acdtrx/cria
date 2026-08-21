# Step 3 — headings in the entry list

Status: done

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

What changed:

- `rows()` is now `entryRows(m.tree, m.prefs.Groups, m.prefs.Backend)` — one
  source for the cursor sequence. `selectedRow`, `reselect`, `clamped` and the
  pick machinery were not touched.
- `listLines()` walks `entrySections(...)`: a heading line where
  `section.heading` says so, then that section's rows through the unchanged
  `rowLine` path. It tracks the line the selected row lands on and windows over
  the drawn lines around it, so headings cost capacity and the cursor stays on
  screen. The empty-backend message still keys off `len(rows) == 0`, so a pane
  whose only sections are empty groups shows where to write entries rather than
  bare headings.
- `headingLine` renders the group name alone (`ungrouped` for the tail), at the
  pane's left edge — rows are indented past the cursor's marker column, which is
  what tells the two apart with no glyph spent on it.
- `headingHex = "#9088b0"` joined the palette table (6.33:1 on black, muted by
  hue and saturation) with `headingStyle` built from it; `styles_test.go` lists
  the style, so both palette tests cover it. Unbolded on purpose: the heading is
  furniture and the ids under it are what the eye hunts for.

Tests: `go test ./...` fully green (phase 2 ends here), `gofmt -l .` silent,
`go vet ./...` clean.

Deviations:

- **No existing test needed re-anchoring.** Every serve-view fixture is
  group-free, so all of them kept passing unchanged — which is the byte-identical
  guarantee showing up as evidence rather than as an assertion. The named line
  numbers were left alone.
- Added `TestWithoutGroupsTheListIsDrawnAsItWas`, which compares `listLines`
  against the concatenated `rowLine`s byte for byte (escapes included) — the
  explicit legacy anchor the step asked for.
- **`TestWithoutGroupsTheRowsAreTheListAsItWas` (step 2's) had gone
  tautological**: it compared `entryRows(...)` with `frame.rows()`, which is now
  the same call. Re-anchored on the literal id sequences per backend instead.
- New render tests: headings in group order per backend (hidden headings absent,
  empty groups keeping theirs), the ungrouped heading only where it separates,
  the cursor never on a heading, and a short pane windowing over the headings.
