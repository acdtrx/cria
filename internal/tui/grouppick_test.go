package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// filing is a grouping as a person reads it down the file: each group in the
// order the preferences hold them, with the ids filed under it.
func filing(groups []entryGroup) []string {
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		lines = append(lines, fmt.Sprintf("%s [%s]", group.Name, strings.Join(group.Entries, " ")))
	}
	return lines
}

// savedFiling is the grouping as the next launch would read it: off the file,
// not off the frame that wrote it.
func savedFiling(t *testing.T, frame model) []string {
	t.Helper()
	saved, err := loadPrefs(frame.root)
	if err != nil {
		t.Fatalf("reading the preferences back: %v", err)
	}
	if !reflect.DeepEqual(saved.Groups, frame.prefs.Groups) {
		t.Errorf("the file holds %v and the frame holds %v", filing(saved.Groups), filing(frame.prefs.Groups))
	}
	return filing(saved.Groups)
}

// assertNothingWritten says the preferences file was never written: the state
// root of a test starts empty, so a question that was cancelled — or a key that
// changed nothing — leaves it that way.
func assertNothingWritten(t *testing.T, frame model) {
	t.Helper()
	if _, err := os.Stat(prefsPath(frame.root)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the preferences file exists after a change that never happened (%v)", err)
	}
}

// pickedHeading is the heading the armed move's cursor is standing on, as the
// pane draws it: the one heading line on the band. The entry the question is
// about keeps its own band down in the rows, so the two are told apart the way
// the eye does it — a row is indented past the marker column, a heading is not.
func pickedHeading(t *testing.T, frame model) string {
	t.Helper()

	var picked []string
	for _, line := range frame.listLines(listWidth, 20) {
		text := strings.TrimRight(plain(line), " ")
		if text == "" || strings.HasPrefix(text, nothingHere) || strings.HasPrefix(text, cursorMark) {
			continue
		}
		if strings.Contains(line, opener(bandStyle)) {
			picked = append(picked, text)
		}
	}
	if len(picked) != 1 {
		t.Fatalf("the pane draws %d picked headings: %q", len(picked), picked)
	}
	return picked[0]
}

// pressAll runs a run of keys through the frame, the way a hand does.
func pressAll(t *testing.T, frame model, keys ...tea.KeyPressMsg) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, pressed := range keys {
		frame, cmd = press(t, frame, pressed)
	}
	return frame, cmd
}

// jTimes is the down key, pressed a number of times.
func jTimes(times int) []tea.KeyPressMsg {
	keys := make([]tea.KeyPressMsg, 0, times)
	for range times {
		keys = append(keys, typed('j'))
	}
	return keys
}

// m arms the question the entry list cannot answer for itself: which group this
// entry belongs to. The line under the box asks it, the bar names the two
// answers, and nothing has been filed yet (docs/specs/TUI.md).
func TestTheMoveKeyAsksWhichGroup(t *testing.T) {
	frame := groupedFrame(t)

	frame, cmd := press(t, frame, typed('m'))
	if cmd != nil {
		t.Fatal("the move key fired a command before it was told which group it meant")
	}
	if frame.move == nil || frame.move.entry != "air" {
		t.Fatalf("the key armed %+v, want the move waiting for air's group", frame.move)
	}
	if want := "move air to which group"; noticeLine(frame) != want {
		t.Errorf("the line under the box reads %q, want %q", noticeLine(frame), want)
	}
	assertNothingWritten(t, frame)

	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{moveScope, "⏎ move", "esc cancel", "q quit"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}
	for _, gone := range []string{"⏎ start", "m move", "c cache", "x delete"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the keybar reads %q, want it not to offer %q while the key is asking", bar, gone)
		}
	}

	// The entry the question is about stays where it was and stays drawn as the
	// row the user is standing on.
	if frame.selected != 0 {
		t.Errorf("arming the move moved the list's cursor to row %d", frame.selected)
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

// The selection scope offers the key on an entry the list can file, and the bar
// and the keyboard say the same thing: a refused entry file cannot be sorted
// under a group — the key that would sort it is the key that could not be read —
// and the cache view's selection is not the entry list's.
func TestTheMoveKeyIsOnlyOnEntriesItCanFile(t *testing.T) {
	frame := groupedFrame(t)
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "m move") {
		t.Errorf("the keybar reads %q, want the selection scope to offer the move", bar)
	}

	t.Run("an entry file cria refused", func(t *testing.T) {
		refused := groupedFrame(t).reselect(3)
		if listed, _ := refused.selectedRow(); listed.broken == nil {
			t.Fatalf("the cursor is on %q, want the refused file", listed.id())
		}
		assertNoMoveKey(t, refused)
	})

	t.Run("the cache view", func(t *testing.T) {
		// The cache view has a selection of its own (docs/specs/CACHE.md), and
		// its keys are not the entry list's.
		cached, _ := serveFrame(t)
		cached = cached.show(viewCache)
		if bar := plain(renderKeybar(200, cached.groups()...)); !strings.Contains(bar, "x delete") {
			t.Fatalf("the cache list has no selection to test the move against: %q", bar)
		}
		assertNoMoveKey(t, cached)
	})
}

