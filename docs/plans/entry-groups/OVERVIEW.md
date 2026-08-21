# Entry groups — plan

Named, ordered groups in the serve view's entry list, so daily-driver profiles,
macmini-candidate tests and experiment piles stop sharing one flat list. Design
settled 2026-08-21 with the user; the contracts live in `docs/specs/TUI.md`
(Entry groups bullet) — this plan is the build order.

## Goal

The entry list renders prefs-defined groups as muted headings in manual order,
ungrouped entries trailing, exactly today's list when no groups exist. Entries
are filed via a move pick over the headings (including `new group…` with a
notice-line name input); groups are reordered/renamed/disbanded in a small
manage mode. Everything persists in `ui.json`.

## Scope

- `internal/tui/prefs.go` — `groups` field, strict validation.
- `internal/tui` — grouped section computation, heading rendering, move pick,
  notice-line name input, manage mode, keybar scopes.
- `docs/specs/TUI.md` — already updated (settled with the design, 2026-08-21).

## Out of scope

- CLI surface (`cria start` etc.) — groups are TUI presentation only.
- Cache view — ungrouped, unchanged.
- Per-entry manual ordering within a group (alphabetical stays; rejected in
  design Q&A).
- Any write to the config tree (hard rail).

## Constraints

- No new dependencies; the name input is hand-rolled rune handling like the
  rest of the TUI (bubbles textinput deliberately not imported).
- `ui.json` keeps its contract: strict decode (unknown keys/wrong types are
  errors), atomic write, broken file resets loudly to defaults.
- Heading style must join the palette table and pass the WCAG AA palette test.
- The three precedence chains (`press()`, `screen()`, `groups()`) plus
  `walksTheCache()` and `syncEscScope()` must stay consistent when modes are
  added.

## Risks

- `window()`/cursor math: selection is an index over entry rows, headings are
  extra render lines — the row→line mapping must keep the cursor on screen
  (step 3 owns this; it is the fiddliest part).
- `serveview_test.go` asserts exact line indexes in several tests; step 3
  updates them deliberately, not incidentally.
- Rune input via `tea.KeyPressMsg.Text` must ignore non-text keys cleanly
  (step 4).

## Phases and steps

- **Phase 1 — storage and shape** (no visible change; suite green at end)
  - Step 1: `groups` in prefs — schema, validation, round-trip tests.
  - Step 2: grouped-section computation — pure functions + pruning helper.
- **Phase 2 — rendering** (suite green at end)
  - Step 3: headings in the entry list — interleave, window math, style,
    test updates.
- **Phase 3 — interactions** (suite green at end)
  - Step 4: notice-line name input mode.
  - Step 5: move-to-group pick (incl. `new group…` chaining into step 4's
    input).
  - Step 6: manage mode — reorder, rename, disband.

## End-to-end verification

After the merge, live in the real TUI (user involved — collaboration rhythm):

1. `go build` and run with the real config tree; list renders as before
   (no groups yet).
2. Move an entry to `new group…`, name it on the notice line; heading appears,
   entry files under it.
3. Move more entries incl. across groups and back to ungrouped; create a
   second group; reorder groups in manage mode; rename; disband one.
4. Restart cria — grouping, order and headings persist; toggle backend —
   only that backend's members show under each heading; a group left with
   only other-backend members hides.
5. Break `ui.json` by hand — loud reset to defaults, next change rewrites it.
6. Delete a grouped entry's `.toml` — list skips it; next prefs write prunes
   the id.
