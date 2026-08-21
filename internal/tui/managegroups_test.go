package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// managingFrame is the grouped fixture with the mode already up: g pressed on
// the entry list, the cursor on the first group.
func managingFrame(t *testing.T) model {
	t.Helper()
	frame, _ := press(t, groupedFrame(t), typed('g'))
	if frame.manage == nil {
		t.Fatal("g did not open the mode the groups are managed in")
	}
	return frame
}

// soleGroupFrame files the whole fixture under one group — the list a disband
// can empty.
func soleGroupFrame(t *testing.T) model {
	t.Helper()
	frame := groupedFrame(t)
	frame.prefs.Groups = []entryGroup{{Name: "daily", Entries: []string{"air", "dust"}}}
	return frame.reselect(frame.cursor())
}

// managedHeadings is every group name the pane draws while the mode is up, in
// order — the tail's own heading included, so a test can say it is drawn and
// still never stood on.
func managedHeadings(frame model) []string { return headings(frame, 20) }

// g stands the cursor on the headings themselves. Every group has one while it
// is up, this backend's members or not, and the tail keeps its own without ever
// being a stop: it is where entries with no group render, not a group
// (docs/specs/TUI.md).
func TestTheGroupsKeyStandsOnTheHeadings(t *testing.T) {
	frame := managingFrame(t)

	if picked := pickedHeading(t, frame); picked != "daily" {
		t.Errorf("the mode opened on %q, want the first group", picked)
	}
	if noticeLine(frame) != "" {
		t.Errorf("the line under the box reads %q; the cursor is on the group and the bar says what the keys do", noticeLine(frame))
	}
	assertNothingWritten(t, frame)

	want := []string{"daily", "mlx only", "emptied", "ghosts", "refused", ungroupedHeading}
	if got := managedHeadings(frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the pane draws %q, want %q — every group, hidden ones included", got, want)
	}
	if got := managedHeadings(frame); slicesContain(got, newGroupLabel) {
		t.Errorf("the pane offers %q; groups are created by filing an entry, not from here", newGroupLabel)
	}

	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{manageScope + " ⏎ move · r rename · d disband · esc done", "q quit"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}
	for _, gone := range []string{"⏎ start", "m move", "c cache", "g groups", "t tools"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the keybar reads %q, want it not to offer %q while the groups are being managed", bar, gone)
		}
	}

	// The entry cursor is the list's own and the mode never took it: it stays
	// where it was and stays drawn there.
	if frame.selected != 0 {
		t.Errorf("opening the mode moved the list's cursor to row %d", frame.selected)
	}
	var marked []string
	for _, line := range list(frame, 20) {
		if strings.Contains(line, cursorMark) {
			marked = append(marked, line)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "air") {
		t.Errorf("the list draws %q as the cursor's row, want air's alone", marked)
	}
}

// slicesContain says whether a run of drawn lines holds one of them.
func slicesContain(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// The key needs a group to manage and a list to manage it on: groups come into
// being through the move and only through it, and the cache view's selection is
// not the entry list's.
func TestTheGroupsKeyIsOnlyWhereThereAreGroups(t *testing.T) {
	t.Run("a list nobody has grouped", func(t *testing.T) {
		ungrouped, _ := serveFrame(t)
		assertNoGroupsKey(t, ungrouped)
	})

	t.Run("the cache view", func(t *testing.T) {
		assertNoGroupsKey(t, groupedFrame(t).show(viewCache))
	})
}

// assertNoGroupsKey says the mode is neither drawn nor opened where the user is
// standing: a key the bar does not draw does nothing when pressed.
func assertNoGroupsKey(t *testing.T, frame model) {
	t.Helper()

	if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "g groups") {
		t.Errorf("the keybar offers the mode here: %q", bar)
	}
	pressed, cmd := press(t, frame, typed('g'))
	if cmd != nil {
		t.Error("the groups key fired a command where it does not apply")
	}
	if pressed.manage != nil {
		t.Errorf("the groups key opened %+v where it does not apply", pressed.manage)
	}
}

