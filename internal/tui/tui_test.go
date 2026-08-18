package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/hubcache"
	"cria/internal/serve"
	"cria/internal/tools"
)

// fakeServers is the lifecycle as the frame drives it: what one refresh
// answers, who holds a port, and what every action did. It is half the seam
// between this package and a live host, so every display state, every
// contextual key and every refusal is exercised with no state directory, no
// process table and no server (docs/specs/TUI.md).
type fakeServers struct {
	listing serve.StatusListing
	err     error

	record     serve.Record
	startErr   error
	use        serve.PortUse
	useErr     error
	stopErr    error
	killErr    error
	dismissErr error
	holderErr  error

	started   []string
	stopped   []string
	killed    []string
	dismissed []string
	holders   []int
	asked     []int // the ports a start asked about, in the order it asked
}

func (f *fakeServers) Snapshots() (serve.StatusListing, error) {
	if f.err != nil {
		return serve.StatusListing{}, f.err
	}
	return f.listing, nil
}

// Running answers off the same listing Snapshots serves: the live server with
// that entry id, if the fixture holds one. A stop or kill removes the record on
// the real manager, so an entry the test has already stopped is not running —
// without that, a restart's second leg would be refused by its own first leg.
func (f *fakeServers) Running(entryID string) (serve.Server, bool, error) {
	for _, status := range f.listing.Servers {
		if status.Phase == serve.PhaseExited || status.EntryID != entryID {
			continue
		}
		if slices.Contains(f.stopped, entryID) || slices.Contains(f.killed, entryID) {
			continue
		}
		return serve.Server{Record: status.Record, Live: true}, true, nil
	}
	return serve.Server{}, false, nil
}

func (f *fakeServers) Start(entry config.Entry, _ tools.Report) (serve.Record, error) {
	f.started = append(f.started, entry.ID)
	if f.startErr != nil {
		return serve.Record{}, f.startErr
	}
	if f.record.EntryID != "" {
		return f.record, nil
	}
	return serve.Record{EntryID: entry.ID, Backend: entry.Backend, Repo: entry.Repo, Quant: entry.Quant,
		Host: entry.Host, Port: entry.Port, PID: 4242, LogPath: "/state/logs/" + entry.ID + ".log"}, nil
}

func (f *fakeServers) Stop(record serve.Record) error {
	f.stopped = append(f.stopped, record.EntryID)
	return f.stopErr
}

func (f *fakeServers) Kill(record serve.Record) error {
	f.killed = append(f.killed, record.EntryID)
	return f.killErr
}

func (f *fakeServers) Dismiss(record serve.Record) error {
	f.dismissed = append(f.dismissed, record.EntryID)
	return f.dismissErr
}

func (f *fakeServers) PortUse(port int) (serve.PortUse, error) {
	f.asked = append(f.asked, port)
	return f.use, f.useErr
}

// KillHolder answers the way a killed process does: the port loses that holder,
// so the re-check the modal runs afterwards sees what the kill changed.
func (f *fakeServers) KillHolder(holder serve.Holder) error {
	f.holders = append(f.holders, holder.PID)
	if f.holderErr != nil {
		return f.holderErr
	}
	var left []serve.Holder
	for _, held := range f.use.Holders {
		if held.PID != holder.PID {
			left = append(left, held)
		}
	}
	f.use.Holders = left
	return nil
}

// testHost is the other half of the seam: the config tree the frame lists, the
// tool check that says what may be launched, the hub cache both lists are drawn
// from, and the surgery the cache view's delete key drives.
type testHost struct {
	servers  *fakeServers
	tree     *config.Tree
	treeErr  error
	report   tools.Report
	cache    *hubcache.Cache
	cacheErr error
	surgery  *fakeSurgery
	checks   int // how many times the tool check was run
}

// newTestHost is a host with nothing declared and every tool in place.
func newTestHost(fake *fakeServers) *testHost {
	return &testHost{
		servers: fake,
		tree:    &config.Tree{Root: "/home/u/.config/cria"},
		report:  usableTools(),
		cache:   &hubcache.Cache{},
		surgery: &fakeSurgery{},
	}
}

