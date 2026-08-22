package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"cria/internal/config"
	"cria/internal/picks"
)

// left and right are the two keys that pick along a row.
var (
	left  = tea.KeyPressMsg{Code: tea.KeyLeft}
	right = tea.KeyPressMsg{Code: tea.KeyRight}
)

// pickingFrame is the choices fixture with the picker already up: p pressed on
// qwen, the cursor on its first axis.
func pickingFrame(t *testing.T) model {
	t.Helper()
	frame, _ := choicesFrame(t, &fakeServers{})
	frame, cmd := press(t, frame, typed('p'))
	if cmd != nil {
		t.Fatal("the picks key fired a command; picking runs no action")
	}
	if frame.picker == nil {
		t.Fatal("p did not open the picker on an entry with choices")
	}
	return frame
}

// pickerLines is the picker as a person reads it, one line per axis: the box
// rendered through the real path and stripped of its frame. capacity is the
// axis lines the screen would have room for (pickerBox's own arithmetic).
func pickerLines(frame model, capacity int) []string {
	rows := strings.Split(frame.pickerBox(200, capacity+2), "\n")
	var lines []string
	for _, row := range rows[1 : len(rows)-1] {
		text := strings.TrimSuffix(strings.TrimRight(plain(row), " "), "│")
		text = strings.TrimRight(strings.TrimPrefix(text, "│ "), " ")
		if text != "" {
			lines = append(lines, text)
		}
	}
	return lines
}

// pickedOn asserts which option rides the chip in the picker's box — the mark
// the plain text no longer carries (pickedStyle, user-chosen over a star).
func pickedOn(t *testing.T, frame model, on []string, off []string) {
	t.Helper()
	box := frame.pickerBox(200, 15)
	for _, name := range on {
		if !strings.Contains(box, pickedStyle.Render(name)) {
			t.Errorf("option %q does not ride the picked chip", name)
		}
	}
	for _, name := range off {
		if strings.Contains(box, pickedStyle.Render(name)) {
			t.Errorf("option %q rides the picked chip, and is not the pick", name)
		}
	}
}

// savedPicks is the store as the next launch would read it: off the file, not
// off the frame that wrote it.
func savedPicks(t *testing.T, frame model) picks.Picks {
	t.Helper()
	stored, err := picks.Load(frame.root)
	if err != nil {
		t.Fatalf("reading the stored picks back: %v", err)
	}
	if !reflect.DeepEqual(stored, frame.stored) {
		t.Errorf("the file holds %v and the frame holds %v", stored, frame.stored)
	}
	return stored
}

// p opens the picker over the list: one row per axis in the file's order, the
// options along it, the current pick marked — and the detail pane beside it is
// untouched, because watching the command line follow the picks is the loop the
// picker exists for (docs/specs/TUI.md).
func TestThePicksKeyOpensThePickerOverTheList(t *testing.T) {
	frame := pickingFrame(t)

	if frame.picker.entry != "qwen" || frame.picker.cursor != 0 {
		t.Errorf("the key opened %+v, want the picker on qwen's first axis", frame.picker)
	}
	if noticeLine(frame) != "" {
		t.Errorf("the line under the box reads %q; the pane names the entry and the bar names the keys", noticeLine(frame))
	}
	assertNoPicksWritten(t, frame)

	want := []string{"▸ quant:   q4  q6  q8", "  layout:  coding  chat"}
	if got := pickerLines(frame, 12); !reflect.DeepEqual(got, want) {
		t.Errorf("the picker draws\n%q\nwant\n%q", got, want)
	}
	pickedOn(t, frame, []string{"q4", "coding"}, []string{"q6", "q8", "chat"})

	// The box floats over the list's corner: the list's own title stays drawn
	// above it saying what is underneath, and the detail pane beside it keeps
	// showing what these picks would run.
	raw := frame.serveScreen(200, 14)
	screen := plain(raw)
	for _, part := range []string{picksTitle + titleSeparator + "qwen",
		"serve" + titleSeparator + "llama",
		"-hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", detailTitle} {
		if !strings.Contains(screen, part) {
			t.Errorf("the serve view does not draw %q while the picker is up:\n%s", part, screen)
		}
	}
	if !strings.Contains(raw, pickedStyle.Render("q4")) {
		t.Error("the pick rides no chip anywhere on the composed screen")
	}

	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{picksScope + " ←→ pick · ⏎ done · esc done", "q quit"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}
	for _, gone := range []string{"⏎ start", "p picks", "m move", "c cache", "s stop", "esc back"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the keybar reads %q, want it not to offer %q while the picker is up", bar, gone)
		}
	}
}

