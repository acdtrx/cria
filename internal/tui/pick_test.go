package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"cria/internal/serve"
)

// serverNamed is one more server in the box: liveStatus under another entry's
// name, port and log, in the phase the case is about.
func serverNamed(id string, port int, phase serve.Phase) serve.Status {
	status := liveStatus(phase)
	status.EntryID, status.Port = id, port
	status.LogPath = "/home/u/.local/state/cria/logs/" + id + ".log"
	return status
}

// pickFrame is a frame watching a box full of servers, observed the way a
// refresh hands them over. It is drawn wide so a banded row is compared at its
// full width rather than against a truncation.
func pickFrame(t *testing.T, servers ...serve.Status) (model, *fakeServers) {
	t.Helper()
	fake := &fakeServers{listing: serve.StatusListing{Servers: servers}}
	frame, _, _ := startFrame(t, fake)
	frame.width, frame.height = 200, 40
	return frame.observed(snapshotMsg{listing: fake.listing}), fake
}

// pickedID is the entry the pick cursor is standing on.
func pickedID(t *testing.T, frame model) string {
	t.Helper()
	record, picked := frame.pickedRecord()
	if !picked {
		t.Fatal("the frame is not picking a server")
	}
	return record.EntryID
}

// A key that could mean any of several servers asks which one rather than
// choosing for the user: it arms itself, the box becomes its picker, and the
// line under the box carries the question — which is exactly what the boxes
// cannot show (tui.go).
func TestSeveralServersMakeTheKeyAsk(t *testing.T) {
	frame, fake := pickFrame(t,
		serverNamed("qwen", 8080, serve.PhaseRunning),
		serverNamed("gemma", 8081, serve.PhaseRunning))

	frame, cmd := press(t, frame, typed('s'))
	if cmd != nil {
		t.Fatal("a key with two servers to choose between acted on one of them")
	}
	if len(fake.stopped) != 0 {
		t.Fatalf("the key stopped %v before it was told which server it meant", fake.stopped)
	}
	if frame.pick == nil || frame.pick.action != pickStop {
		t.Fatalf("the key armed %+v, want the stop waiting for its server", frame.pick)
	}
	if want := "which server to stop"; frame.alert.text != want {
		t.Errorf("the line under the box reads %q, want %q", frame.alert.text, want)
	}

	// The bar names the question and the two answers to it, and offers nothing
	// the box in front of the user does not answer to.
	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{"stop which", "⏎ stop", "esc cancel", "q quit"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}
	for _, gone := range []string{"K kill", "l log", "r restart", "c cache", "⏎ start"} {
		if strings.Contains(bar, gone) {
			t.Errorf("the keybar reads %q, want it not to offer %q while the key is asking", bar, gone)
		}
	}

	// The picked row is drawn on the same band the lists use, marker included,
	// and the row beside it is not.
	lines := statusLines(frame.listing, frame.prefs, frame.bar, frame.boxCursor())
	assertBanded(t, lines[0], lines[1], frame.frameWidth()-4)
	if drawn := plain(lines[0]); !strings.HasPrefix(drawn, cursorMark+"qwen") {
		t.Errorf("the picked row reads %q, want the cursor's mark in front of it", drawn)
	}
	if drawn := plain(lines[1]); !strings.HasPrefix(drawn, nothingHere+"gemma") {
		t.Errorf("the row beside it reads %q, want it to keep the marker's two cells", drawn)
	}
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "which server to stop") {
		t.Errorf("the frame does not ask which server:\n%s", drawn)
	}
}

