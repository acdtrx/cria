package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"cria/internal/serve"
)

// writeLog is a server's log file as the server left it.
func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qwen-20260818-145730.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("writing a log file: %v", err)
	}
	return path
}

// logFrame is a frame watching one running server whose log is at path.
func logFrame(t *testing.T, path string) (model, *fakeServers) {
	t.Helper()
	status := liveStatus(serve.PhaseRunning)
	status.LogPath = path
	fake := &fakeServers{listing: serve.StatusListing{Servers: []serve.Status{status}}}

	frame, _, _ := startFrame(t, fake)
	return frame.observed(snapshotMsg{listing: fake.listing}), fake
}

// l shows the log of what the box is targeting, raw: the lines the server
// printed, with nothing read out of them (docs/cria.md, principle 6).
func TestLogViewShowsTheRawTail(t *testing.T) {
	path := writeLog(t,
		"build: 7031 (abc1234) with cc",
		"main: loading model",
		"srv    load_model: loaded",
		"main: server is listening on http://0.0.0.0:8080")
	frame, _ := logFrame(t, path)

	frame, cmd := press(t, frame, typed('l'))
	if !frame.log.open {
		t.Fatal("the log key did not open the log")
	}
	frame = answer(t, frame, cmd)

	drawn := plain(frame.View().Content)
	for _, line := range []string{"log · qwen", "build: 7031 (abc1234) with cc", "main: server is listening on http://0.0.0.0:8080"} {
		if !strings.Contains(drawn, line) {
			t.Errorf("the log view does not carry %q:\n%s", line, drawn)
		}
	}
	// The status box is true in every view, and the bar says what works here.
	if !strings.Contains(drawn, statusTitle) {
		t.Errorf("the log view lost the status box:\n%s", drawn)
	}
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "esc close") || strings.Contains(bar, "s stop") {
		t.Errorf("the keybar reads %q, want the log's own keys while it is up", bar)
	}
}

// The log follows the file on the same ticker everything else runs on: no second
// timer, and no watching of a file another process owns (CODING-RULES §6).
func TestLogFollowsTheTicker(t *testing.T) {
	path := writeLog(t, "main: loading model")
	frame, _ := logFrame(t, path)

	frame, cmd := press(t, frame, typed('l'))
	frame = answer(t, frame, cmd)

	if err := os.WriteFile(path, []byte("main: loading model\nsrv update_slots: all slots are idle\n"), 0o644); err != nil {
		t.Fatalf("appending to the log: %v", err)
	}

	work := frame.tickWork()
	if len(work) != 4 {
		t.Fatalf("a tick with the log up ran %d commands, want the observation, the tree, the next tick and the log", len(work))
	}
	for _, each := range work {
		if msg, ok := each().(logMsg); ok {
			frame.log = frame.log.read(msg)
		}
	}
	if drawn := plain(frame.log.panel(80, 10)); !strings.Contains(drawn, "all slots are idle") {
		t.Errorf("the log did not follow the file:\n%s", drawn)
	}
}

// A log that has gone — pruned by a new launch, deleted by hand — keeps what it
// last showed and says what happened to the file.
func TestLogViewSurvivesAVanishedFile(t *testing.T) {
	path := writeLog(t, "main: loading model")
	frame, _ := logFrame(t, path)

	frame, cmd := press(t, frame, typed('l'))
	frame = answer(t, frame, cmd)
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the log: %v", err)
	}

	frame.log = frame.log.read(frame.readLog().(logMsg))
	drawn := plain(frame.log.panel(80, 10))
	if !strings.Contains(drawn, "cannot read the log") || !strings.Contains(drawn, "no such file") {
		t.Errorf("the log view does not say the file is gone:\n%s", drawn)
	}
	if !strings.Contains(drawn, "main: loading model") {
		t.Errorf("the log view dropped the last lines it had:\n%s", drawn)
	}
}

// esc and l both leave, and nothing underneath acts while the log is up.
func TestLogViewHoldsTheKeyboard(t *testing.T) {
	path := writeLog(t, "main: loading model")

	for name, leave := range map[string]tea.KeyPressMsg{"esc": escape, "l": typed('l')} {
		t.Run(name, func(t *testing.T) {
			frame, fake := logFrame(t, path)
			frame, cmd := press(t, frame, typed('l'))
			frame = answer(t, frame, cmd)

			frame, ignored := press(t, frame, typed('s'))
			if ignored != nil || len(fake.stopped) != 0 {
				t.Errorf("a stop fired from under the log view: %v", fake.stopped)
			}

			frame, _ = press(t, frame, leave)
			if frame.log.open {
				t.Error("the key did not leave the log view")
			}
		})
	}
}

// The log key follows the status box: a crashed server's log is the crash
// report, and there is nothing to read when nothing has run.
func TestLogKeyFollowsTheStatusBox(t *testing.T) {
	frame, _, _ := startFrame(t, &fakeServers{})
	frame, cmd := press(t, frame, typed('l'))
	if cmd != nil || frame.log.open {
		t.Error("the log key opened a log on a host where nothing has run")
	}

	exited := liveStatus(serve.PhaseExited)
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{Servers: []serve.Status{exited}}})
	frame, _ = press(t, frame, typed('l'))
	if !frame.log.open || frame.log.path != exited.LogPath {
		t.Errorf("the log key opened %q, want the exited record's log %q", frame.log.path, exited.LogPath)
	}
}

// The tail is the end of the file and a whole number of lines of it: a window
// that began mid-line drops that half-line, and a log longer than the tail keeps
// its end.
func TestTailLinesReadsWholeLinesOffTheEnd(t *testing.T) {
	lines := make([]string, 0, 500)
	for i := range 500 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	path := writeLog(t, lines...)

	read, err := tailLines(path, logTailLines)
	if err != nil {
		t.Fatalf("reading the tail: %v", err)
	}
	if len(read) != logTailLines {
		t.Fatalf("the tail is %d lines, want %d", len(read), logTailLines)
	}
	if read[0] != "line 300" || read[len(read)-1] != "line 499" {
		t.Errorf("the tail runs %q…%q, want the last %d lines", read[0], read[len(read)-1], logTailLines)
	}
}

// A log with nothing in it yet is not a log of one empty line.
func TestTailOfAnEmptyLogIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("writing an empty log: %v", err)
	}

	read, err := tailLines(path, logTailLines)
	if err != nil {
		t.Fatalf("reading the tail: %v", err)
	}
	if len(read) != 0 {
		t.Errorf("an empty log read as %q, want nothing", read)
	}
	if drawn := plain(logScreen{open: true, path: path}.panel(60, 6)); !strings.Contains(drawn, "nothing printed yet") {
		t.Errorf("an empty log draws as %q, want it to say so", drawn)
	}
}