// assertNoPicksWritten says nothing was recorded: the state root of a test
// starts empty, so a key that picked nothing leaves it that way.
func assertNoPicksWritten(t *testing.T, frame model) {
	t.Helper()
	written, err := os.ReadDir(frame.root)
	if err != nil {
		t.Fatalf("reading the state root back: %v", err)
	}
	if len(written) != 0 {
		t.Errorf("the state root holds %d files after a keypress that recorded nothing", len(written))
	}
}

// A flat entry has nothing to pick, so the key is neither drawn nor live there —
// and a key the bar does not draw does nothing when pressed (docs/specs/TUI.md).
func TestAFlatEntryOffersNoPicker(t *testing.T) {
	cases := map[string]func(*testing.T) model{
		"an entry with no choices": func(t *testing.T) model {
			frame, _ := serveFrame(t)
			return frame.reselect(1) // qwen, one file per variation
		},
		"an entry file cria refused": func(t *testing.T) model {
			frame, _ := choicesFrame(t, &fakeServers{})
			return frame.reselect(2)
		},
		"the cache view": func(t *testing.T) model {
			frame, _ := choicesFrame(t, &fakeServers{})
			return frame.show(viewCache)
		},
	}

	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			frame := open(t)
			if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "p picks") {
				t.Errorf("the keybar offers the picker here: %q", bar)
			}

			pressed, cmd := press(t, frame, typed('p'))
			if cmd != nil {
				t.Error("the picks key fired a command where it does not apply")
			}
			if pressed.picker != nil {
				t.Errorf("the picks key opened %+v where it does not apply", pressed.picker)
			}
		})
	}
}

// The keybar carries the hint exactly where it works: the entry with axes, and
// no other row of the same list.
func TestTheKeybarOffersThePickerOnlyForEntriesWithChoices(t *testing.T) {
	frame, _ := choicesFrame(t, &fakeServers{})

	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "⏎ start · p picks") {
		t.Errorf("the keybar reads %q, want the picker beside the start it varies", bar)
	}
	if bar := plain(renderKeybar(200, frame.reselect(0).groups()...)); strings.Contains(bar, "p picks") {
		t.Errorf("the keybar offers the picker for gemma, which has no axes: %q", bar)
	}
}

// The cursor walks the rows and stops at both ends; the options wrap, because a
// pick is not a carry — there is no order of the user's own to protect at the
// ends (docs/specs/TUI.md).
func TestThePickerWalksItsRowsAndWrapsItsOptions(t *testing.T) {
	frame := pickingFrame(t)

	frame, _ = pressAll(t, frame, typed('j'), typed('j'))
	if frame.picker.cursor != 1 {
		t.Errorf("j past the last axis landed on row %d, want the second and last", frame.picker.cursor)
	}
	frame, _ = pressAll(t, frame, typed('k'), typed('k'))
	if frame.picker.cursor != 0 {
		t.Errorf("k past the first axis landed on row %d, want the first", frame.picker.cursor)
	}

	// Backwards off the first option lands on the last, and forwards off the
	// last lands on the first.
	rolled := []string{}
	for _, pressed := range []tea.KeyPressMsg{left, left, right, right, right} {
		frame, _ = press(t, frame, pressed)
		rolled = append(rolled, frame.stored["qwen"]["quant"])
	}
	if want := []string{"q8", "q6", "q8", "q4", "q6"}; !reflect.DeepEqual(rolled, want) {
		t.Errorf("←←→→→ picked %q, want %q — wrapping at both ends", rolled, want)
	}

	// The pick lands on the row the cursor is on, and only on it.
	frame, _ = pressAll(t, frame, typed('j'), right)
	if got := frame.stored["qwen"]; got["layout"] != "chat" || got["quant"] != "q6" {
		t.Errorf("the second row's pick left %v, want layout chat and quant q6 untouched", got)
	}

	// h and l are the same gesture, for the hand that never leaves the home row.
	frame, _ = pressAll(t, frame, typed('h'), typed('h'))
	if got := frame.stored["qwen"]["layout"]; got != "chat" {
		t.Errorf("h twice around a two-option axis left %q, want it back on %q", got, "chat")
	}
}

