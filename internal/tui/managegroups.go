package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// This file is the rare half of grouping: the order the groups are drawn in,
// the name one carries, and the end of one. They are kept off the main gesture
// on purpose — filing an entry is the daily action and it gets a selection key
// (grouppick.go), while an order is arranged once and then left alone
// (docs/specs/TUI.md).
//
// The mode stands on the headings the way the move does, and for the same
// reason: a group *is* a heading on the list, so it is pointed at rather than
// named in a form. It takes the move's cursor over them whole — the list asks
// the frame for one heading cursor, and either mode can be what answers.
//
// Nothing here is held unsaved. Every key that changes something writes the
// preferences as it lands, so leaving is a way out of the mode and never a
// discard: there is no version of the order that exists only on screen.

// renamePrompt is what the notice line asks for when a group is renamed
// (naming.go). The input opens on the name the group already has, so the rare
// case of a whole new name costs one more keypress than the common case of
// correcting the one that is there.
const renamePrompt = "rename group"

// manage is the group order being worked on: where the cursor stands among the
// groups the preferences hold. There is nothing else to keep — by the time the
// next key arrives, whatever the last one did is already on disk.
type manage struct {
	cursor int
}

// openGroups is g: the headings become what the cursor is on, every group drawn
// whether this backend has anything under it or not — a group that cannot be
// seen is a group that cannot be reordered.
//
// The key is offered only where there is a group to manage. Groups come into
// being through the move and only through it, so an empty order is not a state
// this mode has anything to say about.
//
// It asks nothing on the notice line. The move writes its question there
// because which entry is being filed is the one thing the list cannot show;
// here the cursor is on the group, the bar names what the keys do, and a line
// repeating either would be one more thing to read that is already on screen
// (tui.go).
func (m model) openGroups() model {
	if len(m.prefs.Groups) == 0 {
		return m
	}
	m.manage = &manage{}
	return m.syncEscScope()
}

// pressInManage is the keyboard while the groups are being managed: the cursor
// walks the headings, K and J carry the group under it through the order, r
// renames it and d disbands it. Nothing underneath acts — the list is being
// rearranged, and a key meant for a row would land on a list that is moving.
func (m model) pressInManage(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.leaveGroups):
		return m.leaveGroups(), nil
	case key.Matches(pressed, m.keys.pickUp):
		return m.stepManaged(-1), nil
	case key.Matches(pressed, m.keys.pickDown):
		return m.stepManaged(1), nil
	case key.Matches(pressed, m.keys.raiseGroup):
		return m.carryGroup(-1), nil
	case key.Matches(pressed, m.keys.lowerGroup):
		return m.carryGroup(1), nil
	case key.Matches(pressed, m.keys.renameGroup):
		return m.askGroupName(), nil
	case key.Matches(pressed, m.keys.disbandGroup):
		return m.disbandGroup(), nil
	}
	return m, nil
}

// stepManaged walks the cursor over the group headings, and over nothing else:
// the ungrouped tail is where entries with no group render, not a group, so
// there is nothing about it to reorder, rename or disband
// (docs/specs/TUI.md). The list's own cursor is untouched — the user is still
// standing on the entry they were, and it stays drawn that way.
//
// The value is copied rather than edited in place, as the move's cursor is: the
// frame travels by value, and a mode edited through its pointer would change
// the frame a message is still being handled against.
func (m model) stepManaged(by int) model {
	managed := *m.manage
	managed.cursor = clamped(m.manage.cursor+by, len(m.prefs.Groups))
	m.manage = &managed
	return m
}

// carryGroup moves the group under the cursor through the order and takes the
// cursor with it: what is being moved is the group, and the highlight follows
// what it is on.
//
// Both ends are walls. A wrap would send the group the user is nudging to the
// far end of a list they are reading, and the order is the one thing here that
// is theirs alone.
func (m model) carryGroup(by int) model {
	at, ok := m.managedGroup()
	if !ok {
		return m
	}
	to := at + by
	if to < 0 || to >= len(m.prefs.Groups) {
		// Nothing moved, so there is nothing to record: a write here would put
		// the same order back on disk and call it a change.
		return m
	}
	return m.recordGroups(carriedGroups(m.prefs.Groups, at, to)).pointAt(to)
}

// askGroupName is r: the name input opens on the name the group already has, so
// a rename reads as an edit of it rather than a blank to fill (naming.go). What
// the input refuses is what the group list refuses — an empty name, a name
// another group has, the tail's own word — and the name it opened on is never a
// collision with itself.
func (m model) askGroupName() model {
	at, ok := m.managedGroup()
	if !ok {
		return m
	}
	return m.askName(renamePrompt, m.prefs.Groups[at].Name, func(m model, name string) (tea.Model, tea.Cmd) {
		return m.renameManagedGroup(name), nil
	})
}

