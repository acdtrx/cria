package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"cria/internal/serve"
	"cria/internal/tools"
)

// missingLlamaServer is the tool check's verdict on a host without llama.cpp
// (docs/specs/TOOLS.md).
func missingLlamaServer() tools.Tool {
	return tools.Tool{
		Name:     tools.LlamaServer,
		Status:   tools.StatusMissing,
		Disables: "starting llama entries; they stay listed, marked unstartable",
		Fix:      "install llama.cpp so llama-server is on PATH, or set tools.llama_server in config.toml",
	}
}

// errUnreadableCache is a hub cache no walk could read.
var errUnreadableCache = errors.New("cannot read the hub cache at /home/u/.cache/huggingface/hub")

// enter is ⏎, and escape is esc, as the terminal reports them.
var (
	enter  = tea.KeyPressMsg{Code: tea.KeyEnter}
	escape = tea.KeyPressMsg{Code: tea.KeyEsc}
)

// startFrame is a frame with the test tree loaded and the cursor on qwen — the
// entry every start case below launches.
func startFrame(t *testing.T, fake *fakeServers) (model, *testHost, string) {
	t.Helper()
	frame, world, root := testFrameOn(t, newTestHost(fake))
	world.tree, world.cache = testTree(), cachedQwen()
	return load(t, frame).reselect(1), world, root
}

// answer runs one action's command and takes what it answered back into the
// frame — a keypress all the way through, the way the program runs it.
func answer(t *testing.T, frame model, cmd tea.Cmd) model {
	t.Helper()
	next, _ := frame.Update(run(t, cmd))
	acted, ok := next.(model)
	if !ok {
		t.Fatalf("the action returned a %T, want the frame's own model", next)
	}
	return acted
}

// ⏎ starts the highlighted entry, and the entry it started becomes what the
// status box falls back to and what restart-last acts on — across sessions,
// which is why it is written down (docs/specs/TUI.md).
func TestStartLaunchesTheSelectedEntry(t *testing.T) {
	fake := &fakeServers{}
	frame, _, root := startFrame(t, fake)

	frame, cmd := press(t, frame, enter)
	// Nothing is said while the start runs: what it will have changed is what
	// the status box says when it lands, and the line is for what the boxes
	// cannot show (tui.go).
	if frame.alert.text != "" {
		t.Errorf("the frame says %q while starting, want the line empty", frame.alert.text)
	}
	frame = answer(t, frame, cmd)

	if len(fake.started) != 1 || fake.started[0] != "qwen" {
		t.Fatalf("the start launched %q, want qwen once", fake.started)
	}
	if len(fake.asked) != 1 || fake.asked[0] != 8080 {
		t.Errorf("the start asked about ports %v, want the entry's port before anything was spawned", fake.asked)
	}
	// A landed start says nothing: the status box shows the server on the next
	// tick, and the alert line carries only what the boxes cannot (tui.go).
	if frame.alert.text != "" {
		t.Errorf("the frame says %q after a successful start, want the line empty", frame.alert.text)
	}

	saved, err := loadPrefs(root)
	if err != nil {
		t.Fatalf("reading the preferences back: %v", err)
	}
	if saved.LastStarted != "qwen" {
		t.Errorf("the next launch would restart %q, want qwen", saved.LastStarted)
	}
}

// ⏎ on the entry that is already running answers from the records alone: no
// tool check, no port question, nothing spawned (docs/specs/SERVE.md — the
// already-running refusal comes first). The tool check execs `llama-server
// --version`, which under load once took long enough to be killed and misread
// as an unverifiable build; this path must never reach it.
func TestStartOfARunningEntryConsultsNothing(t *testing.T) {
	fake := &fakeServers{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}}
	frame, world, _ := startFrame(t, fake)
	frame = frame.observed(snapshotMsg{listing: fake.listing})
	checked := world.checks

	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)

	if !frame.alert.bad || !strings.Contains(frame.alert.text, "already running") {
		t.Errorf("the frame says %q, want the already-running refusal", frame.alert.text)
	}
	if world.checks != checked {
		t.Errorf("the tool check ran %d more time(s) for an entry that is already up", world.checks-checked)
	}
	if len(fake.asked) != 0 || len(fake.started) != 0 {
		t.Errorf("asked ports %v and started %v, want neither touched", fake.asked, fake.started)
	}
}

