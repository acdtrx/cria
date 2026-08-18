package tui

import (
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/serve"
)

// liveStatus is a server as one observation reports it, in the phase the test
// is about (docs/specs/SERVE.md).
func liveStatus(phase serve.Phase) serve.Status {
	return serve.Status{
		Record: serve.Record{
			EntryID:    "qwen",
			Backend:    config.BackendLlama,
			Repo:       "unsloth/Qwen3-30B-A3B-GGUF",
			Quant:      "UD-Q4_K_XL",
			Host:       "0.0.0.0",
			Port:       8080,
			PID:        4242,
			Identity:   procs.Identity{Command: "llama-server", StartedAt: "Tue Aug 18 14:57:30 2026"},
			Command:    []string{"llama-server"},
			LogPath:    "/home/u/.local/state/cria/logs/qwen-20260818-145730.log",
			LaunchedAt: time.Date(2026, 8, 18, 14, 57, 30, 0, time.UTC),
		},
		Phase:  phase,
		Uptime: 3*time.Minute + 12*time.Second,
		Stats:  procs.Stats{RSSBytes: 21474836480, CPUPercent: 42.5},
		Health: serve.Health{URL: "http://127.0.0.1:8080/health", Green: phase == serve.PhaseRunning, Status: 200, Detail: "200 OK"},
	}
}

// box is the status box's plain text, one line per line, drawn the way it is
// whenever no key is asking which server it means (pick.go).
func box(listing serve.StatusListing, saved prefs) []string {
	var lines []string
	for _, line := range statusLines(listing, saved, newProgressBar(), boxCursor{}) {
		lines = append(lines, plain(line))
	}
	return lines
}

// A running server's line carries every fact docs/specs/TUI.md asks the box
// for: what it is, what it costs, and what it last answered.
func TestRunningStatusShowsTheFacts(t *testing.T) {
	lines := box(serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}, defaultPrefs())
	if len(lines) != 1 {
		t.Fatalf("a running server drew %d lines, want 1: %q", len(lines), lines)
	}

	for _, fact := range []string{"qwen", "running", "llama", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"pid 4242", ":8080", "up 3m12s", "20.0 GiB", "42.5% cpu", "200 OK"} {
		if !strings.Contains(lines[0], fact) {
			t.Errorf("the status line reads %q, want it to carry %q", lines[0], fact)
		}
	}
}

// A pid the process table had nothing to say about costs the line its two
// numbers rather than reporting a server that uses no memory.
func TestUnmeasuredServerReportsNoCost(t *testing.T) {
	status := liveStatus(serve.PhaseRunning)
	status.Stats = procs.Stats{}

	lines := box(serve.StatusListing{Servers: []serve.Status{status}}, defaultPrefs())
	if strings.Contains(lines[0], "0 B") || strings.Contains(lines[0], "0.0% cpu") {
		t.Errorf("the status line reads %q, want no cost claimed for a pid `ps` did not answer for", lines[0])
	}
}

// Starting and unhealthy are the same line as running, in their own word: the
// phase is the judgement, and nothing else about the server changes shape.
func TestEveryLivePhaseDrawsOneLine(t *testing.T) {
	for _, phase := range []serve.Phase{serve.PhaseStarting, serve.PhaseRunning, serve.PhaseUnhealthy} {
		lines := box(serve.StatusListing{Servers: []serve.Status{liveStatus(phase)}}, defaultPrefs())
		if len(lines) != 1 {
			t.Fatalf("the %s phase drew %d lines, want 1: %q", phase, len(lines), lines)
		}
		if !strings.Contains(lines[0], string(phase)) {
			t.Errorf("the %s line reads %q, want it to name its phase", phase, lines[0])
		}
	}
}

