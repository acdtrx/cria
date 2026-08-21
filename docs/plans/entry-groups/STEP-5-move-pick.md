# Step 5 — move-to-group pick

Status: done

## Intent

The front door: a selection key arms a pick over the group headings; ⏎ files
the highlighted entry, `new group…` chains into step 4's name input. First
step where prefs are written by a group action.

## Files likely touched

- `internal/tui/grouppick.go` (new) + `internal/tui/grouppick_test.go` (new)
- `internal/tui/serveview.go` (render the armed state)
- `internal/tui/tui.go` (keymap, `press()` precedence, `groups()`,
  `rebindContext()`, `syncEscScope()`)
- `internal/tui/keybar.go` (scope label)

## Decisions (from planning)

- Key: `m` (move), selection scope — enabled for entry rows only; disabled on
  broken rows and in the cache view (`rebindContext`).
- Targets, in display order: every prefs group (hidden headings become visible
  while armed — the pick needs all of them), then `ungrouped` (only when the
  selected entry is currently grouped), then `new group…`. The current group
  of the entry is excluded (moving somewhere it already is means nothing).
- Zero groups + ungrouped entry: the only target is `new group…` — arming
  jumps straight into the name input, no one-item pick (mirrors the status
  pick's "one candidate acts immediately").
- Like `pick.go`, the armed state stores an action + cursor only; targets are
  re-derived every keystroke/draw. j/k moves over target headings in the
  list itself, ⏎ commits, esc cancels; the notice line asks
  "move <id> to which group". The entry cursor and `m.selected` stay put.
- Commit: remove the id from its old group (if any), append to the target's
  `Entries` (order carries no meaning), `pruneGroups` against the tree, then
  `savePrefs` — silent on success, alert on failure, per the
  `switchBackend`/`started()` pattern.
- `new group…` + ⏎: open the name input ("new group"); its confirm creates
  the group at the **end** of the group order and files the entry in one
  prefs write; esc from the name input returns to the armed pick.
- Precedence: the group pick routes after naming, before the status pick, in
  all three chains; `syncEscScope()` learns the new esc meaning.

## Acceptance criteria

- Tests: move to another group, to `ungrouped`, to a brand-new group via the
  naming chain (one prefs write, group appended last); hidden headings appear
  while armed and re-hide after; esc cancels with prefs untouched; `m`
  disabled on broken rows; zero-groups arming goes straight to naming;
  dangling ids pruned on the commit write.
- Suite green expected (additions only).

## Result

Built as planned. `m` (selection scope, entry rows only) arms `model.move`
(`grouppick.go`): the entry's id and a cursor over `moveTargets`, re-derived from
the preferences every keystroke and draw. j/k walk the headings, ⏎ files, esc
cancels; `new group…` chains into step 4's input and creates the group at the end
of the order with the entry in it, in one write. `listLines` draws every group's
heading while armed (plus `ungrouped` when it can answer and `new group…` at the
tail), bands the picked one and windows on it, and is byte-identical to before
when nothing is armed.

Deviations and decisions taken during the build:

- **Cursor keys are the pick's own pair** (`pickUp`/`pickDown`) rather than a
  third identical pair. They are never drawn and never disabled — the reason
  `newKeymap` gives for the box having its own pair is enablement scope, and an
  armed question is an armed question. ⏎ and esc are the move's own bindings
  (`runMove`/`cancelMove`), since the bar's words here are fixed ("move") rather
  than set at arming the way the server pick's are.
- **The band needed a heading tone.** Step 3 recorded that `headingHex` had no
  band reading; the picked heading is one, so `bandHeadingHex` (#9b93bb, 5.12:1
  on the band) joins the palette table and `rowPaint.heading()`. The band spans
  the pane but carries no marker — a heading picked is still not a row.
- **The armed entry is held by id**, not re-read off `m.selected` when ⏎ lands:
  the tree is re-read every couple of seconds under the question, and the entry
  the line names is the entry the answer files.
- **The cursor follows the entry it filed** (`followEntry`): the list reorders
  under the move, and the row worth standing on afterwards is the one that was
  moved rather than whatever took its place.
- `syncEscScope` now treats an armed move like a name being typed — esc belongs
  to the mode, so neither the alert nor the way out of a view claims it.
- `walksTheCache()` verified unchanged: a move is not a screen over the list, so
  the dots under the question keep being walked.

Suite: `go test ./...` green (11 new tests in `grouppick_test.go`); `gofmt -l .`
silent; `go vet ./...` clean. Two mutations were run to check the tests bite
(offering the entry's own group; leaving hidden headings hidden while armed) —
both failed the suite as they should.
