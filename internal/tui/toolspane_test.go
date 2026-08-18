package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"cria/internal/serve"
	"cria/internal/tools"
)

// outdatedLlamaServer is a llama.cpp too old to share the Hugging Face hub
// cache: present, and refused as firmly as an absent one (docs/specs/TOOLS.md).
func outdatedLlamaServer() tools.Tool {
	return tools.Tool{
		Name:     tools.LlamaServer,
		Status:   tools.StatusOutdated,
		Path:     "/opt/homebrew/bin/llama-server",
		Build:    7200,
		Disables: "starting llama entries; they stay listed, marked unstartable (build 7200 downloads models into a private ~/.cache/llama.cpp instead of the Hugging Face hub cache)",
		Fix:      "upgrade llama.cpp to build 8498 or newer",
	}
}

// toolsPane is the pane's plain text, as the pane is opened: the key, then the
// check it fires.
func toolsPane(t *testing.T, frame model) (model, string) {
	t.Helper()
	opened, cmd := press(t, frame, typed('t'))
	if !opened.toolsOpen {
		t.Fatal("the tools key did not open the pane")
	}
	return answer(t, opened, cmd), plain(opened.View().Content)
}

// The pane is every managed tool's finding: where it resolved, what cria may do
// with it, and — for llama-server — the build number that decides whether its
// downloads land in the cache cria reads (docs/specs/TOOLS.md).
func TestToolsPaneRendersTheReport(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	frame = load(t, frame)

	frame, _ = toolsPane(t, frame)
	drawn := plain(frame.View().Content)
	for _, fact := range []string{
		"llama-server", "/opt/homebrew/bin/llama-server", "build 7000", "hub cache ok",
		"mlx_lm.server", "/opt/homebrew/bin/mlx_lm.server",
		"hf", "/opt/homebrew/bin/hf",
	} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the tools pane does not carry %q:\n%s", fact, drawn)
		}
	}
	// Nothing is degraded, so nothing is offered as a fix.
	if strings.Contains(drawn, "fix ") {
		t.Errorf("the tools pane offers a fix for a host with nothing wrong:\n%s", drawn)
	}
	// The pane is over the view, and the box above it still says what is running.
	if !strings.Contains(drawn, statusTitle) {
		t.Errorf("the tools pane took the status box with it:\n%s", drawn)
	}
	if strings.Contains(drawn, detailTitle) {
		t.Errorf("the tools pane was drawn beside the view rather than over it:\n%s", drawn)
	}
}

// A tool that is missing or unfit says what it takes away and the one action
// that clears it — absence disables features, it never hides them
// (docs/specs/TOOLS.md).
func TestToolsPaneReportsWhatIsDegraded(t *testing.T) {
	cases := map[string]struct {
		report tools.Report
		want   []string
	}{
		"a host without llama.cpp": {
			report: func() tools.Report {
				report := usableTools()
				report.LlamaServer = missingLlamaServer()
				return report
			}(),
			want: []string{
				missingMark + "  llama-server  not on this host",
				"disables starting llama entries",
				"fix install llama.cpp so llama-server is on PATH",
			},
		},
		"a llama.cpp too old to share the hub cache": {
			report: func() tools.Report {
				report := usableTools()
				report.LlamaServer = outdatedLlamaServer()
				return report
			}(),
			want: []string{
				degradedMark + "  llama-server",
				"build 7200",
				"~/.cache/llama.cpp",
				"fix upgrade llama.cpp to build 8498 or newer",
			},
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
			world.tree, world.report = testTree(), test.report
			frame = load(t, frame)

			frame, _ = toolsPane(t, frame)
			drawn := plain(frame.View().Content)
			for _, fact := range test.want {
				if !strings.Contains(drawn, fact) {
					t.Errorf("the tools pane does not carry %q:\n%s", fact, drawn)
				}
			}
			if strings.Contains(drawn, "hub cache ok") {
				t.Errorf("the tools pane claims a hub cache verdict it did not earn:\n%s", drawn)
			}
		})
	}
}

// The pane opens on a fresh check rather than on what the frame read at start:
// it is a deliberate question about the host right now, and it is the only
// thing that runs the exec off the refresh tick.
func TestToolsPaneChecksTheHostWhenItOpens(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	frame = load(t, frame)
	if world.checks != 1 {
		t.Fatalf("the first read of the tree ran the check %d times, want once", world.checks)
	}

	world.report.LlamaServer = outdatedLlamaServer()
	frame, _ = toolsPane(t, frame)
	if world.checks != 2 {
		t.Errorf("opening the pane ran the check %d times in all, want a fresh one", world.checks)
	}
	if !strings.Contains(plain(frame.View().Content), "build 7200") {
		t.Errorf("the pane shows the report the frame started with, not the one it just took:\n%s", plain(frame.View().Content))
	}

	// And it is not on a timer: a tick with the pane up asks the host nothing.
	for _, work := range frame.tickWork() {
		if _, checked := work().(toolsMsg); checked {
			t.Error("a refresh tick ran the tool check")
		}
	}
}

// The pane holds the keyboard while it is up, and either of its keys puts it
// away.
func TestToolsPaneHoldsTheKeyboard(t *testing.T) {
	for name, closing := range map[string]tea.KeyPressMsg{"esc": escape, "t": typed('t')} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeServers{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}}
			frame, world, _ := testFrameOn(t, newTestHost(fake))
			world.tree = testTree()
			frame = load(t, frame).observed(snapshotMsg{listing: fake.listing})
			frame, _ = toolsPane(t, frame)

			bar := plain(renderKeybar(200, frame.groups()...))
			if !strings.Contains(bar, toolsScope+" esc close") {
				t.Errorf("the keybar reads %q, want the pane's own key while it is up", bar)
			}
			for _, hidden := range []string{"s stop", "⏎ start", "v view"} {
				if strings.Contains(bar, hidden) {
					t.Errorf("the keybar offers %q from under the tools pane: %q", hidden, bar)
				}
			}

			stopped, cmd := press(t, frame, typed('s'))
			if cmd != nil || len(fake.stopped) != 0 {
				t.Errorf("a stop fired from under the tools pane: %v", fake.stopped)
			}

			closed, _ := press(t, stopped, closing)
			if closed.toolsOpen {
				t.Error("the pane stayed up")
			}
		})
	}
}
