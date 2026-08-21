# Step 6 — manage mode

Status: done

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

Built as planned, on step 5's machinery rather than beside it. `g` (global
scope, serve view, only with a group to manage and a drawn list to manage it on)
opens `model.manage` (`managegroups.go`): a cursor over the groups, held as a
position in the preferences and re-read every keystroke and draw. j/k walk the
headings, `K`/`J` carry the group under the cursor up and down the order, `r`
renames through step 4's input prefilled with the current name, `d` disbands,
esc leaves. Every mutation writes as it lands, so the mode holds nothing
unsaved.

What was reused rather than rebuilt:

- **One heading cursor, one render path.** `headingCursor()` now answers for
  either mode; `listLines` asks the frame for it exactly as before. The struct's
  `filing` became `up` (two modes stand there now) and gained `newGroup`, so the
  `new group…` tail line is gated on the move that offers it rather than on any
  heading cursor. `draws`/`on`, `headingLine`, `bandHeadingStyle`,
  `sectionTarget` and the window math are untouched.
- **`recordGroups` split** as the brief called for: it now prunes, assigns and
  saves only — silent on success, alert on failure — and `followEntry` moved to
  the move's two call sites, which are the only places an entry is what changed.
- `askName`'s `opened` exemption is what makes a rename an editor: confirming
  the name unchanged is accepted and then recorded as nothing.

Decisions taken during the build:

- **The disband counts the entries that actually come back**, not every id the
  group held. The same write that drops the group drops ids the tree has no file
  for (`pruneGroups`), so counting those would promise rows nobody can find in
  the tail — the count is taken by running that same prune over the group being
  disbanded. Named deviation from the brief's "the ids the group held": with the
  test fixture the two differ (daily holds three ids, two entries return).
- **After a disband the cursor holds its position in the order** — the group
  that took the disbanded one's place, or the last one when the tail went — and
  **the mode ends when the last group goes**, leaving its notice on the line.
  `leaveGroups` deliberately does not clear the notice: esc's next meaning is
  dismissing it, which is the frame's own esc order.
- **The entry cursor keeps its row index** rather than following the entry it
  was on. The move follows because the move is *about* that entry; a group
  reorder is not, and `m.selected` staying put is what "the mode never took the
  list's cursor" means.
- **A keypress that moves nothing records nothing**: `K` on the first group and
  `J` on the last leave the file alone rather than rewriting the same order.
- **The mode says nothing on the notice line** when it opens. The move writes
  its question there because which entry is being filed cannot be seen; here the
  cursor is on the group and the bar names the keys, so the line stays free for
  the one outcome that has to be reported.
- `esc` is spelled "done" rather than "cancel" in the bar — there is nothing to
  cancel — and the reorder keys are the cursor pair shifted, so carrying a group
  reads as the same gesture as moving over one. Scope label: `groups`.
- `syncEscScope` learned the mode. Nothing currently reads the difference (the
  bar draws only the mode's own scope while one is up), but the invariant the
  function documents is that a mode owns esc, and the test asserts the key state
  rather than only the bar.

Suite: `go test ./... -count=1` fully green — every package `ok`, 14 new tests in
`managegroups_test.go`; `gofmt -l .` silent; `go vet ./...` clean. Four mutations
were run to check the tests bite (offering `new group…` while managing; leaving
the cursor behind a carried group; counting the ids the group held; enabling `g`
off the entry list; dropping the mode from `syncEscScope`) — each failed the
suite as it should.

Contract nuances settled here and recorded in `docs/specs/TUI.md` in the same
commit: every group shown while the mode is up and the tail never a stop; the
reorder clamp; the rename input opening on the current name; what the disband
notice counts; no confirmation for a disband; every change written as it lands
so leaving is never a discard; the mode ending with its last group.
