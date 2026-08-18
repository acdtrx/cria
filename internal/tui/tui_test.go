package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/serve"
)

// fakeServers is the observation the frame runs on: what one refresh answers.
// It is the whole seam between this package and a live host, so every display
// state and every contextual key is exercised with no state directory, no
// process table and no server (docs/specs/TUI.md).
type fakeServers struct {
	listing serve.StatusListing
	err     error
}

func (f *fakeServers) Snapshots() (serve.StatusListing, error) {
	if f.err != nil {
		return serve.StatusListing{}, f.err
	}
	return f.listing, nil
}

// testFrame is a model over a temporary state root, so a preferences write in a
// test lands nowhere near the host's own.
func testFrame(t *testing.T, fake *fakeServers) (model, string) {
	t.Helper()
	root := t.TempDir()
	saved, err := loadPrefs(root)
	if err != nil {
		t.Fatalf("loading the preferences of a fresh state root: %v", err)
	}
	frame := newModel(fake, root, saved, nil)
	frame.width, frame.height = 120, 40
	return frame, root
}

// press is one keystroke through Update, back as a model.
func press(t *testing.T, frame model, key tea.KeyPressMsg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := frame.Update(key)
	pressed, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned a %T, want the frame's own model", next)
	}
	return pressed, cmd
}

// typed is a printable key as the terminal reports it.
func typed(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// The frame draws its three parts in every view: the box that says what is
// running, the screen, and the bar that says what works right now.
func TestFrameDrawsBoxScreenAndKeybar(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})
	drawn := plain(frame.View().Content)

	for _, part := range []string{statusTitle, "no server has been started yet", "serve · llama",
		"under construction", "server", "global", "⇥ backend", "q quit"} {
		if !strings.Contains(drawn, part) {
			t.Errorf("the frame does not draw %q:\n%s", part, drawn)
		}
	}
}

// The view key switches screens, and the status box stays: it appears in every
// view, the cache view included (docs/specs/TUI.md).
func TestViewKeySwitchesScreens(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})

	frame, _ = press(t, frame, typed('v'))
	if frame.view != viewCache {
		t.Fatalf("the view key left the frame on %v, want the cache view", frame.view)
	}
	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "cache") {
		t.Errorf("the cache view does not name itself:\n%s", drawn)
	}
	if !strings.Contains(drawn, statusTitle) || !strings.Contains(drawn, "no server has been started yet") {
		t.Errorf("the status box is missing from the cache view:\n%s", drawn)
	}

	frame, _ = press(t, frame, typed('v'))
	if frame.view != viewServe {
		t.Errorf("the view key left the frame on %v, want it back on the serve view", frame.view)
	}
}

// The backend toggle is sticky: it changes what the serve view shows and it is
// written down, so the next launch opens on the same backend.
func TestBackendToggleIsWrittenDown(t *testing.T) {
	frame, root := testFrame(t, &fakeServers{})

	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	if frame.prefs.Backend != config.BackendMLX {
		t.Fatalf("the toggle left the backend at %q, want %q", frame.prefs.Backend, config.BackendMLX)
	}
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "serve · mlx") {
		t.Errorf("the serve view does not name the active backend:\n%s", drawn)
	}

	saved, err := loadPrefs(root)
	if err != nil {
		t.Fatalf("reading the preferences back: %v", err)
	}
	if saved.Backend != config.BackendMLX {
		t.Errorf("the next launch would open on %q, want %q", saved.Backend, config.BackendMLX)
	}

	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	if frame.prefs.Backend != config.BackendLlama {
		t.Errorf("the toggle left the backend at %q, want it back on %q", frame.prefs.Backend, config.BackendLlama)
	}
}

// Quit is quit, by either key.
func TestQuitKeys(t *testing.T) {
	for name, key := range map[string]tea.KeyPressMsg{
		"q":      typed('q'),
		"ctrl+c": {Code: 'c', Mod: tea.ModCtrl},
	} {
		t.Run(name, func(t *testing.T) {
			frame, _ := testFrame(t, &fakeServers{})
			_, cmd := press(t, frame, key)
			if cmd == nil {
				t.Fatal("the quit key returned no command")
			}
			if _, quit := cmd().(tea.QuitMsg); !quit {
				t.Errorf("the quit key returned %T, want a quit", cmd())
			}
		})
	}
}

