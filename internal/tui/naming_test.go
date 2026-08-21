package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// backspace is the key that takes a character back off the line.
var backspace = tea.KeyPressMsg{Code: tea.KeyBackspace}

// askedName is what the key that opened the input got back: the name ⏎ handed
// it, and how many times it was handed anything at all.
type askedName struct {
	name  string
	calls int
}

// namingFrame opens the input over the grouped fixture, so the names already
// taken are the ones a duplicate is refused against (groups_test.go).
func namingFrame(t *testing.T, prompt, prefill string) (model, *askedName) {
	t.Helper()
	asked := &askedName{}
	frame := groupedFrame(t).askName(prompt, prefill, func(m model, name string) (tea.Model, tea.Cmd) {
		asked.name, asked.calls = name, asked.calls+1
		return m, nil
	})
	return frame, asked
}

// typeInto presses one string into the frame a character at a time, the way a
// terminal reports them.
func typeInto(t *testing.T, frame model, text string) model {
	t.Helper()
	for _, r := range text {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, typed(r))
		if cmd != nil {
			t.Fatalf("typing %q fired a command", string(r))
		}
	}
	return frame
}

// noticeLine is the row under the status box as a person reads it.
func noticeLine(frame model) string {
	notes := frame.notes(frame.frameWidth())
	return strings.TrimRight(plain(notes[len(notes)-1]), " ")
}

// Typing runs into the notice line: the prompt, what has been typed, and the
// block where the next character lands (docs/specs/TUI.md).
func TestTypingRunsIntoTheNoticeLine(t *testing.T) {
	frame, asked := namingFrame(t, "new group", "")

	frame = typeInto(t, frame, "qwen tésts")
	if frame.naming == nil {
		t.Fatal("the input closed while it was being typed into")
	}
	if want := "qwen tésts"; frame.naming.text != want {
		t.Errorf("the input holds %q, want %q", frame.naming.text, want)
	}
	if want := "new group: qwen tésts" + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q", noticeLine(frame), want)
	}
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "new group: qwen tésts") {
		t.Errorf("the frame does not draw the name being typed:\n%s", drawn)
	}
	if asked.calls != 0 {
		t.Errorf("typing handed the name over %d times before ⏎", asked.calls)
	}
}

// A key that printed nothing leaves the name alone: an arrow is not a character,
// and a name field has nowhere for it to go.
func TestKeysThatPrintNothingLeaveTheNameAlone(t *testing.T) {
	frame, _ := namingFrame(t, "new group", "")
	frame = typeInto(t, frame, "qwen")

	for _, ignored := range []tea.KeyPressMsg{
		{Code: tea.KeyUp}, {Code: tea.KeyDown}, {Code: tea.KeyLeft}, {Code: tea.KeyRight}, {Code: tea.KeyTab},
	} {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, ignored)
		if cmd != nil {
			t.Errorf("%q fired a command from under the input", ignored.String())
		}
		if frame.naming == nil {
			t.Fatalf("%q closed the input", ignored.String())
		}
		if frame.naming.text != "qwen" {
			t.Errorf("%q left the name as %q, want it untouched", ignored.String(), frame.naming.text)
		}
	}
}

// Backspace takes one character off, counted in runes: an accented letter goes
// in one press rather than leaving half of itself on the line.
func TestBackspaceTakesOneCharacterOff(t *testing.T) {
	frame, _ := namingFrame(t, "new group", "")
	frame = typeInto(t, frame, "aé")

	frame, _ = press(t, frame, backspace)
	if frame.naming.text != "a" {
		t.Errorf("backspace left %q, want the whole last character gone", frame.naming.text)
	}

	// An empty name is as far back as it goes, and the input stays up there.
	frame, _ = press(t, frame, backspace)
	frame, cmd := press(t, frame, backspace)
	if cmd != nil {
		t.Error("backspace on an empty name fired a command")
	}
	if frame.naming == nil {
		t.Fatal("backspace on an empty name closed the input")
	}
	if frame.naming.text != "" {
		t.Errorf("backspace on an empty name left %q", frame.naming.text)
	}
	if want := "new group: " + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q", noticeLine(frame), want)
	}
}

// An input can open on a name — a rename edits the name it is changing rather
// than making the user retype it.
func TestTheInputOpensOnTheNameItIsEditing(t *testing.T) {
	frame, _ := namingFrame(t, "rename group", "daily")

	if want := "rename group: daily" + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want %q", noticeLine(frame), want)
	}
	frame, _ = press(t, frame, backspace)
	if frame.naming.text != "dail" {
		t.Errorf("backspace over the prefilled name left %q, want %q", frame.naming.text, "dail")
	}
}

// ⏎ hands the name to the key that asked for it, trimmed — a name is typed, and
// a space at either end of one is a slip rather than a choice — and the mode
// goes with the answer.
func TestEnterHandsTheTrimmedNameToTheKeyThatAsked(t *testing.T) {
	frame, asked := namingFrame(t, "new group", "")
	frame = typeInto(t, frame, "  qwen tests  ")

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("the confirmed name fired a command of the input's own")
	}
	if asked.calls != 1 {
		t.Fatalf("the name was handed over %d times, want once", asked.calls)
	}
	if want := "qwen tests"; asked.name != want {
		t.Errorf("the key was handed %q, want %q", asked.name, want)
	}
	if frame.naming != nil {
		t.Errorf("the input outlived the answer: %+v", frame.naming)
	}
	if noticeLine(frame) != "" {
		t.Errorf("the notice line reads %q after the name landed, want it empty", noticeLine(frame))
	}
}

