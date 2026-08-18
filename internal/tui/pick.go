package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"cria/internal/serve"
)

// This file answers "which server?".
//
// The server keys act on what the status box shows (docs/specs/TUI.md), and the
// box shows every record cria holds — several at once whenever entries declare
// different ports (docs/cria.md, v1 surface). One server the key can act on is
// the whole answer and it acts. With more than one, picking on the user's behalf
// is the one thing it must not do: the key arms itself instead, the box becomes
// its picker, and ⏎ is the answer.
//
// The mode holds the keyboard the way the modal and the log do — what the bar
// draws is what works — and it keeps no records of its own: the rows a key can
// act on are re-read from the current listing every time they are needed, so a
// server that exits under the cursor moves the cursor rather than the action.

// pickAction is the server key waiting for its target.
type pickAction int

const (
	pickStop pickAction = iota
	pickKill
	pickLog
	pickDismiss
	pickRestart
)

// pick is one armed key: what it will do, and where its cursor stands.
type pick struct {
	action pickAction
	cursor int // a position among the rows the action can act on, not among the box's
}

// aim is a server key before it knows which server it means: it acts when only
// one row can answer for it, and arms itself when several can. No row at all
// cannot be reached — the key is not on the bar then, and a key the bar does not
// draw does nothing when pressed (tui.go).
func (m model) aim(action pickAction) (tea.Model, tea.Cmd) {
	rows := m.pickable(action)
	switch len(rows) {
	case 0:
		return m, nil
	case 1:
		return m.carryOut(action, m.listing.Servers[rows[0]].Record)
	}

	m.pick = &pick{action: action}
	m.keys.runPick.SetHelp("⏎", action.verb())
	// The question is exactly what the boxes cannot show — which of several
	// servers a key is about to mean — so the line under the box asks it (tui.go).
	m.alert = alert{text: action.prompt()}
	return m.syncEscScope(), nil
}

// aimRestart is r. Restart is the one server key with a target when nothing is
// running — the box shows the entry that was started last, and starting it again
// is the whole of a restart there (docs/specs/TUI.md). With servers cria can see,
// it is a key like the others: one is the answer, several ask which.
func (m model) aimRestart() (tea.Model, tea.Cmd) {
	if len(m.pickable(pickRestart)) == 0 {
		return m.restartShownEntry()
	}
	return m.aim(pickRestart)
}

// carryOut is the armed action on the server it landed on. Every key keeps its
// own function: what is decided here is which server, never what happens to it.
func (m model) carryOut(action pickAction, record serve.Record) (tea.Model, tea.Cmd) {
	switch action {
	case pickStop:
		return m.stopServer(record)
	case pickKill:
		return m.killServer(record)
	case pickDismiss:
		return m.dismissRecord(record)
	case pickRestart:
		return m.restartServer(record)
	}
	return m.showLog(record)
}

// pressInPick is the keyboard while a key is asking which server it means: move
// between the rows it can act on, run it on the one under the cursor, or leave
// it. Nothing underneath acts — a key that answered something else would answer
// a question the user is in the middle of.
func (m model) pressInPick(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if len(m.pickable(m.pick.action)) == 0 {
		// Everything the key could have acted on has gone since it was armed.
		// There is nothing left to pick, so the question goes with it.
		return m.leavePick(), nil
	}

	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.cancelPick):
		return m.leavePick(), nil
	case key.Matches(pressed, m.keys.pickUp):
		return m.movePick(-1), nil
	case key.Matches(pressed, m.keys.pickDown):
		return m.movePick(1), nil
	case key.Matches(pressed, m.keys.runPick):
		action := m.pick.action
		record, picked := m.pickedRecord()
		if !picked {
			return m.leavePick(), nil
		}
		return m.leavePick().carryOut(action, record)
	}
	return m, nil
}

// movePick moves the cursor between the rows the armed action can act on, and
// nowhere else: a row the key cannot answer for is not a place the cursor can
// stand. The lists' own cursors are untouched — the user is still standing where
// they were when the key was pressed.
func (m model) movePick(by int) model {
	moved := *m.pick
	moved.cursor = clamped(m.pick.cursor+by, len(m.pickable(m.pick.action)))
	m.pick = &moved
	return m
}

// leavePick drops the mode, picked or cancelled. The box's cursor is the
// question's own state and it goes with the question, as does the line that
// asked it.
func (m model) leavePick() model {
	m.pick = nil
	m.alert = alert{}
	return m.syncEscScope()
}

// pickable is the rows one action can be pointed at, as positions in the listing
// the box draws in that order.
func (m model) pickable(action pickAction) []int {
	var rows []int
	for i, status := range m.listing.Servers {
		if action.answers(status.Phase) {
			rows = append(rows, i)
		}
	}
	return rows
}

// answers reports whether a server in this phase is something the action can be
// pointed at: stop, kill and restart mean a server cria can still see — a
// restart of one is its stop and its start — dismiss means a crash report, and
// the log is the one thing every record has either way (docs/specs/SERVE.md).
func (a pickAction) answers(phase serve.Phase) bool {
	exited := phase == serve.PhaseExited
	switch a {
	case pickStop, pickKill, pickRestart:
		return !exited
	case pickDismiss:
		return exited
	}
	return true
}

// pickedRecord is the server the cursor is on right now.
func (m model) pickedRecord() (serve.Record, bool) {
	row, picked := m.pickedRow()
	if !picked {
		return serve.Record{}, false
	}
	return m.listing.Servers[row].Record, true
}

// pickedRow is where the cursor is standing, as a position in the listing. The
// eligible rows are re-read here rather than remembered: the listing behind the
// cursor is re-observed every couple of seconds, and this is the reading that
// says what the cursor points at now.
func (m model) pickedRow() (int, bool) {
	if m.pick == nil {
		return 0, false
	}
	rows := m.pickable(m.pick.action)
	if len(rows) == 0 {
		return 0, false
	}
	return rows[clamped(m.pick.cursor, len(rows))], true
}

// boxCursor is how the status box draws the pick: the row on the band, and the
// width the band has to span. Nothing armed is the box as it is drawn the rest
// of the time.
func (m model) boxCursor() boxCursor {
	row, picked := m.pickedRow()
	if !picked {
		return boxCursor{}
	}
	return boxCursor{picking: true, row: row, inner: m.frameWidth() - 4}
}

// verb is what the armed action does, in the one word both the bar and the
// question spell it with.
func (a pickAction) verb() string {
	switch a {
	case pickStop:
		return "stop"
	case pickKill:
		return "kill"
	case pickDismiss:
		return "dismiss"
	case pickRestart:
		return "restart"
	}
	return "log"
}

// prompt is the question the line under the box asks while the action is armed.
func (a pickAction) prompt() string { return "which server to " + a.verb() }

// scope is what the bar calls the mode: the question itself, so the ⏎ beside it
// reads as the answer to it.
func (a pickAction) scope() string { return a.verb() + " which" }
