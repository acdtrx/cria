package tui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// This file answers "which group?" — the one key that files an entry, and the
// only way a group is ever created (docs/specs/TUI.md).
//
// Moving is the front door: membership is a UI preference, so an entry is filed
// by pointing at a heading rather than by editing anything. The key arms itself
// over the headings the way a server key arms itself over the status box
// (pick.go), and for the same reason — choosing on the user's behalf is the one
// thing it must not do. One answer acts immediately there too: a list with no
// groups yet can only be answered with a group to name, so the key opens the
// name input rather than a pick of one.
//
// The mode keeps no list of its own: which headings can answer is re-read from
// the preferences every time it is needed, so the question follows the list
// rather than a copy of it taken when the key was pressed.

// moveTarget is one answer to the question: a group the preferences already
// hold, named by its position in them, or one of the two answers that are no
// group in that list — the tail, and the group that does not exist yet.
type moveTarget int

const (
	moveToNewGroup  moveTarget = -2
	moveToUngrouped moveTarget = -1
)

const (
	// newGroupLabel is the answer with no heading of its own, drawn at the tail
	// of the list. The ellipsis is what says it asks something rather than files
	// something.
	newGroupLabel = "new group…"

	// newGroupPrompt is what the notice line asks for once that answer is picked
	// (naming.go).
	newGroupPrompt = "new group"
)

// move is one entry waiting for its group: which entry, and where the cursor
// stands among the headings that can take it.
//
// The entry is held by id rather than read back off the list's cursor when the
// answer lands: the tree is re-read every couple of seconds under the armed
// question, and the entry the line names is the entry the answer files, whatever
// the list does underneath.
type move struct {
	entry  string
	cursor int // a position among the headings that can answer, not among the drawn ones
}

// aimMove is m: the entry under the cursor asking which group it belongs to.
// Every group but its own can answer, the tail can when it is in one, and there
// is always the group that does not exist yet — so the question has an answer
// even on a list nobody has grouped.
func (m model) aimMove() (tea.Model, tea.Cmd) {
	selected, ok := m.selectedRow()
	if !ok || selected.broken != nil {
		return m, nil
	}

	id := selected.entry.ID
	if len(m.moveTargets(id)) == 1 {
		// Nothing to pick between: the only answer is a group to name, and a
		// one-item pick would ask a question with one button on it.
		return m.askNewGroup(id), nil
	}

	m.move = &move{entry: id}
	// The question is exactly what the list cannot show — which heading this
	// entry is about to live under — so the line under the box asks it (tui.go).
	m.alert = alert{text: "move " + id + " to which group"}
	return m.syncEscScope(), nil
}

// pressInMove is the keyboard while an entry is waiting to be filed: move
// between the headings that can take it, file it under the one the cursor is on,
// or leave the question. Nothing underneath acts — a key that answered something
// else would answer a question the user is in the middle of.
func (m model) pressInMove(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.cancelMove):
		return m.leaveMove(), nil
	case key.Matches(pressed, m.keys.pickUp):
		return m.moveCursor(-1), nil
	case key.Matches(pressed, m.keys.pickDown):
		return m.moveCursor(1), nil
	case key.Matches(pressed, m.keys.runMove):
		entry := m.move.entry
		target, picked := m.pickedTarget()
		if !picked {
			return m.leaveMove(), nil
		}
		if target == moveToNewGroup {
			// The input opens over the armed question rather than in place of
			// it: esc there is a step back to the headings, not out of the move.
			return m.askNewGroup(entry), nil
		}
		return m.leaveMove().fileEntry(entry, target), nil
	}
	return m, nil
}

// moveCursor moves between the headings that can answer, and nowhere else: the
// group the entry is already in is not a place the cursor can stand. The list's
// own cursor is untouched — the user is still standing on the entry they pressed
// the key on, and it stays drawn that way.
func (m model) moveCursor(by int) model {
	moved := *m.move
	moved.cursor = clamped(m.move.cursor+by, len(m.moveTargets(m.move.entry)))
	m.move = &moved
	return m
}

// leaveMove drops the mode, filed or cancelled. The cursor over the headings is
// the question's own state and it goes with the question, as does the line that
// asked it.
func (m model) leaveMove() model {
	m.move = nil
	m.alert = alert{}
	return m.syncEscScope()
}

// askNewGroup is the answer that has no heading yet: the name input opens, and
// what it comes back with is one write — the group created at the end of the
// order with the entry already in it. The move stays armed underneath, so a
// cancelled name is a step back to the headings.
func (m model) askNewGroup(id string) model {
	return m.askName(newGroupPrompt, "", func(m model, name string) (tea.Model, tea.Cmd) {
		return m.leaveMove().fileNewGroup(id, name), nil
	})
}

// moveTargets is where one entry could go, in the order the list draws the
// answers: every group but the one holding it — filing something where it
// already is means nothing — then the tail when it is in a group at all, then
// the group that does not exist yet.
func (m model) moveTargets(id string) []moveTarget {
	holder := m.groupOf(id)

	targets := make([]moveTarget, 0, len(m.prefs.Groups)+2)
	for at := range m.prefs.Groups {
		if moveTarget(at) != holder {
			targets = append(targets, moveTarget(at))
		}
	}
	if holder != moveToUngrouped {
		targets = append(targets, moveToUngrouped)
	}
	return append(targets, moveToNewGroup)
}

// groupOf is the group an entry is filed in, as a position in the preferences,
// and the tail when no group holds it.
func (m model) groupOf(id string) moveTarget {
	for at, group := range m.prefs.Groups {
		if slices.Contains(group.Entries, id) {
			return moveTarget(at)
		}
	}
	return moveToUngrouped
}