// The tool gate comes before the port check: a host without llama-server has to
// hear about llama-server, not about a busy port (docs/specs/SERVE.md).
func TestStartRefusedByAnUnusableTool(t *testing.T) {
	fake := &fakeServers{}
	frame, world, _ := startFrame(t, fake)
	world.report.LlamaServer = missingLlamaServer()

	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)

	if !frame.alert.bad || !strings.Contains(frame.alert.text, "llama-server is missing") {
		t.Errorf("the refusal reads %q, want the tool it is missing", frame.alert.text)
	}
	if len(fake.asked) != 0 || len(fake.started) != 0 {
		t.Errorf("a start with no tool asked about the port %v and spawned %v", fake.asked, fake.started)
	}
	if frame.modal != nil {
		t.Error("a missing tool put a modal up; the refusal has nothing to answer")
	}
}

// A port held by a server cria started needs no modal: the fix is one keypress
// on something already on screen (docs/specs/SERVE.md).
func TestStartRefusedByAManagedServer(t *testing.T) {
	held := liveStatus(serve.PhaseRunning)
	held.EntryID, held.Port = "gemma", 8080
	fake := &fakeServers{use: serve.PortUse{Managed: &serve.Server{Record: held.Record, Live: true}}}

	frame, _, _ := startFrame(t, fake)
	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)

	if frame.modal != nil {
		t.Fatal("a port serving one of cria's own servers put a modal up")
	}
	for _, fact := range []string{"port 8080", "gemma", "stop gemma first"} {
		if !strings.Contains(frame.alert.text, fact) {
			t.Errorf("the refusal reads %q, want it to carry %q", frame.alert.text, fact)
		}
	}
	if len(fake.started) != 0 {
		t.Errorf("the refused start spawned %v", fake.started)
	}
}

// A port held by a process cria did not start is the modal docs/specs/SERVE.md
// describes: pid, command line and working directory, with the kill cria offers
// here and nowhere else.
func TestForeignHolderRaisesTheModalAndItsKill(t *testing.T) {
	fake := &fakeServers{use: serve.PortUse{Holders: []serve.Holder{{
		PID:        9111,
		Command:    "/opt/homebrew/bin/llama-server -m /models/other.gguf --port 8080",
		WorkingDir: "/Users/u/work",
	}}}}
	frame, _, _ := startFrame(t, fake)

	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)
	if frame.modal == nil {
		t.Fatal("a foreign holder raised no modal")
	}

	drawn := plain(frame.View().Content)
	for _, fact := range []string{"port 8080 is held", "9111", "/opt/homebrew/bin/llama-server", "/Users/u/work", "k kills it"} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the modal does not carry %q:\n%s", fact, drawn)
		}
	}
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "k kill it") || strings.Contains(bar, "⏎ start") {
		t.Errorf("the keybar reads %q, want the modal's own keys while it is up", bar)
	}

	// The kill is one keypress, and it does not start anything: the port is
	// asked again, and the user presses ⏎ again deliberately.
	frame, killed := press(t, frame, typed('k'))
	frame = answer(t, frame, killed)

	if len(fake.holders) != 1 || fake.holders[0] != 9111 {
		t.Fatalf("the kill went to %v, want the pid holding the port", fake.holders)
	}
	if frame.modal != nil {
		t.Errorf("the modal stayed up after the port came free: %+v", frame.modal)
	}
	if len(fake.started) != 0 {
		t.Errorf("the kill started %v by itself; ⏎ is what starts an entry", fake.started)
	}
	if !strings.Contains(frame.alert.text, "port 8080 is free") {
		t.Errorf("the frame says %q, want the port it just freed", frame.alert.text)
	}
}