// Every change is written at the keypress, so leaving the picker is a way out
// and never a discard (docs/specs/TUI.md). The write is where the store is
// tidied: a pick whose entry the tree no longer holds goes with it.
func TestEveryPickIsWrittenAtTheKeypress(t *testing.T) {
	frame := pickingFrame(t)
	frame.stored = picks.Picks{
		"qwen":  {"layout": "chat"},
		"ghost": {"quant": "q4"},
	}

	frame, cmd := press(t, frame, right)
	if cmd != nil {
		t.Error("the pick fired a command; the write happens where the key lands")
	}
	if frame.alert.text != "" {
		t.Errorf("the pick reported %q; the row and the command line under it are the report", frame.alert.text)
	}
	if frame.picker == nil {
		t.Error("the pick closed the picker; ⏎ and esc are what close it")
	}

	want := picks.Picks{"qwen": {"quant": "q6", "layout": "chat"}}
	if got := savedPicks(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the store holds %v, want %v — the pick written and the ghost pruned", got, want)
	}

	// The row shows what was written, and only the axis that moved changed.
	if got, want := pickerLines(frame, 12), "▸ quant:   q4  q6  q8"; got[0] != want {
		t.Errorf("the picked row reads %q, want %q", got[0], want)
	}
	pickedOn(t, frame, []string{"q6"}, []string{"q4", "q8"})
}

// A store cria cannot write is not a pick that did not happen: the picker stays
// up showing it, and the line under the box says what was lost (grouppick.go's
// doctrine for the preferences).
func TestAFailedWriteKeepsThePickerUpAndSaysSo(t *testing.T) {
	frame := pickingFrame(t)

	// A state root cria cannot create: its parent is a file.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("arranging an unwritable state root: %v", err)
	}
	frame.root = filepath.Join(blocked, "state")

	frame, _ = press(t, frame, right)
	if frame.picker == nil {
		t.Fatal("a failed write closed the picker; the user decides whether to keep going")
	}
	if !frame.alert.bad || !strings.Contains(frame.alert.text, "state directory") {
		t.Errorf("the line under the box reads %q (bad %v), want the write's own failure", frame.alert.text, frame.alert.bad)
	}
	if got := frame.stored["qwen"]["quant"]; got != "q6" {
		t.Errorf("the session forgot the pick as well: %q", got)
	}
	if got, want := pickerLines(frame, 12)[0], "▸ quant:   q4  q6  q8"; got != want {
		t.Errorf("the row reads %q, want %q — the pick is still shown", got, want)
	}
	pickedOn(t, frame, []string{"q6"}, []string{"q4"})
}

// ⏎ and esc both just close: every pick was written as it was made, so there is
// nothing to confirm and nothing to throw away (docs/specs/TUI.md).
func TestEnterAndEscBothCloseThePicker(t *testing.T) {
	for name, closing := range map[string]tea.KeyPressMsg{"⏎": enter, "esc": escape} {
		t.Run(name, func(t *testing.T) {
			frame := pickingFrame(t)
			frame, _ = press(t, frame, right)

			closed, cmd := press(t, frame, closing)
			if cmd != nil {
				t.Error("closing the picker fired a command")
			}
			if closed.picker != nil {
				t.Errorf("%s left the picker up: %+v", name, closed.picker)
			}
			if got := closed.stored["qwen"]["quant"]; got != "q6" {
				t.Errorf("%s discarded the pick: %q", name, got)
			}
			// The list is back, drawn exactly as it draws itself.
			if !strings.Contains(plain(closed.serveScreen(120, 14)), "serve"+titleSeparator+"llama") {
				t.Error("closing the picker did not put the entry list back")
			}
		})
	}
}

// esc answers what is on screen, in order: the picker while it is up, then the
// notice a failed write left behind — and the bar names whichever is next
// (docs/specs/TUI.md).
func TestEscLeavesThePickerBeforeItDismissesANotice(t *testing.T) {
	frame := pickingFrame(t)
	frame.alert = alert{text: "cannot write the stored picks", bad: true}

	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc done") || strings.Contains(bar, "esc dismiss") {
		t.Errorf("the bar reads %q while the picker is up, want esc to close it", bar)
	}

	frame, _ = press(t, frame, escape)
	if frame.picker != nil {
		t.Fatal("esc left the picker up")
	}
	if frame.alert.text == "" {
		t.Error("esc took the notice with the picker; the failed write is the only thing saying so")
	}

	bar = plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc dismiss") {
		t.Errorf("the bar reads %q after the picker closed, want esc to dismiss the notice", bar)
	}
	frame, _ = press(t, frame, escape)
	if frame.alert.text != "" {
		t.Errorf("the second esc left the notice up: %q", frame.alert.text)
	}
}