// j and k walk the groups and stop at both ends. The tail is drawn throughout
// and never stood on, and the list goes back to its own drawing the moment the
// mode does — line for line, escapes included.
func TestManagingWalksTheGroupsThenPutsTheListBack(t *testing.T) {
	frame := managingFrame(t)
	quiet := groupedFrame(t).listLines(listWidth, 12)

	want := []string{"daily", "mlx only", "emptied", "ghosts", "refused"}
	got := []string{pickedHeading(t, frame)}
	for range len(want) + 1 {
		frame, _ = press(t, frame, typed('j'))
		if picked := pickedHeading(t, frame); picked != got[len(got)-1] {
			got = append(got, picked)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the mode walks %q, want %q — the groups, and never the tail", got, want)
	}

	if picked := pickedHeading(t, frame); picked != "refused" {
		t.Errorf("j past the last group landed on %q, want %q", picked, "refused")
	}
	for range len(want) + 2 {
		frame, _ = press(t, frame, typed('k'))
	}
	if picked := pickedHeading(t, frame); picked != "daily" {
		t.Errorf("k past the first group landed on %q, want %q", picked, "daily")
	}

	frame, _ = press(t, frame, escape)
	if frame.manage != nil {
		t.Errorf("esc left the mode up: %+v", frame.manage)
	}
	if got := frame.listLines(listWidth, 12); !reflect.DeepEqual(got, quiet) {
		t.Errorf("the list came back as\n%q\nwant the drawing it had before the mode\n%q", got, quiet)
	}
	assertNothingWritten(t, frame)
}

// A pane too short for the list follows the heading being managed: what has to
// stay on screen is the group the keys act on.
func TestTheWindowFollowsTheManagedHeading(t *testing.T) {
	frame := managingFrame(t)
	frame, _ = pressAll(t, frame, jTimes(4)...)

	if picked := pickedHeading(t, frame); picked != "refused" {
		t.Fatalf("the cursor is on %q, want the last group", picked)
	}
	if shown := list(frame, 3); !slicesContain(shown, "refused") {
		t.Errorf("a three-line pane reads %q, want the managed heading on it", shown)
	}
}

// ⏎ picks the group under the cursor up, the cursor keys carry it, ⏎ sets it
// down — and the cursor goes with it, since what is being moved is the group.
// Each step of the carry is written as it lands, so the list and the file say
// the same thing before the next key arrives.
func TestCarryingAGroupWritesTheOrder(t *testing.T) {
	frame := managingFrame(t)

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("picking a group up fired a command")
	}
	if frame.manage == nil || !frame.manage.carrying {
		t.Fatalf("⏎ left the mode as %+v, want the group under the cursor picked up", frame.manage)
	}
	assertNothingWritten(t, frame)

	frame, cmd = press(t, frame, typed('j'))
	if cmd != nil {
		t.Error("carrying a group fired a command")
	}
	if picked := pickedHeading(t, frame); picked != "daily" {
		t.Errorf("the cursor is on %q after carrying daily down, want it still on daily", picked)
	}
	want := []string{"mlx only [cliff]", "daily [dust air]", "emptied []", "ghosts []", "refused [typo]"}
	if got := savedFiling(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the file holds\n%q\nwant\n%q", got, want)
	}
	drawn := []string{"mlx only", "daily", "emptied", "ghosts", "refused", ungroupedHeading}
	if got := managedHeadings(frame); !reflect.DeepEqual(got, drawn) {
		t.Errorf("the pane draws %q, want %q — the new order", got, drawn)
	}
	if frame.alert.text != "" {
		t.Errorf("the frame says %q about an order the headings already show", frame.alert.text)
	}

	frame, _ = press(t, frame, typed('k'))
	if picked := pickedHeading(t, frame); picked != "daily" {
		t.Errorf("the cursor is on %q after carrying daily back up, want it still on daily", picked)
	}
	back := []string{"daily [dust air]", "mlx only [cliff]", "emptied []", "ghosts []", "refused [typo]"}
	if got := savedFiling(t, frame); !reflect.DeepEqual(got, back) {
		t.Errorf("the file holds\n%q\nwant\n%q", got, back)
	}

	// ⏎ sets the group down where it stands: the mode is back to walking the
	// headings, and setting down writes nothing the carry has not written.
	frame, _ = press(t, frame, enter)
	if frame.manage == nil || frame.manage.carrying {
		t.Fatalf("⏎ left the mode as %+v, want the group set down and the mode up", frame.manage)
	}
	if picked := pickedHeading(t, frame); picked != "daily" {
		t.Errorf("the cursor is on %q after setting daily down, want it still on daily", picked)
	}

	// The entry cursor is not what was being moved, and it stayed where it was.
	if frame.selected != 0 {
		t.Errorf("carrying a group moved the list's cursor to row %d", frame.selected)
	}
}