// A download gets a bar and its two byte counts, from the cache and the Hub
// (docs/specs/SERVE.md).
func TestDownloadingStatusShowsProgress(t *testing.T) {
	status := liveStatus(serve.PhaseDownloading)
	status.Progress = serve.Progress{Bytes: 6 * 1024 * 1024 * 1024, Total: 24 * 1024 * 1024 * 1024, Known: true}

	lines := box(serve.StatusListing{Servers: []serve.Status{status}}, defaultPrefs())
	if len(lines) != 2 {
		t.Fatalf("a download drew %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "downloading") {
		t.Errorf("the status line reads %q, want the phase", lines[0])
	}
	if !strings.Contains(lines[1], "6.0 GiB of 24.0 GiB") {
		t.Errorf("the progress line reads %q, want both byte counts", lines[1])
	}
	if !strings.Contains(lines[1], "25%") {
		t.Errorf("the progress line reads %q, want the percentage the bar carries", lines[1])
	}
}

// A Hub that could not be asked costs the download its bar, not its progress:
// the bytes on disk are the honest half, and the reason travels with them.
func TestDownloadWithoutATotalStillReportsBytes(t *testing.T) {
	status := liveStatus(serve.PhaseDownloading)
	status.Progress = serve.Progress{Bytes: 3 * 1024 * 1024 * 1024, Reason: "the Hub did not answer"}

	lines := box(serve.StatusListing{Servers: []serve.Status{status}}, defaultPrefs())
	if want := "3.0 GiB so far (no total: the Hub did not answer)"; !strings.Contains(lines[1], want) {
		t.Errorf("the progress line reads %q, want %q", lines[1], want)
	}
}

// An exited record is a crash report: what died, when it was launched, and the
// log that says why. Nothing is claimed about what it costs or answers.
func TestExitedStatusIsACrashReport(t *testing.T) {
	lines := box(serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseExited)}}, defaultPrefs())
	if len(lines) != 2 {
		t.Fatalf("an exited record drew %d lines, want 2: %q", len(lines), lines)
	}

	for _, fact := range []string{"qwen", "exited", "pid 4242 is gone", "launched 2026-08-18 14:57:30"} {
		if !strings.Contains(lines[0], fact) {
			t.Errorf("the crash line reads %q, want it to carry %q", lines[0], fact)
		}
	}
	if !strings.Contains(lines[1], "/home/u/.local/state/cria/logs/qwen-20260818-145730.log") {
		t.Errorf("the crash report reads %q, want the log path", lines[1])
	}
	if strings.Contains(lines[0], "200 OK") || strings.Contains(lines[0], "up 3m12s") {
		t.Errorf("the crash line reads %q, want nothing claimed about a process that is gone", lines[0])
	}
}

// Nothing in the state directory is the stopped state: the box falls back to the
// entry that was started last, so the server keys keep a target across sessions
// (docs/specs/TUI.md).
func TestStoppedStatusComesFromPreferences(t *testing.T) {
	lines := box(serve.StatusListing{}, prefs{Backend: config.BackendLlama, LastStarted: "qwen"})
	if len(lines) != 1 {
		t.Fatalf("the stopped box drew %d lines, want 1: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "qwen") || !strings.Contains(lines[0], "stopped") {
		t.Errorf("the stopped line reads %q, want the last-started entry in a stopped state", lines[0])
	}
}

// A host where nothing has ever been started says so, rather than showing an
// empty box.
func TestStoppedStatusWithoutAHistorySaysSo(t *testing.T) {
	lines := box(serve.StatusListing{}, defaultPrefs())
	if want := "no server has been started yet"; !strings.Contains(lines[0], want) {
		t.Errorf("the stopped line reads %q, want %q", lines[0], want)
	}
}

// Several servers at once stack their lines — entries declaring different ports
// (docs/cria.md, v1 surface).
func TestSeveralServersStack(t *testing.T) {
	second := liveStatus(serve.PhaseRunning)
	second.EntryID, second.Port = "gemma", 8081

	lines := box(serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning), second}}, defaultPrefs())
	if len(lines) != 2 {
		t.Fatalf("two servers drew %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "qwen") || !strings.Contains(lines[1], "gemma") {
		t.Errorf("the box reads %q, want one line per server", lines)
	}

	// The lines are one table: every shared fact starts at the same column on
	// both rows, however long the ids and phases above it were.
	for _, fact := range []string{"running", "llama", "unsloth/", "pid ", "up "} {
		if a, b := runeColumn(lines[0], fact), runeColumn(lines[1], fact); a != b || a < 0 {
			t.Errorf("column %q is not aligned: %d vs %d\n%q\n%q", fact, a, b, lines[0], lines[1])
		}
	}
}

// The table holds across states: an exited server's first columns line up under
// a live one's, so the box reads as one grid however the servers are doing.
func TestTheTableHoldsAcrossPhases(t *testing.T) {
	dead := liveStatus(serve.PhaseExited)
	dead.EntryID = "gemma-27b"

	lines := box(serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning), dead}}, defaultPrefs())
	if len(lines) != 3 {
		t.Fatalf("a live and an exited server drew %d lines, want 3 (the crash log line included): %q", len(lines), lines)
	}
	for _, fact := range []string{"llama", "unsloth/"} {
		if a, b := runeColumn(lines[0], fact), runeColumn(lines[1], fact); a != b || a < 0 {
			t.Errorf("column %q is not aligned across phases: %d vs %d\n%q\n%q", fact, a, b, lines[0], lines[1])
		}
	}
	if !strings.Contains(lines[1], "pid 4242 is gone") || !strings.Contains(lines[2], "log ") {
		t.Errorf("the exited row lost its crash report: %q", lines[1:])
	}
}

