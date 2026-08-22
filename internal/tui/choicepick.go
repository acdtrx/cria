package tui

import (
	"maps"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/picks"
)

// This file is the write side of an entry's choices: the picker a selection key
// opens over the list, one row per axis with that axis's options laid along it
// (docs/specs/TUI.md).
//
// It floats over the list's corner, sized to its rows, and leaves the detail
// pane beside it alone (serveScreen). Floating is the gesture's meaning:
// configuring the entry the cursor is on, with the list still visible around
// the box saying where that is. And the pane is what the picking is watched
// in — every ←/→ re-composes the command line under the axes — so covering it
// would break the one loop the picker exists for.
//
// Nothing here is held unsaved. Every ←/→ writes the store as it lands, so ⏎ and
// esc are two ways out of a mode with nothing to confirm, and leaving is never a
// discard — the doctrine the group modes are built on (managegroups.go). Picks
// are defaults "until I change them"; a launch that varies one combination and
// records nothing is `cria start <id> choice=option`, which is CLI territory.

// picksTitle names the pane while the picker stands in the list's place. It is
// the word the store, the CLI and the bar all use for the same thing.
const picksTitle = "picks"

// pickerFloor is the narrowest the floating box gets: a dialog sized purely to
// two short axes reads as clutter stuck to the list, and air is what says
// "this is a dialog" (user-chosen 2026-08-23). The pane it floats over can
// still be narrower — the pane's own width wins (pickerBox).
const pickerFloor = 40

// picker is one entry's axes being picked over: which entry, and which axis the
// cursor stands on. There is nothing else to keep — by the time the next key
// arrives, whatever the last one picked is already on disk.
//
// The entry is held by id rather than by value: the tree is re-read every couple
// of seconds under the open picker, and the axes drawn are the ones the file
// declares now.
type picker struct {
	entry  string
	cursor int
}

// openPicker is p: the highlighted entry's axes, with the cursor on the first
// one. A flat entry has none to show, so the key is neither drawn nor live there
// and pressing it does nothing (docs/specs/TUI.md, rebindContext).
//
// It asks nothing on the notice line. The pane names the entry, the rows carry
// the picks and the bar names the keys; a line repeating any of that would be
// one more thing to read that is already on screen (managegroups.go).
func (m model) openPicker() model {
	selected, ok := m.selectedRow()
	if !ok || selected.broken != nil || len(selected.entry.Choices) == 0 {
		return m
	}
	m.picker = &picker{entry: selected.entry.ID}
	return m.syncEscScope()
}

// pressInPicker is the keyboard while the picker is up: the cursor walks the
// axes, ←/→ picks along the one it is on, ⏎ and esc close. Nothing underneath
// acts — a start or a move would act on a list the picker is standing over.
func (m model) pressInPicker(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	entry, open := m.pickedEntry()
	if !open {
		// The entry lost its axes while the picker stood on them — the file
		// edited, or gone. There is nothing left to pick between.
		return m.leavePicker(), nil
	}

	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.closePicks), key.Matches(pressed, m.keys.leavePicks):
		return m.leavePicker(), nil
	case key.Matches(pressed, m.keys.pickUp):
		return m.stepChoice(entry, -1), nil
	case key.Matches(pressed, m.keys.pickDown):
		return m.stepChoice(entry, 1), nil
	case key.Matches(pressed, m.keys.prevOption):
		return m.rollPick(entry, -1), nil
	case key.Matches(pressed, m.keys.nextOption):
		return m.rollPick(entry, 1), nil
	}
	return m, nil
}

// stepChoice walks the cursor between the entry's axes, clamped at both ends the
// way every list here is. The list's own cursor is untouched: the entry the
// picker is about is the one the user is standing on, and it is still drawn that
// way when the picker closes.
//
// The value is copied rather than edited in place, as the other modes' cursors
// are: the frame travels by value, and a mode edited through its pointer would
// change the frame a message is still being handled against.
func (m model) stepChoice(entry config.Entry, by int) model {
	picking := *m.picker
	picking.cursor = clamped(m.picker.cursor+by, len(entry.Choices))
	m.picker = &picking
	return m
}