// A key whose action is not built yet still says what it would do, rather than
// doing nothing visible.
func TestUnwiredKeysReportThemselves(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})
	frame, _ = press(t, frame, typed('t'))

	if !strings.Contains(frame.alert.text, "not wired yet") {
		t.Errorf("the tools key left %q on screen, want it to say the pane is not built", frame.alert.text)
	}
	if !strings.Contains(plain(frame.View().Content), "not wired yet") {
		t.Error("the frame does not draw what the last keypress did")
	}
}

// The server keys follow what the box shows, in the bar and on the keyboard
// alike: a key that is not drawn does nothing when pressed
// (docs/specs/TUI.md).
func TestServerKeysFollowTheStatusBox(t *testing.T) {
	cases := []struct {
		name    string
		listing serve.StatusListing
		last    string
		want    []string
		gone    []string
	}{
		{
			name: "a fresh host offers no server keys at all",
			gone: []string{"s stop", "l log", "r restart", "d dismiss"},
		},
		{
			name: "a stopped box still has something to restart",
			last: "qwen",
			want: []string{"r restart"},
			gone: []string{"s stop", "l log", "d dismiss"},
		},
		{
			name:    "a running server can be stopped and read",
			listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}},
			want:    []string{"s stop", "l log", "r restart"},
			gone:    []string{"d dismiss"},
		},
		{
			name:    "an exited record can be read and dismissed, not stopped",
			listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseExited)}},
			want:    []string{"l log", "r restart", "d dismiss"},
			gone:    []string{"s stop"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame, _ := testFrame(t, &fakeServers{listing: test.listing})
			frame.prefs.LastStarted = test.last
			frame = frame.observed(snapshotMsg{listing: test.listing})

			bar := plain(renderKeybar(200, frame.groups()...))
			for _, hint := range test.want {
				if !strings.Contains(bar, hint) {
					t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
				}
			}
			for _, hint := range test.gone {
				if strings.Contains(bar, hint) {
					t.Errorf("the keybar reads %q, want it not to offer %q", bar, hint)
				}
			}
		})
	}

	// A key the bar does not draw does nothing: the two must never disagree.
	frame, _ := testFrame(t, &fakeServers{})
	frame, _ = press(t, frame, typed('s'))
	if frame.alert.text != "" {
		t.Errorf("stop acted with nothing running: %q", frame.alert.text)
	}
}

// An observation that failed keeps the last good box on screen and says what
// could not be read — an empty box would claim nothing is running, which is a
// plausible-looking lie about a host cria could not ask (CODING-RULES §4).
func TestRefreshFailuresAreStickyUntilOneSucceeds(t *testing.T) {
	running := serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}
	frame, _ := testFrame(t, &fakeServers{})

	frame = frame.observed(snapshotMsg{listing: running})
	frame = frame.observed(snapshotMsg{err: errors.New("cannot read the server records")})

	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "cannot read the server records") {
		t.Errorf("the frame does not report the failed observation:\n%s", drawn)
	}
	if !strings.Contains(drawn, "running") {
		t.Errorf("the frame dropped the last good observation:\n%s", drawn)
	}

	// Every further failure keeps it there; only a reading supersedes it.
	frame = frame.observed(snapshotMsg{err: errors.New("cannot read the server records")})
	if frame.failure == nil {
		t.Error("a second failure cleared the first one")
	}
	frame = frame.observed(snapshotMsg{listing: running})
	if frame.failure != nil {
		t.Errorf("a successful observation left %v on screen", frame.failure)
	}
}