// assertNoMoveKey says the move is neither drawn nor fired where the user is
// standing: a key the bar does not draw does nothing when pressed.
func assertNoMoveKey(t *testing.T, frame model) {
	t.Helper()

	if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "m move") {
		t.Errorf("the keybar offers the move here: %q", bar)
	}
	pressed, cmd := press(t, frame, typed('m'))
	if cmd != nil {
		t.Error("the move key fired a command where it does not apply")
	}
	if pressed.move != nil || pressed.naming != nil {
		t.Errorf("the move key armed %+v / opened %+v where it does not apply", pressed.move, pressed.naming)
	}
}

// The answers are every group but the one the entry is already in — filing
// something where it is means nothing — then the tail, then the group that does
// not exist yet. They are walked in the order the pane draws them, and the
// cursor stops at both ends rather than wrapping.
func TestTheMoveWalksTheHeadingsItCanFileUnder(t *testing.T) {
	frame := groupedFrame(t)
	frame, _ = press(t, frame, typed('m'))

	want := []string{"mlx only", "emptied", "ghosts", "refused", ungroupedHeading, newGroupLabel}
	got := []string{pickedHeading(t, frame)}
	for range len(want) + 1 {
		frame, _ = press(t, frame, typed('j'))
		if picked := pickedHeading(t, frame); picked != got[len(got)-1] {
			got = append(got, picked)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the move offers %q, want %q — every group but air's own, then the tail, then the new one", got, want)
	}

	frame, _ = pressAll(t, frame, jTimes(2)...)
	if picked := pickedHeading(t, frame); picked != newGroupLabel {
		t.Errorf("j past the last answer landed on %q, want %q", picked, newGroupLabel)
	}
	for range len(want) + 2 {
		frame, _ = press(t, frame, typed('k'))
	}
	if picked := pickedHeading(t, frame); picked != "mlx only" {
		t.Errorf("k past the first answer landed on %q, want %q", picked, "mlx only")
	}
}

// While the question is up every group has a heading, whether or not this
// backend has anything under it: a group the user cannot see is a group they
// cannot answer with. The list goes back to its own rules the moment the
// question does — drawn line for drawn line, escapes included.
func TestArmingShowsEveryGroupThenPutsTheListBack(t *testing.T) {
	frame := groupedFrame(t)

	quiet := frame.listLines(listWidth, 12)
	if got, want := headings(frame, 12), []string{"daily", "emptied", "ghosts", ungroupedHeading}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the unarmed pane's headings are %q, want %q", got, want)
	}

	frame, _ = press(t, frame, typed('m'))
	armed := []string{"daily", "mlx only", "emptied", "ghosts", "refused", ungroupedHeading, newGroupLabel}
	if got := headings(frame, 20); !reflect.DeepEqual(got, armed) {
		t.Errorf("the armed pane's headings are %q, want %q — every group, the tail and the new one", got, armed)
	}

	frame, _ = press(t, frame, escape)
	if got := frame.listLines(listWidth, 12); !reflect.DeepEqual(got, quiet) {
		t.Errorf("the list came back as\n%q\nwant the drawing it had before the question\n%q", got, quiet)
	}
}

// The picked heading is drawn on the cursor's own band, spanning the pane, and
// without the marker a row keeps: a heading is still not a row the list stops on.
func TestThePickedHeadingIsDrawnOnTheBand(t *testing.T) {
	frame := groupedFrame(t)
	frame, _ = press(t, frame, typed('m'))

	var picked string
	for _, line := range frame.listLines(listWidth, 20) {
		if strings.HasPrefix(plain(line), "mlx only") {
			picked = line
		}
	}
	if picked == "" {
		t.Fatalf("the pane does not draw the picked heading:\n%q", frame.listLines(listWidth, 20))
	}
	if !strings.Contains(picked, opener(bandStyle)) {
		t.Errorf("the picked heading carries no band: %q", picked)
	}
	if !strings.Contains(picked, opener(bandHeadingStyle)) {
		t.Errorf("the picked heading is not drawn in the heading's band tone: %q", picked)
	}
	if strings.Contains(plain(picked), cursorMark) {
		t.Errorf("the picked heading carries the row cursor's marker: %q", picked)
	}
	if got := lipgloss.Width(picked); got != listWidth {
		t.Errorf("the picked heading is %d cells wide, want the pane's %d so the band spans it", got, listWidth)
	}
}