// Both ends are walls: a group at the top has nowhere above it, one at the
// bottom nowhere below, and a keypress that moves nothing records nothing.
func TestCarryingStopsAtBothEnds(t *testing.T) {
	cases := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{name: "up from the first", keys: []tea.KeyPressMsg{enter, typed('k')}},
		{name: "down from the last", keys: append(jTimes(4), enter, typed('j'))},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame := managingFrame(t)
			frame, _ = pressAll(t, frame, test.keys...)

			if got, want := filing(frame.prefs.Groups), filing(groupedPrefs()); !reflect.DeepEqual(got, want) {
				t.Errorf("the order is now %q, want %q", got, want)
			}
			assertNothingWritten(t, frame)
		})
	}
}

// r opens the name input on the name the group already has, and what comes back
// is the same group under it: a name is how a group is shown, and changing it
// refiles nothing.
func TestRenamingAGroupKeepsItsMembers(t *testing.T) {
	frame := managingFrame(t)

	frame, cmd := press(t, frame, typed('r'))
	if cmd != nil {
		t.Error("the rename key fired a command before it had a name")
	}
	if frame.naming == nil {
		t.Fatal("r opened no name input")
	}
	if want := renamePrompt + ": daily" + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q — the input opens on the name it edits", noticeLine(frame), want)
	}
	if frame.manage == nil {
		t.Error("the input closed the mode it was asked from; esc there has nowhere to step back to")
	}

	for range len("daily") {
		frame, _ = press(t, frame, backspace)
	}
	frame = typeInto(t, frame, "mornings")
	frame, cmd = press(t, frame, enter)
	if cmd != nil {
		t.Error("the renamed group fired a command")
	}
	if frame.naming != nil {
		t.Errorf("the name landed and left %+v behind", frame.naming)
	}

	want := []string{"mornings [dust air]", "mlx only [cliff]", "emptied []", "ghosts []", "refused [typo]"}
	if got := savedFiling(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the file holds\n%q\nwant\n%q — the same members under the new name", got, want)
	}
	if picked := pickedHeading(t, frame); picked != "mornings" {
		t.Errorf("the cursor is on %q, want the group it just renamed", picked)
	}
	if got, want := list(frame, 20)[:3], []string{"mornings", "air", "dust"}; !carries(got, want) {
		t.Errorf("the pane reads %q, want %q — the members still under their heading", got, want)
	}
}

// The name rules are the group list's own and they are answered in place: the
// refusal goes beside the typed name, the mode stays up, and nothing is written
// until a name is accepted (naming.go).
func TestARefusedRenameChangesNothing(t *testing.T) {
	cases := []struct {
		name    string
		typed   string
		refusal string
	}{
		{name: "a name another group has", typed: "ghosts", refusal: `there is already a group named "ghosts"`},
		{name: "no name at all", typed: "", refusal: "a group needs a name"},
		{name: "the tail's own word", typed: ungroupedHeading, refusal: `"ungrouped" is the list's own tail, not a group`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame, _ := press(t, managingFrame(t), typed('r'))
			for range len("daily") {
				frame, _ = press(t, frame, backspace)
			}
			frame = typeInto(t, frame, test.typed)
			frame, _ = press(t, frame, enter)

			if frame.naming == nil {
				t.Fatal("the refused name closed the input")
			}
			if !strings.Contains(noticeLine(frame), test.refusal) {
				t.Errorf("the notice line reads %q, want the refusal %q on it", noticeLine(frame), test.refusal)
			}
			if got, want := filing(frame.prefs.Groups), filing(groupedPrefs()); !reflect.DeepEqual(got, want) {
				t.Errorf("the refused name left the groups as %q, want %q", got, want)
			}
			assertNothingWritten(t, frame)
		})
	}
}

// An input that opened on a name and came back with it changed nothing, so
// nothing is recorded: confirming a name unchanged is an editor closed, not a
// rename.
func TestConfirmingTheNameUnchangedWritesNothing(t *testing.T) {
	frame, _ := press(t, managingFrame(t), typed('r'))

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("the unchanged name fired a command")
	}
	if frame.naming != nil {
		t.Errorf("the input stayed up on a name it accepts: %+v", frame.naming)
	}
	if frame.manage == nil {
		t.Fatal("the confirmed name left the mode it was asked from")
	}
	if picked := pickedHeading(t, frame); picked != "daily" {
		t.Errorf("the cursor is on %q, want it still on daily", picked)
	}
	assertNothingWritten(t, frame)
}

