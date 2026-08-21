# Step 4 — notice-line name input

Status: done

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

`internal/tui/naming.go` holds the mode; nothing arms it yet (steps 5 and 6 do).

The seam steps 5 and 6 plug into:

- Arm: `m.askName(prompt, prefill, commit)` → `model`. Prefill is `""` for a
  creation and the current name for a rename.
- Commit: `type nameCommit func(m model, name string) (tea.Model, tea.Cmd)` —
  it takes the frame rather than closing over one, and is called with the input
  already gone and the name already trimmed and validated. The commit owns
  everything after that (prefs write, alert, cursor), so naming.go knows nothing
  about groups beyond what a name may be.
- Refusal: `m.refuseName(name)` answers the reason or `""`; a refused ⏎ leaves
  the mode up with the text as typed and the reason after it on the same line,
  cleared by the next keystroke. Rules: trimmed-empty, exact collision with an
  existing group, and the `ungroupedHeading` word. A name equal to the one the
  input opened on is not a collision with itself, so a rename confirmed
  unchanged is a no-op rather than a refusal.

Decisions taken during implementation:

- The input never writes to `m.alert`; it stands in front of the notice line and
  carries its own refusal, so cancelling puts back whatever the line was already
  saying.
- `q` is a letter while a name is typed, so the bar spells the one key that still
  leaves: a second quit binding (`quitTyping`, ctrl+c, drawn `^C quit`) replaces
  the global scope's `q quit` for the duration.
- `walksTheCache()` needed no change, as planned: the input takes no screen, so
  the entry list behind it still reads the walk.

Verification: `go test ./... -count=1` fully green (10 packages);
`gofmt -l .` prints nothing; `go vet ./...` clean.