// renameManagedGroup is the name landed: the same group, the same members, a
// different name. Membership is what a group holds and a name is how it is
// shown, so nothing is refiled here (docs/specs/TUI.md).
//
// The group is read off the cursor again rather than remembered from when the
// input opened: the mode is still up underneath it, and its cursor is where the
// answer belongs.
func (m model) renameManagedGroup(name string) model {
	at, ok := m.managedGroup()
	if !ok {
		return m
	}
	if name == m.prefs.Groups[at].Name {
		// The input opened on this name and came back with it. An editor left
		// as it was found changed nothing, and nothing is what gets written.
		return m
	}
	return m.recordGroups(renamedGroup(m.prefs.Groups, at, name)).pointAt(at)
}

// disbandGroup is d: the group ends and its members come back to the ungrouped
// tail. Nothing is destroyed — the entries are files in the config tree and
// this never touched them — so there is no confirmation to give
// (docs/specs/TUI.md).
//
// It is the one group action whose outcome the list cannot show: the heading is
// gone and its rows are somewhere further down among the ungrouped, so the
// notice line says what happened and to how many.
func (m model) disbandGroup() model {
	at, ok := m.managedGroup()
	if !ok {
		return m
	}
	disbanded := m.prefs.Groups[at]

	// How many entries come back is the write's own answer: an id whose entry
	// file is gone leaves with the same write that drops the group (groups.go),
	// so counting it would promise a row nobody can find in the tail.
	returning := len(pruneGroups([]entryGroup{disbanded}, m.tree)[0].Entries)

	m = m.recordGroups(withoutGroup(m.prefs.Groups, at))
	if !m.alert.bad {
		// A failed write outranks the outcome: what the line has to say then is
		// that cria will not remember any of this.
		m.alert = alert{text: fmt.Sprintf("disbanded %s — %d entries ungrouped", disbanded.Name, returning)}
	}
	return m.pointAt(at)
}

// leaveGroups drops the mode. There is nothing to keep or to throw away — every
// change was written as it was made — so esc here is a way out and not an
// answer.
//
// The notice line keeps whatever it is carrying. A disband reported there is
// the only thing on screen that says it happened, and esc's next meaning is
// dismissing it (tui.go).
func (m model) leaveGroups() model {
	m.manage = nil
	return m.syncEscScope()
}

// managedGroup is the group the cursor stands on, as a position in the
// preferences. It is re-read rather than remembered, like the move's target:
// the order it indexes into is whatever the last write left behind.
func (m model) managedGroup() (int, bool) {
	if m.manage == nil || len(m.prefs.Groups) == 0 {
		return 0, false
	}
	return clamped(m.manage.cursor, len(m.prefs.Groups)), true
}

// pointAt is where the mode stands once a change has landed: on the group the
// change was about, with the frame's keys re-read against the order the write
// left behind — the key that opens this mode goes with its last group.
//
// A disband can take the last one, and a mode over the group headings with no
// group heading left has nothing to stand on: it ends, and the notice line it
// wrote is what the user is left reading.
func (m model) pointAt(at int) model {
	m.manage = nil
	if len(m.prefs.Groups) > 0 {
		m.manage = &manage{cursor: clamped(at, len(m.prefs.Groups))}
	}
	// The list's own cursor keeps the row it was on: what moved is the order of
	// the headings, and the entry the user is standing on is not what they were
	// rearranging. reselect is what re-matches every contextual key to it.
	return m.reselect(m.cursor())
}

// The group list is rebuilt rather than edited, exactly as filedInto is
// (grouppick.go): preferences travel by value through this package, so several
// copies share one backing array and an edit in place would change a list still
// being read elsewhere.

// carriedGroups is the order with one group moved to another position, the rest
// closing up behind it.
func carriedGroups(groups []entryGroup, at, to int) []entryGroup {
	rest := make([]entryGroup, 0, len(groups)-1)
	rest = append(rest, groups[:at]...)
	rest = append(rest, groups[at+1:]...)

	carried := make([]entryGroup, 0, len(groups))
	carried = append(carried, rest[:to]...)
	carried = append(carried, groups[at])
	return append(carried, rest[to:]...)
}

// renamedGroup is the list with one group under a different name and the same
// members.
func renamedGroup(groups []entryGroup, at int, name string) []entryGroup {
	renamed := make([]entryGroup, 0, len(groups))
	renamed = append(renamed, groups[:at]...)
	renamed = append(renamed, entryGroup{Name: name, Entries: groups[at].Entries})
	return append(renamed, groups[at+1:]...)
}

// withoutGroup is the list with one group gone. Its members are named nowhere
// else after that, which is all "ungrouped" means (groups.go).
func withoutGroup(groups []entryGroup, at int) []entryGroup {
	kept := make([]entryGroup, 0, len(groups)-1)
	kept = append(kept, groups[:at]...)
	return append(kept, groups[at+1:]...)
}