// esc leaves the name untyped and hands nothing over. The line goes back to what
// it was saying before: the input stood in front of it rather than writing there.
func TestEscCancelsTheNameAndPutsTheLineBack(t *testing.T) {
	frame, asked := namingFrame(t, "new group", "")
	frame.alert = alert{text: "port 8080 is held by a process cria did not start", bad: true}

	frame = typeInto(t, frame, "qwen")
	if want := "new group: qwen" + nameCursor; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q, want the name to take it from the alert", noticeLine(frame))
	}
	rows := strings.Count(frame.View().Content, "\n")

	frame, cmd := press(t, frame, escape)
	if cmd != nil {
		t.Error("the cancelled name fired a command")
	}
	if frame.naming != nil {
		t.Errorf("esc left the input up: %+v", frame.naming)
	}
	if asked.calls != 0 {
		t.Errorf("the cancelled name was handed over %d times", asked.calls)
	}
	if want := frame.alert.text; noticeLine(frame) != want {
		t.Errorf("the notice line reads %q after the cancel, want %q back", noticeLine(frame), want)
	}
	if got := strings.Count(frame.View().Content, "\n"); got != rows {
		t.Errorf("the frame is %d rows with the name gone and was %d with it up", got, rows)
	}
}

// A name cria cannot use is refused on the line it was typed on, and the input
// stays up with the text as it was: the correction is a backspace away, and the
// key that asked for the name is handed nothing (docs/specs/TUI.md).
func TestRefusedNamesKeepTheInputUp(t *testing.T) {
	cases := []struct {
		name    string
		typed   string
		refusal string
	}{
		{name: "nothing typed", typed: "", refusal: "a group needs a name"},
		{name: "spaces only", typed: "   ", refusal: "a group needs a name"},
		{name: "a name another group already has", typed: "daily", refusal: `there is already a group named "daily"`},
		{name: "the word the ungrouped tail is called", typed: ungroupedHeading, refusal: `"` + ungroupedHeading + `" is the list's own tail, not a group`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame, asked := namingFrame(t, "new group", "")
			frame = typeInto(t, frame, test.typed)

			frame, cmd := press(t, frame, enter)
			if cmd != nil {
				t.Error("a refused name fired a command")
			}
			if asked.calls != 0 {
				t.Fatalf("a refused name was handed over %d times", asked.calls)
			}
			if frame.naming == nil {
				t.Fatal("the refusal closed the input, leaving nothing to correct")
			}
			if frame.naming.text != test.typed {
				t.Errorf("the refusal left %q on the line, want the text as it was typed (%q)", frame.naming.text, test.typed)
			}

			line := noticeLine(frame)
			if !strings.Contains(line, test.refusal) {
				t.Errorf("the notice line reads %q, want it to say %q", line, test.refusal)
			}
			if !strings.Contains(line, "new group: "+test.typed+nameCursor) {
				t.Errorf("the notice line reads %q, want the typed name still on it", line)
			}

			// The refusal answered that ⏎, so the next keystroke takes it away.
			frame = typeInto(t, frame, "s")
			if got := noticeLine(frame); strings.Contains(got, test.refusal) {
				t.Errorf("the notice line still reads %q after the name changed", got)
			}
		})
	}
}

// A name is not a collision with itself: an input that opened on a name is that
// name's editor, and confirming it unchanged is a no-op rather than a refusal.
func TestANameIsNotACollisionWithItself(t *testing.T) {
	frame, asked := namingFrame(t, "rename group", "daily")

	frame, _ = press(t, frame, enter)
	if asked.calls != 1 || asked.name != "daily" {
		t.Fatalf("the unchanged name was handed over %d times as %q, want once as \"daily\"", asked.calls, asked.name)
	}
	if frame.naming != nil {
		t.Errorf("the input outlived the answer: %+v", frame.naming)
	}
}

// The bar says what works while a name is being typed, and nothing else does:
// the two keys the input answers to, and the one key that still leaves.
func TestTheBarOffersTheNameKeysWhileOneIsTyped(t *testing.T) {
	frame, _ := namingFrame(t, "new group", "")

	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{namingScope, "⏎ confirm", "esc cancel", "^C quit"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}
	for _, gone := range []string{"q quit", "⏎ start", "s stop", "c cache", "esc dismiss", "esc back"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the keybar reads %q, want it not to offer %q while a name is typed", bar, gone)
		}
	}
}

// The input takes every key: a letter is a letter here, so q types rather than
// quits and nothing underneath acts. ctrl+c is the exception every terminal
// program keeps.
func TestTheInputHoldsTheKeyboard(t *testing.T) {
	frame, _ := namingFrame(t, "new group", "")
	before := frame.selected

	frame = typeInto(t, frame, "qcxstbjk")
	if want := "qcxstbjk"; frame.naming.text != want {
		t.Fatalf("the input holds %q, want %q — every one of those keys is a character here", frame.naming.text, want)
	}
	if frame.view != viewServe {
		t.Errorf("a key moved the frame to %v while a name was being typed", frame.view)
	}
	if frame.prefs.Backend != defaultPrefs().Backend {
		t.Errorf("a key switched the backend to %q while a name was being typed", frame.prefs.Backend)
	}
	if frame.toolsOpen || frame.benchOpen || frame.log.open || frame.pick != nil || frame.confirm != nil {
		t.Error("a key opened another screen from under the input")
	}
	if frame.selected != before {
		t.Errorf("a key moved the entry list's cursor to %d, want it left on %d", frame.selected, before)
	}

	frame, cmd := press(t, frame, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if _, quits := run(t, cmd).(tea.QuitMsg); !quits {
		t.Error("ctrl+c did not leave the program while a name was being typed")
	}
	if frame.naming.text != "qcxstbjk" {
		t.Errorf("ctrl+c typed itself into the name: %q", frame.naming.text)
	}
}