func (h *testHost) host() host {
	return host{
		servers: h.servers,
		entries: func() (*config.Tree, error) { return h.tree, h.treeErr },
		tools: func(config.Settings) tools.Report {
			h.checks++
			return h.report
		},
		cache:   func() (*hubcache.Cache, error) { return h.cache, h.cacheErr },
		surgery: h.surgery.surgery(),
	}
}

// usableTools is a host with every managed tool found and fit.
func usableTools() tools.Report {
	return tools.Report{
		LlamaServer: tools.Tool{Name: tools.LlamaServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/llama-server", Build: 7000},
		MLXLMServer: tools.Tool{Name: tools.MLXLMServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/mlx_lm.server"},
		HF:          tools.Tool{Name: tools.HF, Status: tools.StatusFound, Path: "/opt/homebrew/bin/hf"},
	}
}

// testTree is a config tree with one entry per case the list has to draw: two
// llama entries — one on the tree's default port, one on its own — an mlx entry
// under the other tab, and an entry file cria had to refuse.
func testTree() *config.Tree {
	root := "/home/u/.config/cria"
	return &config.Tree{
		Root:     root,
		Settings: config.Settings{DefaultPort: 8080},
		Entries: []config.Entry{
			{
				ID: "gemma", Path: root + "/models/gemma.toml", Backend: config.BackendLlama,
				Repo: "unsloth/gemma-3-27b-it-GGUF", Quant: "Q4_K_M", Port: 8081, Host: "0.0.0.0", Name: "gemma",
			},
			{
				ID: "mlx-qwen", Path: root + "/models/mlx-qwen.toml", Backend: config.BackendMLX,
				Repo: "mlx-community/Qwen3-30B-A3B-4bit", Port: 8080, Host: "127.0.0.1", Name: "mlx qwen",
			},
			{
				ID: "qwen", Path: root + "/models/qwen.toml", Backend: config.BackendLlama,
				Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL", Port: 8080, Host: "0.0.0.0",
				Name: "qwen 30b", Args: []string{"--ctx-size", "16384", "--jinja"},
			},
		},
		Broken: []config.BrokenEntry{{
			ID:   "typo",
			Path: root + "/models/typo.toml",
			Err:  &config.KeyError{Key: "prot", Reason: "unknown key"},
		}},
	}
}

// testFrame is a model over a temporary state root, so a preferences write in a
// test lands nowhere near the host's own.
func testFrame(t *testing.T, fake *fakeServers) (model, string) {
	t.Helper()
	frame, _, root := testFrameOn(t, newTestHost(fake))
	return frame, root
}

// testFrameOn is testFrame over a host a test has arranged.
func testFrameOn(t *testing.T, world *testHost) (model, *testHost, string) {
	t.Helper()
	root := t.TempDir()
	saved, err := loadPrefs(root)
	if err != nil {
		t.Fatalf("loading the preferences of a fresh state root: %v", err)
	}
	frame := newModel(world.host(), root, saved, nil)
	frame.width, frame.height = 120, 40
	return frame, world, root
}

// load is one read of the config tree through the frame, the way a tick takes
// it: the real command, then the real handler.
func load(t *testing.T, frame model) model {
	t.Helper()
	msg, ok := frame.readEntries().(entriesMsg)
	if !ok {
		t.Fatal("reading the config tree did not answer with the tree")
	}
	return frame.loaded(msg)
}

// run drives one command to the message it answers with.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the key returned no command")
	}
	return cmd()
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
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	frame = load(t, frame)
	drawn := plain(frame.View().Content)

	for _, part := range []string{statusTitle, "no server has been started yet", "serve · llama",
		"qwen", detailTitle, "selection", "⏎ start", "global", "⇥ backend", "q quit"} {
		if !strings.Contains(drawn, part) {
			t.Errorf("the frame does not draw %q:\n%s", part, drawn)
		}
	}
}