// A record file cria refused names a pid cria started: it is shown, with the one
// line that clears it, rather than dropped (CODING-RULES §4).
func TestBrokenRecordsAreShown(t *testing.T) {
	listing := serve.StatusListing{Broken: []serve.BrokenRecord{{
		EntryID: "gemma",
		Path:    "/home/u/.local/state/cria/servers/gemma.json",
		Err:     errors.New("port is 0, want a port between 1 and 65535"),
	}}}

	lines := box(listing, defaultPrefs())
	if len(lines) != 2 {
		t.Fatalf("a broken record drew %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "unreadable record") || !strings.Contains(lines[0], "gemma.json") {
		t.Errorf("the broken line reads %q, want the record it refused", lines[0])
	}
	if !strings.Contains(lines[1], "port is 0") || !strings.Contains(lines[1], "delete that file") {
		t.Errorf("the broken line reads %q, want the reason and the fix", lines[1])
	}
}

// The phase decides the colour, and nothing else does. A phase cria does not
// know is drawn as context: an unrecognised word must never borrow the
// authority of green.
func TestPhaseToneMapping(t *testing.T) {
	cases := map[serve.Phase]color.Color{
		serve.PhaseRunning:     green,
		serve.PhaseDownloading: yellow,
		serve.PhaseStarting:    amber,
		serve.PhaseUnhealthy:   red,
		serve.PhaseExited:      red,
		serve.Phase("wedged"):  dim,
	}

	for phase, want := range cases {
		if got := phaseColor(phase); got != want {
			t.Errorf("the %s phase is drawn in %v, want %v", phase, got, want)
		}
		if got := phaseTone(phase).GetForeground(); got != want {
			t.Errorf("the %s style carries %v, want %v", phase, got, want)
		}
	}
}

// The colours are actually applied: a running server's phase word is green on
// screen, and an exited record's whole line is drawn as a crash report.
func TestPhaseColoursReachTheLine(t *testing.T) {
	running := statusLines(serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}, defaultPrefs(), newProgressBar(), boxCursor{})
	if !strings.Contains(running[0], lipgloss.NewStyle().Foreground(green).Render("running")) {
		t.Errorf("the running line does not draw its phase in green: %q", running[0])
	}

	exited := statusLines(serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseExited)}}, defaultPrefs(), newProgressBar(), boxCursor{})
	if !strings.HasPrefix(exited[0], opener(alarmStyle)) {
		t.Errorf("the exited line does not open in the crash-report colour: %q", exited[0])
	}
}

// opener is the escape sequence a style opens with: how a test asks what colour
// a line was drawn in without spelling the palette out a second time.
func opener(style lipgloss.Style) string {
	rendered := style.Render("x")
	return rendered[:strings.Index(rendered, "x")]
}

// What the box shows is what the server keys act on (docs/specs/TUI.md), so the
// target is read off the same two things the box is drawn from.
func TestBoxTarget(t *testing.T) {
	exited := liveStatus(serve.PhaseExited)
	cases := []struct {
		name    string
		listing serve.StatusListing
		saved   prefs
		want    boxTarget
	}{
		{
			name:  "a fresh host shows nothing at all",
			saved: defaultPrefs(),
			want:  boxTarget{},
		},
		{
			name:  "a stopped box still names the last-started entry",
			saved: prefs{Backend: config.BackendLlama, LastStarted: "qwen"},
			want:  boxTarget{shown: true},
		},
		{
			name:    "a running server is live",
			listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}},
			saved:   defaultPrefs(),
			want:    boxTarget{live: true, shown: true},
		},
		{
			name:    "an exited record is a crash report to dismiss",
			listing: serve.StatusListing{Servers: []serve.Status{exited}},
			saved:   defaultPrefs(),
			want:    boxTarget{exited: true, shown: true},
		},
		{
			name:    "one of each is both",
			listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning), exited}},
			saved:   defaultPrefs(),
			want:    boxTarget{live: true, exited: true, shown: true},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := targetOf(test.listing, test.saved); got != test.want {
				t.Errorf("the box target is %+v, want %+v", got, test.want)
			}
		})
	}
}