// The cursor moves between the rows the armed key can act on and stands nowhere
// else: a stop skips a crash report, a dismiss skips a running server.
func TestThePickCursorMovesOverEligibleRowsOnly(t *testing.T) {
	servers := []serve.Status{
		serverNamed("qwen", 8080, serve.PhaseRunning),
		serverNamed("gemma", 8081, serve.PhaseExited),
		serverNamed("mlx-qwen", 8082, serve.PhaseRunning),
		serverNamed("phi", 8083, serve.PhaseExited),
	}

	cases := []struct {
		name  string
		key   tea.KeyPressMsg
		first string
		next  string
		last  string
	}{
		{name: "stop skips the crash reports", key: typed('s'), first: "qwen", next: "mlx-qwen", last: "mlx-qwen"},
		{name: "dismiss skips the running servers", key: typed('d'), first: "gemma", next: "phi", last: "phi"},
		{name: "the log reads either", key: typed('l'), first: "qwen", next: "gemma", last: "phi"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame, _ := pickFrame(t, servers...)
			frame, _ = press(t, frame, test.key)
			if got := pickedID(t, frame); got != test.first {
				t.Fatalf("the cursor started on %q, want %q — the first row the key can act on", got, test.first)
			}

			frame, _ = press(t, frame, typed('j'))
			if got := pickedID(t, frame); got != test.next {
				t.Errorf("j moved the cursor to %q, want %q", got, test.next)
			}
			frame, _ = press(t, frame, typed('k'))
			if got := pickedID(t, frame); got != test.first {
				t.Errorf("k moved the cursor to %q, want it back on %q", got, test.first)
			}

			// The cursor stops at the ends of what the key can act on rather
			// than wrapping onto a row it cannot answer for.
			frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyUp})
			if got := pickedID(t, frame); got != test.first {
				t.Errorf("↑ off the top of the eligible rows landed on %q, want %q", got, test.first)
			}
			for range len(servers) {
				frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyDown})
			}
			if got := pickedID(t, frame); got != test.last {
				t.Errorf("↓ off the bottom of the eligible rows landed on %q, want %q", got, test.last)
			}
		})
	}
}

// ⏎ runs the armed key on the server the cursor is on, and the mode goes with
// the answer: the question is answered, so the line that asked it clears.
func TestEnterRunsTheArmedKeyOnThePickedServer(t *testing.T) {
	cases := []struct {
		name  string
		key   tea.KeyPressMsg
		acted func(*fakeServers) []string
	}{
		{name: "stop", key: typed('s'), acted: func(f *fakeServers) []string { return f.stopped }},
		{name: "kill", key: typed('K'), acted: func(f *fakeServers) []string { return f.killed }},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame, fake := pickFrame(t,
				serverNamed("qwen", 8080, serve.PhaseRunning),
				serverNamed("gemma", 8081, serve.PhaseRunning))

			frame, _ = press(t, frame, test.key)
			frame, _ = press(t, frame, typed('j'))
			frame, cmd := press(t, frame, enter)

			if frame.pick != nil {
				t.Errorf("the mode stayed up after the answer: %+v", frame.pick)
			}
			if frame.alert.text != "" {
				t.Errorf("the frame says %q after the pick, want the question gone", frame.alert.text)
			}
			frame = answer(t, frame, cmd)

			if acted := test.acted(fake); len(acted) != 1 || acted[0] != "gemma" {
				t.Fatalf("the key acted on %q, want the picked server once", acted)
			}
			if frame.alert.text != "" {
				t.Errorf("the frame says %q after the action landed, want the line empty", frame.alert.text)
			}
			// The box is nobody's cursor again once the question is answered.
			if frame.boxCursor().picking {
				t.Error("the box kept its cursor after the pick")
			}
		})
	}
}

// A dismiss picks between crash reports, and clears the one it was pointed at.
func TestPickedDismissClearsTheRecordItLandedOn(t *testing.T) {
	frame, fake := pickFrame(t,
		serverNamed("gemma", 8081, serve.PhaseExited),
		serverNamed("phi", 8083, serve.PhaseExited))

	frame, _ = press(t, frame, typed('d'))
	frame, _ = press(t, frame, typed('j'))
	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)

	if len(fake.dismissed) != 1 || fake.dismissed[0] != "phi" {
		t.Fatalf("the dismiss cleared %q, want the picked record once", fake.dismissed)
	}
	if frame.pick != nil || frame.alert.text != "" {
		t.Errorf("the mode outlived the answer: %+v, %q", frame.pick, frame.alert.text)
	}
}

// The log reads either kind of record, so it picks across both — a running
// server's log and a crash report are the same key's targets
// (docs/specs/SERVE.md).
func TestTheLogPicksAcrossLiveAndExitedRecords(t *testing.T) {
	crashed := serverNamed("gemma", 8081, serve.PhaseExited)
	frame, _ := pickFrame(t, serverNamed("qwen", 8080, serve.PhaseRunning), crashed)

	frame, _ = press(t, frame, typed('l'))
	if frame.pick == nil || frame.pick.action != pickLog {
		t.Fatalf("the log key armed %+v, want it waiting for its record", frame.pick)
	}
	if want := "which server to log"; frame.alert.text != want {
		t.Errorf("the line under the box reads %q, want %q", frame.alert.text, want)
	}

	frame, _ = press(t, frame, typed('j'))
	frame, cmd := press(t, frame, enter)
	if !frame.log.open || frame.log.path != crashed.LogPath {
		t.Fatalf("the log opened %q, want the picked record's log %q", frame.log.path, crashed.LogPath)
	}
	if frame.pick != nil || frame.alert.text != "" {
		t.Errorf("the mode outlived the answer: %+v, %q", frame.pick, frame.alert.text)
	}
	if cmd == nil {
		t.Error("the picked log did not go and read the file")
	}
}