// A port something still holds after the kill keeps the modal up, with whoever
// holds it now.
func TestModalStaysUpWhileThePortIsStillHeld(t *testing.T) {
	fake := &fakeServers{use: serve.PortUse{Holders: []serve.Holder{{PID: 9111, Command: "llama-server"}}}}
	fake.holderErr = errors.New("sending SIGKILL to pid 9111: operation not permitted")

	frame, _, _ := startFrame(t, fake)
	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)
	frame, killed := press(t, frame, typed('k'))
	frame = answer(t, frame, killed)

	if frame.modal == nil {
		t.Fatal("a kill that failed dropped the modal")
	}
	if !frame.alert.bad || !strings.Contains(frame.alert.text, "operation not permitted") {
		t.Errorf("the frame says %q, want the reason the kill failed", frame.alert.text)
	}
}

// esc leaves the holder alone, and nothing underneath the modal acts while it is
// up: a start refused for a busy port must not become a stop of something else.
func TestModalHoldsTheKeyboard(t *testing.T) {
	// The live server is another entry: a start of the selected one has to get
	// past its own already-running check to reach the port and raise the modal.
	other := liveStatus(serve.PhaseRunning)
	other.EntryID = "gemma"
	fake := &fakeServers{
		listing: serve.StatusListing{Servers: []serve.Status{other}},
		use:     serve.PortUse{Holders: []serve.Holder{{PID: 9111, Command: "llama-server"}}},
	}
	frame, _, _ := startFrame(t, fake)
	frame = frame.observed(snapshotMsg{listing: fake.listing})

	frame, cmd := press(t, frame, enter)
	frame = answer(t, frame, cmd)

	frame, ignored := press(t, frame, typed('s'))
	if ignored != nil {
		t.Error("a stop fired from under the modal")
	}
	if len(fake.stopped) != 0 {
		t.Errorf("the modal let a stop through to %v", fake.stopped)
	}

	frame, _ = press(t, frame, escape)
	if frame.modal != nil {
		t.Error("esc left the modal up")
	}
}

// An entry file cria refused is listed and readable, and says what to fix when
// the start key is pressed on it (docs/specs/CONFIG.md).
func TestRefusedEntryCannotStart(t *testing.T) {
	fake := &fakeServers{}
	frame, _, _ := startFrame(t, fake)
	frame = frame.reselect(2)

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Fatal("a refused entry file was handed to a start")
	}

	// The key is not offered for that row, so pressing it does nothing — and
	// what the row needs is beside it, in the detail pane.
	detail := plain(strings.Join(frame.detailLines(120, 20), "\n"))
	if !strings.Contains(detail, "typo.toml") || !strings.Contains(detail, "cria docs") {
		t.Errorf("the refused entry's detail reads %q, want the file that has to be fixed", detail)
	}
}

// Stop, kill and dismiss act on what the status box shows, whatever the list
// selection is (docs/specs/TUI.md). One server the key can act on is the whole
// answer, so the key acts on it and asks nothing (pick.go).
func TestServerKeysActOnTheStatusBox(t *testing.T) {
	cases := []struct {
		name  string
		phase serve.Phase
		key   tea.KeyPressMsg
		acted func(*fakeServers) []string
	}{
		{
			name:  "stop",
			phase: serve.PhaseRunning,
			key:   typed('s'),
			acted: func(f *fakeServers) []string { return f.stopped },
		},
		{
			name:  "kill",
			phase: serve.PhaseRunning,
			key:   typed('K'),
			acted: func(f *fakeServers) []string { return f.killed },
		},
		{
			name:  "dismiss",
			phase: serve.PhaseExited,
			key:   typed('d'),
			acted: func(f *fakeServers) []string { return f.dismissed },
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(test.phase)}}}
			frame, _, _ := startFrame(t, fake)
			frame = frame.observed(snapshotMsg{listing: fake.listing})

			frame, cmd := press(t, frame, test.key)
			if frame.pick != nil {
				t.Errorf("the key asked which server with only one to act on: %+v", frame.pick)
			}
			// The action says nothing while it runs: the box shows what it did
			// on the next tick (tui.go).
			if frame.alert.text != "" {
				t.Errorf("the frame says %q while acting, want the line empty", frame.alert.text)
			}
			frame = answer(t, frame, cmd)

			if acted := test.acted(fake); len(acted) != 1 || acted[0] != "qwen" {
				t.Fatalf("the key acted on %q, want qwen once", acted)
			}
			// A landed action says nothing — the box shows the result on the
			// next tick; the line is for what the boxes cannot show (tui.go).
			if frame.alert.text != "" {
				t.Errorf("the frame says %q after the action landed, want the line empty", frame.alert.text)
			}
		})
	}
}