// A pane too short for the list follows the question while one is up: the
// heading being picked is what has to stay on screen, and the entry cursor —
// which nothing is moving — is what scrolls off.
func TestTheWindowFollowsThePickedHeading(t *testing.T) {
	frame := groupedFrame(t)
	frame, _ = press(t, frame, typed('m'))

	if got, want := list(frame, 3), []string{"dust", "mlx only", "emptied"}; !carries(got, want) {
		t.Errorf("a three-line pane reads %q, want %q around the picked heading", got, want)
	}

	frame, _ = pressAll(t, frame, jTimes(5)...)
	bottom := list(frame, 3)
	if !strings.Contains(bottom[len(bottom)-1], newGroupLabel) {
		t.Errorf("the pane scrolled the picked heading off itself: %q", bottom)
	}
}

// ⏎ files the entry under the heading the cursor landed on and writes it down:
// out of the group that held it, into the one that answers, and the ids no entry
// file backs any more go out with the same write (docs/specs/TUI.md).
func TestFilingAnEntryWritesTheGrouping(t *testing.T) {
	cases := []struct {
		name  string
		keys  []tea.KeyPressMsg
		filed []string
	}{
		{
			name:  "into another group",
			keys:  jTimes(2), // mlx only, emptied, ghosts
			filed: []string{"daily [dust]", "mlx only [cliff]", "emptied []", "ghosts [air]", "refused [typo]"},
		},
		{
			name:  "into the ungrouped tail",
			keys:  jTimes(4),
			filed: []string{"daily [dust]", "mlx only [cliff]", "emptied []", "ghosts []", "refused [typo]"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame := groupedFrame(t)
			frame, _ = press(t, frame, typed('m'))
			frame, _ = pressAll(t, frame, test.keys...)

			frame, cmd := press(t, frame, enter)
			if cmd != nil {
				t.Error("filing an entry fired a command")
			}
			if frame.move != nil {
				t.Errorf("the question outlived its answer: %+v", frame.move)
			}
			if frame.alert.text != "" {
				t.Errorf("the frame says %q about a move the headings already show", frame.alert.text)
			}
			if got := savedFiling(t, frame); !reflect.DeepEqual(got, test.filed) {
				t.Errorf("the file holds\n%q\nwant\n%q", got, test.filed)
			}

			// The cursor keeps the entry it filed rather than whatever took its
			// place in a list that has just reordered.
			listed, ok := frame.selectedRow()
			if !ok || listed.id() != "air" {
				t.Errorf("the cursor is on %q after the move, want it still on air", listed.id())
			}
		})
	}
}

// `new group…` is how a group comes into being: ⏎ opens the name input over the
// armed question, and the name creates the group at the end of the order with
// the entry already in it — one write, the file never holding a group with
// nothing in it.
func TestFilingIntoANewGroup(t *testing.T) {
	frame := groupedFrame(t)
	frame, _ = press(t, frame, typed('m'))
	frame, _ = pressAll(t, frame, jTimes(5)...)
	if picked := pickedHeading(t, frame); picked != newGroupLabel {
		t.Fatalf("the cursor is on %q, want %q", picked, newGroupLabel)
	}

	frame, _ = press(t, frame, enter)
	if frame.naming == nil {
		t.Fatal("the new-group answer did not open the name input")
	}
	if frame.move == nil {
		t.Error("the name input closed the question it was asked from; esc there has nowhere to step back to")
	}
	if want := newGroupPrompt + ": " + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q", noticeLine(frame), want)
	}

	// esc there is a step back to the headings, not out of the move.
	frame, _ = press(t, frame, escape)
	if frame.naming != nil || frame.move == nil {
		t.Fatalf("esc left the input as %+v and the move as %+v", frame.naming, frame.move)
	}
	if want := "move air to which group"; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q after the cancelled name, want %q back", noticeLine(frame), want)
	}
	assertNothingWritten(t, frame)

	frame, _ = press(t, frame, enter)
	frame = typeInto(t, frame, "qwen tests")
	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("the named group fired a command")
	}
	if frame.naming != nil || frame.move != nil {
		t.Errorf("the name landed and left %+v / %+v behind", frame.naming, frame.move)
	}

	want := []string{"daily [dust]", "mlx only [cliff]", "emptied []", "ghosts []", "refused [typo]", "qwen tests [air]"}
	if got := savedFiling(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the file holds\n%q\nwant\n%q — the new group last, with the entry in it", got, want)
	}
	if got := headings(frame, 20); !reflect.DeepEqual(got, []string{"daily", "emptied", "ghosts", "qwen tests", ungroupedHeading}) {
		t.Errorf("the list's headings are %q, want the new group among them", got)
	}
}

