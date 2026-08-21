# Step 4 — notice-line name input

Status: pending

## Intent

A minimal typed-text mode rendered on the reserved notice line — the TUI's
first free-text input. Built standalone so steps 5 and 6 can chain into it.

## Files likely touched

- `internal/tui/naming.go` (new) + `internal/tui/naming_test.go` (new)
- `internal/tui/tui.go` (routing precedence, `groups()`, `syncEscScope()`)
- `internal/tui/keybar.go` (scope label)

## Decisions (from planning)

- State: `m.naming *naming`, nil when off. Carries the prompt ("new group" /
  "rename group"), the text (prefillable for rename), and what to do on
  confirm (the arming step owns the commit — naming only collects the name
  and validates it).
- Input: `tea.KeyPressMsg` — append `Text` when non-empty (ignores pure
  modifier/special keys), backspace removes the last rune, ⏎ confirms, esc
  cancels. No cursor movement, no selection — a name field, not an editor.
- Render: on the notice line as `<prompt>: <text>▌`, replacing any alert while
  active (the row is already permanently reserved — zero layout shift).
- Validation at ⏎, refused in place via the notice line: empty name; name
  colliding with an existing group (exact match). `ungrouped` is reserved and
  refused as a group name (it names the implicit section).
- Precedence: naming routes keyboard before pick in `press()`; it takes no
  screen space so `screen()` is untouched; `groups()` returns a naming scope
  (⏎ confirm / esc cancel) + global, per the pick precedent;
  `walksTheCache()` unaffected (no overlay pane).

## Acceptance criteria

- Unit tests: typing accumulates runes (incl. multi-byte), backspace,
  confirm hands the trimmed name to the arming context, esc cancels cleanly,
  empty/duplicate/reserved names are refused with the notice and the mode
  stays active for correction.
- Keybar shows the naming scope while active; suite green expected (pure
  addition — nothing arms the mode yet, which is fine mid-phase).

## Result

(recorded on completion)