// esc in the input is a step back to the headings, not out of the mode: the
// cursor is exactly where it was left.
func TestEscFromTheRenameGoesBackToTheHeadings(t *testing.T) {
	frame := managingFrame(t)
	frame, _ = pressAll(t, frame, jTimes(2)...)
	if picked := pickedHeading(t, frame); picked != "emptied" {
		t.Fatalf("the cursor is on %q, want %q", picked, "emptied")
	}

	frame, _ = pressAll(t, frame, typed('r'), typed('!'), escape)
	if frame.naming != nil || frame.manage == nil {
		t.Fatalf("esc left the input as %+v and the mode as %+v", frame.naming, frame.manage)
	}
	if picked := pickedHeading(t, frame); picked != "emptied" {
		t.Errorf("the cursor came back on %q, want %q", picked, "emptied")
	}
	assertNothingWritten(t, frame)
}

// d ends a group and its members come back to the ungrouped tail. Nothing is
// destroyed, so nothing is confirmed first — and because the heading and its
// rows are the only trace of what happened, the notice line reports it
// (docs/specs/TUI.md).
func TestDisbandingAGroupUngroupsItsMembers(t *testing.T) {
	frame := managingFrame(t)

	frame, cmd := press(t, frame, typed('d'))
	if cmd != nil {
		t.Error("the disband fired a command")
	}
	if frame.confirm != nil || frame.modal != nil {
		t.Error("the disband asked to be confirmed; nothing about it is destructive")
	}

	// daily held three ids and one of them has no entry file left: the write
	// that drops the group drops that id too, so it is not an entry coming back.
	if want := "disbanded daily — 2 entries ungrouped"; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q", noticeLine(frame), want)
	}
	if frame.alert.bad {
		t.Error("the disband is drawn as something cria could not do")
	}

	want := []string{"mlx only [cliff]", "emptied []", "ghosts []", "refused [typo]"}
	if got := savedFiling(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the file holds\n%q\nwant\n%q", got, want)
	}
	if got, want := managedHeadings(frame), []string{"mlx only", "emptied", "ghosts", "refused", ungroupedHeading}; !reflect.DeepEqual(got, want) {
		t.Errorf("the pane draws %q, want %q", got, want)
	}
	if picked := pickedHeading(t, frame); picked != "mlx only" {
		t.Errorf("the cursor landed on %q, want the group that took the disbanded one's place", picked)
	}

	// esc still belongs to the mode with that line up: the notice stands behind
	// the mode rather than in front of it, so neither the bar nor the keyboard
	// offers dismissing it until the mode is gone.
	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc done") || strings.Contains(bar, "esc dismiss") {
		t.Errorf("the keybar reads %q, want esc leaving the mode rather than the notice", bar)
	}
	if frame.keys.clearAlert.Enabled() {
		t.Error("esc is bound to dismissing the notice under a mode that owns the key")
	}

	// The members are on the list where the ungrouped go, in the tail's own
	// alphabetical order.
	tail := list(frame, 20)
	from := 0
	for i, line := range tail {
		if line == ungroupedHeading {
			from = i + 1
		}
	}
	if got, want := tail[from:], []string{"air", "bark", "dust", "typo"}; !carries(got, want) {
		t.Errorf("the tail reads %q, want %q", got, want)
	}
}

// The mode manages groups, so it ends with the last one: there is no heading
// left to stand on. What it reported stays on the line behind it.
func TestDisbandingTheLastGroupEndsTheMode(t *testing.T) {
	frame, _ := press(t, soleGroupFrame(t), typed('g'))
	frame, _ = press(t, frame, typed('d'))

	if frame.manage != nil {
		t.Errorf("the mode outlived the groups: %+v", frame.manage)
	}
	if want := "disbanded daily — 2 entries ungrouped"; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q kept behind the mode", noticeLine(frame), want)
	}
	if len(frame.prefs.Groups) != 0 {
		t.Errorf("the groups are %q, want none left", filing(frame.prefs.Groups))
	}
	if got := savedFiling(t, frame); len(got) != 0 {
		t.Errorf("the file holds %q, want no groups in it", got)
	}

	// A list nobody has grouped draws no headings, and the key that manages them
	// is gone with them.
	if got := headings(frame, 20); len(got) != 0 {
		t.Errorf("the pane draws %q, want a flat list", got)
	}
	assertNoGroupsKey(t, frame)

	// esc is the alert's again, now that no mode owns it.
	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc dismiss") {
		t.Errorf("the keybar reads %q, want esc back on the notice it left", bar)
	}
}