// A list with no groups yet can only be answered one way, so the key does not
// ask: it opens the name input, the way a server key with one target acts
// instead of arming (pick.go).
func TestWithNoGroupsTheMoveOpensTheNameInput(t *testing.T) {
	frame, _ := serveFrame(t)

	frame, cmd := press(t, frame, typed('m'))
	if cmd != nil {
		t.Fatal("the move key fired a command")
	}
	if frame.move != nil {
		t.Errorf("the key armed a pick of one: %+v", frame.move)
	}
	if frame.naming == nil {
		t.Fatal("the key asked nothing at all; the only answer is a group to name")
	}
	if want := newGroupPrompt + ": " + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q", noticeLine(frame), want)
	}

	// esc there leaves nothing armed behind it, since nothing was.
	cancelled, _ := press(t, frame, escape)
	if cancelled.naming != nil || cancelled.move != nil {
		t.Errorf("esc left %+v / %+v up", cancelled.naming, cancelled.move)
	}
	assertNothingWritten(t, cancelled)

	frame = typeInto(t, frame, "daily")
	frame, _ = press(t, frame, enter)

	if got, want := savedFiling(t, frame), []string{"daily [gemma]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the file holds %q, want %q", got, want)
	}
	if got, want := headings(frame, 12), []string{"daily", ungroupedHeading}; !reflect.DeepEqual(got, want) {
		t.Errorf("the list's headings are %q, want %q", got, want)
	}
}

// esc leaves the question unanswered and the preferences exactly as they were:
// nothing filed, nothing written, and the frame's own keys back.
func TestEscCancelsTheMove(t *testing.T) {
	frame := groupedFrame(t)

	frame, _ = pressAll(t, frame, typed('m'), typed('j'))
	frame, cmd := press(t, frame, escape)
	if cmd != nil {
		t.Error("the cancelled move ran something")
	}
	if frame.move != nil {
		t.Errorf("esc left the question up: %+v", frame.move)
	}
	if frame.alert.text != "" {
		t.Errorf("esc left %q under the box, want the question gone with it", frame.alert.text)
	}
	if !reflect.DeepEqual(frame.prefs.Groups, groupedPrefs()) {
		t.Errorf("the cancelled move left the grouping as %q", filing(frame.prefs.Groups))
	}
	assertNothingWritten(t, frame)

	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "m move") || strings.Contains(bar, moveScope) {
		t.Errorf("the keybar reads %q, want the frame's own keys back", bar)
	}
}

// The question holds the keyboard while it is up: every other key would answer
// something the user is in the middle of being asked.
func TestTheMoveHoldsTheKeyboard(t *testing.T) {
	frame := groupedFrame(t)
	frame, _ = press(t, frame, typed('m'))

	for _, ignored := range []tea.KeyPressMsg{typed('c'), {Code: tea.KeyTab}, typed('x'), typed('t'), typed('b'), typed('m')} {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, ignored)
		if cmd != nil {
			t.Errorf("%q fired a command from under the move", ignored.String())
		}
	}

	if frame.view != viewServe {
		t.Errorf("a key moved the frame to %v while it was being asked which group", frame.view)
	}
	if frame.prefs.Backend != defaultPrefs().Backend {
		t.Errorf("a key switched the backend to %q while the move was up", frame.prefs.Backend)
	}
	if frame.toolsOpen || frame.benchOpen || frame.log.open || frame.pick != nil || frame.confirm != nil {
		t.Error("a key opened another screen from under the move")
	}
	if frame.move == nil {
		t.Fatal("the question went away by itself")
	}
	assertNothingWritten(t, frame)

	// And the one key that does work still does.
	frame, _ = press(t, frame, escape)
	if frame.move != nil {
		t.Error("esc did not leave the move")
	}
}