// The refresh runs off the UI thread and re-arms itself, so the box keeps up
// with a host nothing pushes from (docs/specs/SERVE.md).
func TestRefreshTickObservesAndRearms(t *testing.T) {
	fake := &fakeServers{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}}
	frame, _ := testFrame(t, fake)
	frame.interval = time.Millisecond

	next, cmd := frame.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("the tick returned no commands")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("the tick returned a %T, want an observation and the next tick", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("the tick returned %d commands, want the observation and the next tick", len(batch))
	}

	observed, rearmed := false, false
	for _, each := range batch {
		switch msg := each().(type) {
		case snapshotMsg:
			observed = true
			if len(msg.listing.Servers) != 1 {
				t.Errorf("the observation carried %d servers, want the one the host holds", len(msg.listing.Servers))
			}
		case tickMsg:
			rearmed = true
		}
	}
	if !observed {
		t.Error("the tick did not take an observation")
	}
	if !rearmed {
		t.Error("the tick did not arm the next one")
	}
	if _, ok := next.(model); !ok {
		t.Errorf("the tick returned a %T, want the frame's own model", next)
	}
}

// Preferences that could not be read do not stop the program: it opens on the
// defaults and carries the reason on screen.
func TestBrokenPreferencesAreReportedOnScreen(t *testing.T) {
	frame := newModel(&fakeServers{}, t.TempDir(), defaultPrefs(), errors.New("the UI preferences are unreadable"))
	frame.width, frame.height = 120, 40

	if !strings.Contains(plain(frame.View().Content), "the UI preferences are unreadable") {
		t.Errorf("the frame does not report the preferences it could not read:\n%s", plain(frame.View().Content))
	}
}

// The frame follows the terminal, and survives one no reasonable layout fits
// in: the status box and the keybar are true everywhere, so a screen too short
// for everything drops the view pane rather than either of them.
func TestFrameFollowsTheTerminal(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})

	resized, _ := frame.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	frame = resized.(model)
	if frame.width != 40 || frame.height != 12 {
		t.Fatalf("the frame is %dx%d, want 40x12", frame.width, frame.height)
	}
	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "under construction") {
		t.Errorf("a 40x12 terminal fits the whole frame, but the view pane is missing:\n%s", drawn)
	}
	for _, line := range strings.Split(drawn, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Errorf("a line is %d cells wide in a 40-cell terminal: %q", lipgloss.Width(line), line)
		}
	}

	tiny, _ := frame.Update(tea.WindowSizeMsg{Width: 12, Height: 6})
	frame = tiny.(model)
	drawn = plain(frame.View().Content)
	if strings.Contains(drawn, "under construction") {
		t.Errorf("a terminal too short for everything kept the view pane:\n%s", drawn)
	}
	if !strings.Contains(drawn, globalScope) {
		t.Errorf("a tiny terminal lost the keybar:\n%s", drawn)
	}
	for _, line := range strings.Split(drawn, "\n") {
		if lipgloss.Width(line) > 12 {
			t.Errorf("a line is %d cells wide in a 12-cell terminal: %q", lipgloss.Width(line), line)
		}
	}
}

// A frame drawn before the terminal has said how wide it is still draws.
func TestFrameDrawsBeforeTheFirstResize(t *testing.T) {
	frame := newModel(&fakeServers{}, t.TempDir(), defaultPrefs(), nil)
	if frame.frameWidth() != defaultWidth {
		t.Errorf("the frame draws at %d cells before the first resize, want %d", frame.frameWidth(), defaultWidth)
	}

	frame.width = 0
	if frame.frameWidth() != defaultWidth {
		t.Errorf("a terminal reporting no width draws at %d, want %d", frame.frameWidth(), defaultWidth)
	}
	frame.width = 3
	if frame.frameWidth() != minWidth {
		t.Errorf("a 3-cell terminal draws at %d, want the floor %d", frame.frameWidth(), minWidth)
	}
	// A terminal cria can follow is followed exactly: the frame is never wider
	// than the window it is drawn in.
	frame.width = 31
	if frame.frameWidth() != 31 {
		t.Errorf("a 31-cell terminal draws at %d, want 31", frame.frameWidth())
	}
}