// A lifecycle call that failed says so and changes nothing else.
func TestAFailedStopIsReported(t *testing.T) {
	fake := &fakeServers{
		listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}},
		stopErr: errors.New("qwen did not exit: pid 4242 is still running 2s after SIGKILL"),
	}
	frame, _, _ := startFrame(t, fake)
	frame = frame.observed(snapshotMsg{listing: fake.listing})

	frame, cmd := press(t, frame, typed('s'))
	frame = answer(t, frame, cmd)

	if !frame.alert.bad || !strings.Contains(frame.alert.text, "still running") {
		t.Errorf("the frame says %q, want the reason the stop failed", frame.alert.text)
	}
}

// Restart is stop-then-start of what the box shows — the one-keypress swap-back.
func TestRestartStopsThenStarts(t *testing.T) {
	fake := &fakeServers{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}}
	frame, _, _ := startFrame(t, fake)
	frame = frame.observed(snapshotMsg{listing: fake.listing})

	frame, cmd := press(t, frame, typed('r'))
	if frame.alert.text != "" {
		t.Errorf("the frame says %q while restarting, want the line empty", frame.alert.text)
	}
	frame = answer(t, frame, cmd)

	if len(fake.stopped) != 1 || fake.stopped[0] != "qwen" {
		t.Errorf("the restart stopped %q, want qwen first", fake.stopped)
	}
	if len(fake.started) != 1 || fake.started[0] != "qwen" {
		t.Errorf("the restart started %q, want qwen after the stop", fake.started)
	}
	// The landed restart says nothing; the box shows the fresh server (tui.go).
	if frame.alert.text != "" {
		t.Errorf("the frame says %q after the restart landed, want the line empty", frame.alert.text)
	}
}

// Restart works from the stopped state too: the box names the last-started entry
// there, and that is what the key acts on (docs/specs/TUI.md).
func TestRestartFromStopped(t *testing.T) {
	fake := &fakeServers{}
	frame, _, _ := startFrame(t, fake)
	frame.prefs.LastStarted = "qwen"
	frame.keys.retarget(targetOf(frame.listing, frame.prefs))

	frame, cmd := press(t, frame, typed('r'))
	frame = answer(t, frame, cmd)

	if len(fake.stopped) != 0 {
		t.Errorf("a restart with nothing running stopped %v", fake.stopped)
	}
	if len(fake.started) != 1 || fake.started[0] != "qwen" {
		t.Errorf("the restart started %q, want the last-started entry", fake.started)
	}
}

// An entry the tree no longer declares cannot be restarted, and says so rather
// than failing silently: a record is self-contained, but a start needs the file
// (docs/specs/SERVE.md).
func TestRestartOfAVanishedEntrySaysSo(t *testing.T) {
	fake := &fakeServers{}
	frame, _, _ := startFrame(t, fake)
	frame.prefs.LastStarted = "gone"
	frame.keys.retarget(targetOf(frame.listing, frame.prefs))

	frame, cmd := press(t, frame, typed('r'))
	if cmd != nil {
		t.Fatal("a restart of an entry cria cannot read was handed to a start")
	}
	if !frame.alert.bad || !strings.Contains(frame.alert.text, "gone") {
		t.Errorf("the frame says %q, want the entry it cannot restart", frame.alert.text)
	}
}