// The detail pane follows every keypress: the picks it marks and the command it
// spells are the ones the picker just wrote (docs/specs/TUI.md — picking and
// seeing the command are one loop).
func TestTheDetailPaneFollowsAPickMadeInThePicker(t *testing.T) {
	frame := pickingFrame(t)

	frame, _ = press(t, frame, right)
	drawn := strings.Join(frame.detailLines(200, 30), "\n")
	if !strings.Contains(plain(drawn), "-hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL") {
		t.Errorf("the command line does not follow the pick while the picker is up:\n%s", plain(drawn))
	}
	if !strings.Contains(drawn, pickedStyle.Render("q6")) || strings.Contains(drawn, pickedStyle.Render("q4")) {
		t.Errorf("the pane's chip does not follow the pick while the picker is up:\n%s", plain(drawn))
	}

	frame, _ = pressAll(t, frame, typed('j'), right)
	if detail := plain(strings.Join(frame.detailLines(200, 30), "\n")); !strings.Contains(detail, "--parallel 1") {
		t.Errorf("the command line does not carry the second axis's pick:\n%s", detail)
	}
}

// The cached mark answers for the model a start would actually fetch: an entry
// whose quant lives in its options is only as cached as its current pick, and a
// pick made in the picker moves the mark on the same frame (docs/specs/TUI.md).
func TestTheCachedMarkFollowsThePicks(t *testing.T) {
	frame := pickingFrame(t)

	// The cache holds q4's quantization and no other (cachedQwen).
	if row := list(frame, 10)[1]; !strings.Contains(row, cachedMark+"  qwen") {
		t.Errorf("qwen's row reads %q, want the cached mark for the pick that is on disk", row)
	}
	if word := plain(strings.Join(frame.detailLines(200, 30), "\n")); !strings.Contains(word, "yes — starting it serves") {
		t.Errorf("the detail pane does not say the current pick is on disk:\n%s", word)
	}

	frame, _ = press(t, frame, right) // q6, which the cache does not hold
	if row := list(frame, 10)[1]; !strings.Contains(row, absentMark+"  qwen") {
		t.Errorf("qwen's row reads %q, want the absent mark once the pick moved off the cached quant", row)
	}
	if word := plain(strings.Join(frame.detailLines(200, 30), "\n")); !strings.Contains(word, "no — starting it downloads first") {
		t.Errorf("the detail pane does not say the new pick downloads first:\n%s", word)
	}

	frame, _ = press(t, frame, left) // back to q4
	if row := list(frame, 10)[1]; !strings.Contains(row, cachedMark+"  qwen") {
		t.Errorf("qwen's row reads %q, want the mark back on the cached pick", row)
	}
}

// A cache cria has not read says nothing about a resolved pick either: "not
// cached" would be a claim it has not earned (CODING-RULES §4).
func TestTheCachedMarkIsUnknownWithoutAWalk(t *testing.T) {
	frame, _ := choicesFrame(t, &fakeServers{})
	frame.cache = nil

	if row := list(frame, 10)[1]; !strings.Contains(row, unknownMark+"  qwen") {
		t.Errorf("qwen's row reads %q, want the unknown mark before the first walk", row)
	}
}

// The mode ends when what it stands on goes: an entry file edited while cria is
// open is the expected way an entry changes (docs/cria.md, principle 5).
func TestThePickerClosesWhenTheEntryLosesItsAxes(t *testing.T) {
	frame, world := choicesFrame(t, &fakeServers{})
	frame, _ = press(t, frame, typed('p'))
	if frame.picker == nil {
		t.Fatal("p did not open the picker")
	}

	flat := choicesTree()
	flat.Entries[2].Choices = nil
	flat.Entries[2].Quant = "UD-Q4_K_XL"
	world.tree = flat

	frame = load(t, frame)
	if frame.picker != nil {
		t.Errorf("the picker stands on axes the tree no longer declares: %+v", frame.picker)
	}
}

// The frame reads the store once and writes it through, so what the picker wrote
// is what a start launches — no re-read, no second reading to disagree with
// (docs/specs/CONFIG.md, Choices).
func TestAPickIsWhatTheNextStartLaunches(t *testing.T) {
	fake := &fakeServers{}
	frame, _ := choicesFrame(t, fake)
	frame, _ = pressAll(t, frame, typed('p'), right, escape)

	_, cmd := press(t, frame, enter)
	if msg, ok := run(t, cmd).(startedMsg); !ok || msg.err != nil {
		t.Fatalf("the start answered %+v", msg)
	}
	if want := (config.Selection{"quant": "q6", "layout": "coding"}); !reflect.DeepEqual(fake.picked[0], want) {
		t.Errorf("the start composed %v, want the picked %v", fake.picked[0], want)
	}
}