// pickedTarget is the heading the cursor is standing on right now. The answers
// are re-read here rather than remembered: the preferences behind the question
// are what say which headings there are, and this is the reading that says what
// the cursor points at now.
func (m model) pickedTarget() (moveTarget, bool) {
	if m.move == nil {
		return moveToNewGroup, false
	}
	targets := m.moveTargets(m.move.entry)
	return targets[clamped(m.move.cursor, len(targets))], true
}

// headingCursor is how the entry list draws a mode standing on the headings:
// which heading is on the band, and which headings the mode makes visible that
// the list would otherwise hide. Nothing up is the list as it is drawn the rest
// of the time.
//
// Two modes can be standing there — a move asking which group takes an entry,
// and the manage mode rearranging the groups themselves (managegroups.go) — and
// the pane asks the frame for exactly one of these, so what differs between
// them is said in these fields rather than in a second way of drawing the list.
type headingCursor struct {
	up        bool
	picked    moveTarget
	ungrouped bool // the tail is one of the answers, so its heading is drawn
	newGroup  bool // the group that does not exist yet closes the list
	held      bool // the picked heading is in the air, so it rides the carry band
}

// headingCursor reads that off the frame.
func (m model) headingCursor() headingCursor {
	if at, managing := m.managedGroup(); managing {
		// Managing stands on the groups and only the groups: the tail is not
		// one of them, and a group is created by filing an entry into it rather
		// than from here (managegroups.go).
		return headingCursor{up: true, picked: moveTarget(at), held: m.manage.carrying}
	}

	picked, filing := m.pickedTarget()
	if !filing {
		return headingCursor{}
	}
	return headingCursor{
		up:        true,
		picked:    picked,
		ungrouped: slices.Contains(m.moveTargets(m.move.entry), moveToUngrouped),
		newGroup:  true,
	}
}

// draws reports whether the mode puts a heading on screen the list would have
// left off. Every group's heading is drawn while one is up, this backend's
// members or not: the headings are what is being pointed at, and a group the
// user cannot see is a group they cannot point at. The tail's heading is drawn
// when the tail is one of the answers.
func (h headingCursor) draws(target moveTarget) bool {
	switch {
	case !h.up:
		return false
	case target == moveToUngrouped:
		return h.ungrouped
	}
	return true
}

// on reports whether the cursor is standing on this heading.
func (h headingCursor) on(target moveTarget) bool { return h.up && h.picked == target }

// sectionTarget is which answer a drawn section stands for: the groups come in
// the preferences' own order and the tail is the last section, exactly as
// entrySections lays them out (groups.go).
func sectionTarget(section, sections int) moveTarget {
	if section == sections-1 {
		return moveToUngrouped
	}
	return moveTarget(section)
}

// fileEntry is the answer landed: the entry leaves whatever group held it and
// joins the one the cursor was on — or the tail, which is no group at all.
func (m model) fileEntry(id string, target moveTarget) model {
	return m.recordGroups(filedInto(m.prefs.Groups, id, target)).followEntry(id)
}

// fileNewGroup is the answer that had no heading: the group is created at the
// end of the order — new groups go last, since the order is the user's own and
// nothing else may reshuffle it — and the entry is filed into it in the same
// write.
func (m model) fileNewGroup(id, name string) model {
	filed := filedInto(m.prefs.Groups, id, moveToNewGroup)
	return m.recordGroups(append(filed, entryGroup{Name: name, Entries: []string{id}})).followEntry(id)
}

// filedInto is the group list with one entry in one group and nowhere else — an
// entry belongs to at most one group (docs/specs/TUI.md), so the move out and
// the move in are one pass. A target no group holds is how "into no group at
// all" is spelled: the tail, and the group that is about to be appended.
//
// The lists are built rather than edited: preferences travel by value through
// this package, so several copies share one backing array and an append in place
// would edit a list still being read elsewhere (groups.go, pruneGroups).
func filedInto(groups []entryGroup, id string, target moveTarget) []entryGroup {
	filed := make([]entryGroup, 0, len(groups)+1)
	for at, group := range groups {
		entries := make([]string, 0, len(group.Entries)+1)
		for _, held := range group.Entries {
			if held != id {
				entries = append(entries, held)
			}
		}
		if moveTarget(at) == target {
			entries = append(entries, id)
		}
		filed = append(filed, entryGroup{Name: group.Name, Entries: entries})
	}
	return filed
}

// recordGroups writes one change to the grouping and says nothing about it: the
// headings are the report, and a line repeating what they show would still be
// there three keypresses later (tui.go). Every key that changes a group goes
// through here, filing or managing (managegroups.go), so no change to the
// grouping is ever held only on screen.
//
// The write is where memberships are tidied: ids the tree no longer holds a file
// for go out with it (groups.go). A write that fails is not the change failing —
// the session has the grouping the user asked for, and what was lost is cria's
// memory of it — so that one still speaks, exactly as the backend toggle does.
func (m model) recordGroups(groups []entryGroup) model {
	m.prefs.Groups = pruneGroups(groups, m.tree)
	m.alert = alert{}
	if err := savePrefs(m.root, m.prefs); err != nil {
		m.alert = alert{text: err.Error(), bad: true}
	}
	return m
}

// followEntry keeps the cursor on the entry that was just filed: the list has
// reordered under it, and where the user wants to be standing is the entry they
// moved rather than whatever took its place.
func (m model) followEntry(id string) model {
	for at, listed := range m.rows() {
		if listed.broken == nil && listed.entry.ID == id {
			return m.reselect(at)
		}
	}
	return m.reselect(m.cursor())
}