// esc backs out one level at a time and never discards: with a group in the
// air it sets it down, with the cursor walking it leaves the mode — and every
// change was written as it was made, so what the mode did is still there after
// it is gone.
func TestEscLeavesTheModeWithTheLastWriteKept(t *testing.T) {
	frame := managingFrame(t)
	frame, _ = pressAll(t, frame, enter, typed('j'), escape)

	if frame.manage == nil || frame.manage.carrying {
		t.Fatalf("esc under a carried group left the mode as %+v, want the group set down and the mode up", frame.manage)
	}

	frame, _ = press(t, frame, escape)
	if frame.manage != nil {
		t.Errorf("esc left the mode up: %+v", frame.manage)
	}
	want := []string{"mlx only [cliff]", "daily [dust air]", "emptied []", "ghosts []", "refused [typo]"}
	if got := savedFiling(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the file holds\n%q\nwant\n%q — the order the mode wrote", got, want)
	}
	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "g groups") || strings.Contains(bar, "esc done") || strings.Contains(bar, "⏎ place") {
		t.Errorf("the keybar reads %q, want the frame's own keys back", bar)
	}
}

// A group in the air rides the carry band — its own hue, so "in your hand" is
// never read as "selected". The heading cursor is what says so to the pane:
// held while carrying, not held while walking, never held under the move.
func TestACarriedGroupRidesTheCarryBand(t *testing.T) {
	frame := managingFrame(t)
	if frame.headingCursor().held {
		t.Error("the heading cursor reads held while the mode is only walking")
	}

	frame, _ = press(t, frame, enter)
	if !frame.headingCursor().held {
		t.Error("the heading cursor does not read held with a group in the air")
	}
	banded := headingLine("daily", true, true, 20)
	walking := headingLine("daily", true, false, 20)
	if banded == walking && carryHex != bandHex {
		t.Error("a held heading draws exactly as a picked one; the carry band is not being painted")
	}

	moving, _ := press(t, groupedFrame(t), typed('m'))
	if moving.headingCursor().held {
		t.Error("the heading cursor reads held under the move pick")
	}
}

// A group in the air answers only to the carry: the bar says so, and the keys
// that change what a group is — rename, disband — wait until it is set down.
func TestACarriedGroupHoldsTheKeys(t *testing.T) {
	frame, _ := press(t, managingFrame(t), enter)

	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{manageScope + " ⏎ place · esc place", "q quit"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}
	for _, gone := range []string{"r rename", "d disband", "esc done"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the keybar reads %q, want it not to offer %q while a group is in the air", bar, gone)
		}
	}

	for _, ignored := range []tea.KeyPressMsg{typed('r'), typed('d'), typed('g'), typed('c')} {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, ignored)
		if cmd != nil {
			t.Errorf("%q fired a command from under the carry", ignored.String())
		}
	}
	if frame.naming != nil {
		t.Error("rename opened its input on a group in the air")
	}
	if frame.manage == nil || !frame.manage.carrying {
		t.Fatalf("a key put the mode into %+v, want the group still in the air", frame.manage)
	}
	if got, want := filing(frame.prefs.Groups), filing(groupedPrefs()); !reflect.DeepEqual(got, want) {
		t.Errorf("the groups are now %q, want %q untouched", got, want)
	}
	assertNothingWritten(t, frame)
}

// The mode holds the keyboard while it is up: the list underneath is being
// rearranged, and every other key would act on a list that is moving.
func TestManagingHoldsTheKeyboard(t *testing.T) {
	frame := managingFrame(t)

	for _, ignored := range []tea.KeyPressMsg{typed('c'), {Code: tea.KeyTab}, typed('x'), typed('t'), typed('b'), typed('m'), typed('g')} {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, ignored)
		if cmd != nil {
			t.Errorf("%q fired a command from under the mode", ignored.String())
		}
	}

	if frame.view != viewServe {
		t.Errorf("a key moved the frame to %v while the groups were being managed", frame.view)
	}
	if frame.prefs.Backend != defaultPrefs().Backend {
		t.Errorf("a key switched the backend to %q while the mode was up", frame.prefs.Backend)
	}
	if frame.toolsOpen || frame.benchOpen || frame.log.open || frame.pick != nil || frame.confirm != nil || frame.move != nil {
		t.Error("a key opened another screen from under the mode")
	}
	if frame.manage == nil {
		t.Fatal("the mode went away by itself")
	}
	assertNothingWritten(t, frame)

	// And the one key that does work still does.
	frame, _ = press(t, frame, escape)
	if frame.manage != nil {
		t.Error("esc did not leave the mode")
	}
}