// rollPick moves the pick one option along the axis under the cursor, and writes
// it. The options wrap: this is a pick and not a carry — there is no order of
// the user's own to protect at the ends, and walking off the last option lands
// back on the first (docs/specs/TUI.md).
func (m model) rollPick(entry config.Entry, by int) model {
	choice, ok := m.pickedChoice(entry)
	if !ok {
		return m
	}

	// Where the pick stands now is read off the same merge the rows are drawn
	// from, so the option that moves is the one the user is looking at. A
	// selection naming nothing for this axis rolls from its first option, which
	// is where an unmarked row reads as standing.
	at, current := 0, m.picks(entry)[choice.Name]
	for i, option := range choice.Options {
		if option.Name == current {
			at = i
			break
		}
	}
	rolled := (at + by + len(choice.Options)) % len(choice.Options)
	return m.recordPick(entry.ID, choice.Name, choice.Options[rolled].Name)
}

// recordPick writes one pick and says nothing about it: the row it lands on
// carries the mark, and the detail pane beside it carries the command it
// composes — picking and seeing the command are one loop (docs/specs/TUI.md).
//
// Only the axis that moved is recorded. An axis still sitting on its config
// default stores nothing, which is exactly what an absent key means to the store
// (internal/picks) — and the store is rebuilt rather than written into, because
// picks travel by reference and a map edited in place would change what an
// earlier frame holds.
//
// The write is also where stale picks go: a pick whose entry or option the tree
// no longer holds is dropped by picks.Prune on the next write, and this is it.
//
// A write that fails is not the pick failing — the session holds the pick the
// user made, and what was lost is cria's memory of it — so the picker stays up
// showing it and the line under the box says so, exactly as a failed
// preferences write does (grouppick.go, recordGroups).
func (m model) recordPick(id, choice, option string) model {
	stored := make(picks.Picks, len(m.stored)+1)
	for entry, selection := range m.stored {
		stored[entry] = maps.Clone(selection)
	}
	if stored[id] == nil {
		stored[id] = config.Selection{}
	}
	stored[id][choice] = option

	m.stored = picks.Prune(stored, m.tree)
	m.alert = alert{}
	if err := picks.Save(m.root, m.stored); err != nil {
		m.alert = alert{text: err.Error(), bad: true}
	}
	return m
}

// leavePicker drops the mode, on ⏎ or on esc. There is nothing to confirm and
// nothing to throw away — every pick was written as it was made — so the two
// keys mean the same thing, and neither is an answer to a question.
//
// The notice line keeps whatever it is carrying: a write that failed is the one
// thing on screen saying so, and esc's next meaning is dismissing it (tui.go).
func (m model) leavePicker() model {
	m.picker = nil
	return m.syncEscScope()
}

// closeStalePicker drops a picker whose entry the tree no longer offers axes
// for. An entry file edited while cria is open is the expected way one changes
// (docs/cria.md, principle 5), and every read of the tree passes through here,
// so the mode never stands over choices that are gone.
func (m model) closeStalePicker() model {
	if m.picker == nil {
		return m
	}
	if _, open := m.pickedEntry(); open {
		return m
	}
	return m.leavePicker()
}

// pickedEntry is the entry the picker is open on, as the tree declares it now.
// It is re-read rather than remembered, like the move's target and the armed
// key's rows: what the picker draws and what it writes are the file as it
// stands.
func (m model) pickedEntry() (config.Entry, bool) {
	if m.picker == nil {
		return config.Entry{}, false
	}
	entry, found := m.entryNamed(m.picker.entry)
	if !found || len(entry.Choices) == 0 {
		return config.Entry{}, false
	}
	return entry, true
}