// c opens the cache and esc comes back, and the status box stays: it appears in
// every view, the cache view included (docs/specs/TUI.md).
// esc answers what is on screen: a visible alert first, the cache view's way
// back second — and the bar names whichever of the two is next
// (docs/specs/TUI.md).
func TestEscDismissesTheAlertBeforeLeading(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})
	frame = frame.show(viewCache)
	frame.alert = alert{text: "port 8080 is held by a process cria did not start", bad: true}

	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc dismiss") || strings.Contains(bar, "esc back") {
		t.Errorf("the bar reads %q while an alert shows, want esc to dismiss it", bar)
	}

	frame, _ = press(t, frame, escape)
	if frame.alert.text != "" {
		t.Errorf("esc left the alert up: %q", frame.alert.text)
	}
	if frame.view != viewCache {
		t.Error("esc changed the view while it was dismissing the alert")
	}

	bar = plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc back") || strings.Contains(bar, "esc dismiss") {
		t.Errorf("the bar reads %q after the dismissal, want esc to lead back", bar)
	}
	frame, _ = press(t, frame, escape)
	if frame.view != viewServe {
		t.Error("the second esc did not lead back to the serve view")
	}
}

// The notice row is reserved: a frame with nothing to say is exactly as tall as
// one with an alert, so nothing shifts when a notice appears or goes
// (docs/specs/TUI.md).
func TestTheNoticeRowIsReserved(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})
	quiet := lipgloss.Height(frame.frame())

	frame.alert = alert{text: "reclaimed 84.1 MiB from unsloth/SmolLM2-135M-Instruct-GGUF:Q2_K"}
	if talking := lipgloss.Height(frame.frame()); talking != quiet {
		t.Errorf("the frame grew from %d to %d rows when the alert appeared", quiet, talking)
	}
}

func TestViewKeysSwitchScreens(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})

	frame, _ = press(t, frame, typed('c'))
	if frame.view != viewCache {
		t.Fatalf("the cache key left the frame on %v, want the cache view", frame.view)
	}
	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "cache") {
		t.Errorf("the cache view does not name itself:\n%s", drawn)
	}
	if !strings.Contains(drawn, statusTitle) || !strings.Contains(drawn, "no server has been started yet") {
		t.Errorf("the status box is missing from the cache view:\n%s", drawn)
	}

	frame, _ = press(t, frame, escape)
	if frame.view != viewServe {
		t.Errorf("esc left the frame on %v, want it back on the serve view", frame.view)
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

// The toggle says nothing under the status box: the pane title already names the
// backend, in that backend's own colour, and a line reporting the change would
// still be sitting there long after it (docs/specs/TUI.md).
func TestBackendToggleReportsItselfInTheTitle(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	frame = load(t, frame)

	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	if frame.alert.text != "" {
		t.Errorf("the toggle left %q under the status box, want the title to carry the change alone", frame.alert.text)
	}

	// The two backends are told apart by colour, not only by the word.
	title := frame.serveTitle()
	if !strings.Contains(title, backendTone(config.BackendMLX).Render("mlx")) {
		t.Errorf("the title does not spell mlx in the mlx colour: %q", title)
	}
	if plain(title) != "serve · mlx" {
		t.Errorf("the title reads %q, want %q", plain(title), "serve · mlx")
	}
	if backendTone(config.BackendLlama).GetForeground() == backendTone(config.BackendMLX).GetForeground() {
		t.Error("both backends are drawn in one colour; the active one has to be recognisable at a glance")
	}

	// And an alert already on screen goes with the keypress rather than
	// outliving it.
	frame.alert = alert{text: "stopped qwen"}
	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	if frame.alert.text != "" {
		t.Errorf("the toggle kept %q from the keypress before it", frame.alert.text)
	}
}

// Navigation is one key each way, and the bar offers exactly the one that works
// where the user is standing (docs/specs/TUI.md).
func TestNavigationKeysAreScopedToTheView(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})

	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "c cache") {
		t.Errorf("the serve view's keybar reads %q, want the key that opens the cache", bar)
	}
	if strings.Contains(bar, "esc back") || strings.Contains(bar, "v view") {
		t.Errorf("the serve view's keybar offers a way back from where it is: %q", bar)
	}

	frame, _ = press(t, frame, typed('c'))
	bar = plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "esc back") {
		t.Errorf("the cache view's keybar reads %q, want esc to come back", bar)
	}
	if strings.Contains(bar, "c cache") {
		t.Errorf("the cache view offers the key that opens it: %q", bar)
	}

	// The key that is gone is gone: v does nothing in either view.
	frame, _ = press(t, frame, typed('v'))
	if frame.view != viewCache {
		t.Errorf("v moved the frame to %v; the view key is c now", frame.view)
	}
}

