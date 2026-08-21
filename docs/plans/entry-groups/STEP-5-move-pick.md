# Step 5 — move-to-group pick

Status: pending

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

(recorded on completion)