// pickedChoice is the axis the cursor stands on right now.
func (m model) pickedChoice(entry config.Entry) (config.Choice, bool) {
	if m.picker == nil || len(entry.Choices) == 0 {
		return config.Choice{}, false
	}
	return entry.Choices[clamped(m.picker.cursor, len(entry.Choices))], true
}

// pickerBox is the floating box serveScreen centers over the list: the entry
// it is about in the title, one row per axis, sized to its own rows rather
// than to the pane it covers. An entry whose axes have gone draws an empty
// box — the next keypress closes the mode, and a read of the tree closes it
// before that (closeStalePicker).
//
// width and rows are the list's own rectangle. The box's width is the widest
// row's, clamped a cell inside the pane's; the height is one line per axis,
// windowed on the cursor when the pane is shorter than the entry has axes.
func (m model) pickerBox(width, rows int) string {
	title := paneTitle(picksTitle + titleSeparator + m.picker.entry)
	entry, open := m.pickedEntry()
	if !open {
		return pane(title, minWidth, sizeLines(nil, 1))
	}

	// One reading of the picks draws every row, and it is the same reading the
	// detail pane beside the box composes its command line from (m.picks).
	selection := m.picks(entry)
	cursor := clamped(m.picker.cursor, len(entry.Choices))
	column := choiceColumn(entry.Choices)

	paints := make([]rowPaint, 0, len(entry.Choices))
	rows2 := make([]string, 0, len(entry.Choices))
	widest := 0
	for at, choice := range entry.Choices {
		paint, row := choiceRow(choice, selection, at == cursor, column)
		widest = max(widest, lipgloss.Width(row))
		paints = append(paints, paint)
		rows2 = append(rows2, row)
	}

	boxWidth := min(max(widest+4, lipgloss.Width(title)+6, pickerFloor), width-2)
	lines := make([]string, len(rows2))
	for i, row := range rows2 {
		lines[i] = paints[i].fill(row, boxWidth-4)
	}
	capacity := min(len(lines), max(rows-2, 1))
	return pane(title, boxWidth, sizeLines(window(lines, cursor, capacity), capacity))
}

// choiceColumn is how wide the axis-name column has to be for every name to fit
// in it, so the options start at one x down the rows and a row is read across
// rather than parsed.
func choiceColumn(choices []config.Choice) int {
	column := 0
	for _, choice := range choices {
		column = max(column, lipgloss.Width(choiceLabel(choice)))
	}
	return column
}

// choiceLabel is how a row names its axis. The colon is what holds the author's
// own word apart from the options after it.
func choiceLabel(choice config.Choice) string { return choice.Name + ":" }

// choiceRow is one axis as a row of the picker, unfilled so the box can size
// itself to the widest before padding any (pickerBox): the name, then the
// options with the current pick on the mauve chip (pickedStyle) and the rest as
// the context around it — the detail pane's hierarchy, painted like a list row.
// The chip keeps its own background on the cursor's band: the chip is what the
// axis is set to, the band is where the cursor is.
//
// Every option wears the chip's footprint — a cell of padding either side —
// whether or not it is the pick: rolling ←/→ must move the lit background along
// a row that stands still, never reflow the row under the eye that is reading
// it.
//
// A row wider than the box loses its tail rather than wrapping: a picker's row
// is one axis, and ←/→ walk it whether or not every option fits on screen.
func choiceRow(choice config.Choice, selection config.Selection, cursor bool, column int) (rowPaint, string) {
	paint := paintFor(cursor)
	pieces := []string{paint.cell(choiceLabel(choice), paint.quiet(), column)}
	for _, option := range choice.Options {
		if option.Name == selection[choice.Name] {
			pieces = append(pieces, pickedStyle.Render(option.Name))
			continue
		}
		pieces = append(pieces, paint.quiet().Render(" "+option.Name+" "))
	}
	return paint, paint.marker() + paint.join(pieces...)
}