// esc leaves the question unanswered and nothing acted on.
func TestEscCancelsThePick(t *testing.T) {
	frame, fake := pickFrame(t,
		serverNamed("qwen", 8080, serve.PhaseRunning),
		serverNamed("gemma", 8081, serve.PhaseRunning))

	frame, _ = press(t, frame, typed('s'))
	frame, cmd := press(t, frame, escape)

	if cmd != nil {
		t.Error("the cancelled pick ran something")
	}
	if frame.pick != nil {
		t.Errorf("esc left the mode up: %+v", frame.pick)
	}
	if frame.alert.text != "" {
		t.Errorf("esc left %q under the box, want the question gone with it", frame.alert.text)
	}
	if len(fake.stopped) != 0 || len(fake.killed) != 0 {
		t.Errorf("the cancelled pick stopped %v and killed %v", fake.stopped, fake.killed)
	}
	if frame.boxCursor().picking {
		t.Error("the box kept its cursor after the cancel")
	}
	// The bar is the frame's own again.
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "s stop") || strings.Contains(bar, "stop which") {
		t.Errorf("the keybar reads %q, want the frame's own keys back", bar)
	}
}

// The mode holds the keyboard while it is up: every other key would answer
// something the user is in the middle of being asked.
func TestPickHoldsTheKeyboard(t *testing.T) {
	frame, fake := pickFrame(t,
		serverNamed("qwen", 8080, serve.PhaseRunning),
		serverNamed("gemma", 8081, serve.PhaseRunning))
	frame, _ = press(t, frame, typed('s'))

	for _, ignored := range []tea.KeyPressMsg{typed('c'), {Code: tea.KeyTab}, typed('x'), typed('r'), typed('t')} {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, ignored)
		if cmd != nil {
			t.Errorf("%q fired a command from under the pick", ignored.String())
		}
	}

	if frame.view != viewServe {
		t.Errorf("a key moved the frame to %v while it was being asked which server", frame.view)
	}
	if frame.prefs.Backend != defaultPrefs().Backend {
		t.Errorf("a key switched the backend to %q while the pick was up", frame.prefs.Backend)
	}
	if frame.toolsOpen || frame.log.open || frame.modal != nil || frame.confirm != nil {
		t.Error("a key opened another screen from under the pick")
	}
	if len(fake.started) != 0 || len(fake.stopped) != 0 {
		t.Errorf("the pick let a key through: started %v, stopped %v", fake.started, fake.stopped)
	}
	if frame.pick == nil {
		t.Fatal("the question went away by itself")
	}

	// And the one key that does work still does.
	frame, _ = press(t, frame, escape)
	if frame.pick != nil {
		t.Error("esc did not leave the pick")
	}
}

// The rows a key can be pointed at are re-read from the listing rather than
// remembered: a record that goes while the question is up takes its row with it,
// and one that leaves nothing to pick takes the question too.
func TestThePickFollowsTheListing(t *testing.T) {
	frame, _ := pickFrame(t,
		serverNamed("qwen", 8080, serve.PhaseRunning),
		serverNamed("gemma", 8081, serve.PhaseRunning))

	frame, _ = press(t, frame, typed('s'))
	frame, _ = press(t, frame, typed('j'))
	if got := pickedID(t, frame); got != "gemma" {
		t.Fatalf("the cursor is on %q, want gemma", got)
	}

	// The server under the cursor exits: the cursor is left on a row that is
	// still there rather than pointing past the end of the box.
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{Servers: []serve.Status{
		serverNamed("qwen", 8080, serve.PhaseRunning),
	}}})
	if got := pickedID(t, frame); got != "qwen" {
		t.Errorf("the cursor is on %q after the picked server exited, want qwen", got)
	}

	// Nothing left to stop at all: the next keypress leaves the mode rather
	// than acting on a server that is gone.
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{}})
	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("⏎ acted with nothing left to act on")
	}
	if frame.pick != nil {
		t.Errorf("the question outlived every server it could have asked about: %+v", frame.pick)
	}
}