// esc closes whatever has the keyboard before it means "back": a pane over the
// cache view is what the key is reached for there.
func TestEscapeClosesOverlaysBeforeLeavingTheCacheView(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})
	frame, _ = press(t, frame, typed('c'))

	frame, cmd := press(t, frame, typed('t'))
	frame = answer(t, frame, cmd)
	if !frame.toolsOpen {
		t.Fatal("the tools pane did not open over the cache view")
	}

	frame, _ = press(t, frame, escape)
	if frame.toolsOpen {
		t.Error("esc left the tools pane up")
	}
	if frame.view != viewCache {
		t.Errorf("esc closed the pane and left the view too: %v", frame.view)
	}

	frame, _ = press(t, frame, escape)
	if frame.view != viewServe {
		t.Errorf("esc with nothing over the cache view left the frame on %v", frame.view)
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
			gone: []string{"s stop", "K kill", "l log", "r restart", "d dismiss"},
		},
		{
			name: "a stopped box still has something to restart",
			last: "qwen",
			want: []string{"r restart"},
			gone: []string{"s stop", "K kill", "l log", "d dismiss"},
		},
		{
			name:    "a running server can be stopped, killed and read",
			listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}},
			want:    []string{"s stop", "K kill", "l log", "r restart"},
			gone:    []string{"d dismiss"},
		},
		{
			name:    "an exited record can be read and dismissed, not stopped",
			listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseExited)}},
			want:    []string{"l log", "r restart", "d dismiss"},
			gone:    []string{"s stop", "K kill"},
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
	if len(batch) != 3 {
		t.Fatalf("the tick returned %d commands, want the observation, the config tree and the next tick", len(batch))
	}

	observed, listed, rearmed := false, false, false
	for _, each := range batch {
		switch msg := each().(type) {
		case snapshotMsg:
			observed = true
			if len(msg.listing.Servers) != 1 {
				t.Errorf("the observation carried %d servers, want the one the host holds", len(msg.listing.Servers))
			}
		case entriesMsg:
			listed = true
		case tickMsg:
			rearmed = true
		}
	}
	if !observed {
		t.Error("the tick did not take an observation")
	}
	if !listed {
		t.Error("the tick did not re-read the config tree")
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
	frame := newModel(newTestHost(&fakeServers{}).host(), t.TempDir(), defaultPrefs(),
		errors.New("the UI preferences are unreadable"))
	frame.width, frame.height = 120, 40

	if !strings.Contains(plain(frame.View().Content), "the UI preferences are unreadable") {
		t.Errorf("the frame does not report the preferences it could not read:\n%s", plain(frame.View().Content))
	}
}

// The frame follows the terminal, and survives one no reasonable layout fits
// in: the status box and the keybar are true everywhere, so a screen too short
// for everything drops the view pane rather than either of them.
func TestFrameFollowsTheTerminal(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	frame = load(t, frame)

	resized, _ := frame.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	frame = resized.(model)
	if frame.width != 40 || frame.height != 12 {
		t.Fatalf("the frame is %dx%d, want 40x12", frame.width, frame.height)
	}
	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "qwen") {
		t.Errorf("a 40x12 terminal fits the whole frame, but the entry list is missing:\n%s", drawn)
	}
	for _, line := range strings.Split(drawn, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Errorf("a line is %d cells wide in a 40-cell terminal: %q", lipgloss.Width(line), line)
		}
	}

	tiny, _ := frame.Update(tea.WindowSizeMsg{Width: 12, Height: 6})
	frame = tiny.(model)
	drawn = plain(frame.View().Content)
	if strings.Contains(drawn, "qwen") {
		t.Errorf("a terminal too short for everything kept the view pane:\n%s", drawn)
	}
	// The bar is the frame's last line, whatever fits on it: a terminal this
	// narrow loses the far end of the bar, never the bar itself.
	lines := strings.Split(strings.TrimRight(drawn, "\n"), "\n")
	if strings.TrimSpace(lines[len(lines)-1]) == "" {
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
	frame := newModel(newTestHost(&fakeServers{}).host(), t.TempDir(), defaultPrefs(), nil)
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
