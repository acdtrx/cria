package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/picks"
	"cria/internal/procs"
	"cria/internal/serve"
)

// routerRecord is what a started router wrote down: an engine's own record,
// naming a preset where an entry's names a model (docs/specs/SERVE.md).
func routerRecord() serve.Record {
	return serve.Record{
		EntryID:    string(config.BackendRouter),
		Backend:    config.BackendRouter,
		Preset:     "/state/engines/router/preset.ini",
		Host:       "0.0.0.0",
		Port:       11437,
		PID:        7788,
		Identity:   procs.Identity{Command: "llama-server --models-preset ...", StartedAt: "Sun Aug 31 09:00:00 2026"},
		Command:    []string{"/opt/homebrew/bin/llama-server", "--models-preset", "/state/engines/router/preset.ini"},
		LogPath:    "/state/engines/router/logs/router-20260831-090000.log",
		LaunchedAt: time.Now().Add(-time.Minute),
	}
}

// heldModels is what the store and the tree compose the router to serve: two
// entries with sections, and one the preset could not carry.
func heldModels() serve.RouterModels {
	return serve.RouterModels{
		Preset: "[*]\nngl = 99\n",
		Served: []serve.ServedModel{
			{
				ID:        "qwen",
				Repo:      "unsloth/Qwen3-30B-A3B-GGUF",
				Quant:     "UD-Q6_K_XL",
				Selection: config.Selection{"quant": "q6", "layout": "chat"},
				Section:   "[unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL]\nalias = qwen\nparallel = 1\n",
			},
			{
				ID:      "gemma",
				Repo:    "ggml-org/gemma-3-4b-it-GGUF",
				Quant:   "Q8_0",
				Section: "[ggml-org/gemma-3-4b-it-GGUF:Q8_0]\nalias = gemma\n",
			},
		},
		Skipped: []engine.Skipped{{
			ID:     "typo",
			Reason: "--ctx-size is written more than once, and a preset key holds one value; write it once",
		}},
	}
}

// routerFrame is the frame standing in the router's view, over a host running
// one: the toggle walked to the router's position, the tree read, and the models
// composed.
func routerFrame(t *testing.T, fake *fakeServers) (model, *testHost, string) {
	t.Helper()
	frame, world, root := testFrameOn(t, newTestHost(fake))
	world.tree = choicesTree()
	frame = load(t, frame)
	frame.prefs.Backend = config.BackendRouter
	frame = frame.observed(frame.refresh().(snapshotMsg))
	return frame.reselect(0), world, root
}

// The toggle reaches the router, and its position is not an entry list: it is
// the models the router holds, each with the router's own word for what it is
// doing (docs/specs/TUI.md).
func TestTheEngineToggleReachesTheRoutersOwnView(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{
		routerFound:  true,
		routerRecord: routerRecord(),
		routerModels: heldModels(),
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL", Aliases: []string{"qwen"}, State: engine.RouterLoaded, Phase: serve.PhaseRunning},
		}},
	}))
	world.tree = choicesTree()
	frame = load(t, frame)

	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	if frame.prefs.Backend != config.BackendRouter {
		t.Fatalf("two presses left the toggle on %q, want the router", frame.prefs.Backend)
	}

	frame = frame.observed(frame.refresh().(snapshotMsg))
	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "serve · router") {
		t.Errorf("the view does not name the engine it is showing:\n%s", drawn)
	}
	for _, want := range []string{"qwen", "loaded", "gemma", routerDetailTitle} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the router's view does not draw %q:\n%s", want, drawn)
		}
	}
	// The entry list's own rows are not what this position shows: mlx-qwen is an
	// entry of the tree and no model of the router's.
	if strings.Contains(drawn, "mlx-qwen") {
		t.Errorf("the router's view is showing entries rather than the models it holds:\n%s", drawn)
	}
}

// Each row carries the router's own word for the model, not a phase cria
// invented: three of the six states it publishes have no phase in cria's
// vocabulary, and a model the preset could not carry has no state at all
// (docs/specs/SERVE.md).
func TestTheRouterListCarriesTheRoutersOwnWordPerModel(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{
		routerFound:  true,
		routerRecord: routerRecord(),
		routerModels: heldModels(),
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL", Aliases: []string{"qwen"}, State: engine.RouterLoaded, Phase: serve.PhaseRunning},
			{Model: "ggml-org/gemma-3-4b-it-GGUF:Q8_0", Aliases: []string{"gemma"}, State: engine.RouterSleeping},
		}},
	})

	lines := plain(strings.Join(frame.routerLines(listWidth, 8), "\n"))
	for _, want := range []string{
		"gemma  sleeping",                       // a state cria's phases cannot say, shown as written
		"qwen   loaded",                         // one they can
		"typo   " + routerNoSection,             // and one the preset could not carry
		"unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL", // with the reference each is served as
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("the model list reads\n%s\nwant it to carry %q", lines, want)
		}
	}
}

// A model included since the router started is not in the running router: the
// preset is composed at every start and never edited, so the list says exactly
// that rather than dropping the row the operator just added.
func TestAModelIncludedSinceTheStartReadsAsNotYetServed(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{
		routerFound:  true,
		routerRecord: routerRecord(),
		routerModels: heldModels(),
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL", Aliases: []string{"qwen"}, State: engine.RouterLoaded},
		}},
	})

	lines := plain(strings.Join(frame.routerLines(listWidth, 8), "\n"))
	if !strings.Contains(lines, "gemma  "+routerNotServed) {
		t.Errorf("the model list reads\n%s\nwant gemma to read as included and not yet served", lines)
	}
}

// With no router on the host the list is still the store's: what the next start
// would serve, with nothing serving it yet.
func TestTheModelListStandsWithoutARouterRunning(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{routerModels: heldModels()})

	lines := plain(strings.Join(frame.routerLines(listWidth, 8), "\n"))
	if !strings.Contains(lines, "qwen   "+routerNotRunning) {
		t.Errorf("the model list reads\n%s\nwant every model to read as not running", lines)
	}
}

// A router holding nothing is a true answer, and it names the verb that changes
// it: inclusion is settled by a command, not in this list (docs/specs/CLI.md).
func TestAnEmptyRouterNamesTheVerbThatFillsIt(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{})

	lines := plain(strings.Join(frame.routerLines(listWidth, 6), "\n"))
	if !strings.Contains(lines, "cria router include") {
		t.Errorf("the empty list reads\n%s\nwant it to name the verb that fills it", lines)
	}
}

// The detail pane is the picking-and-seeing loop in the router's flavour: the
// entry through the picks the *router* holds it under, and the preset section a
// start would write for it.
func TestTheRouterDetailShowsTheRouterPicksAndThePresetSection(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{
		routerFound:  true,
		routerRecord: routerRecord(),
		routerModels: heldModels(),
	})
	// The list is in id order — gemma, qwen, typo — so the cursor moves onto qwen.
	frame = frame.reselect(1)

	pane := plain(strings.Join(frame.routerDetail(80, 20), "\n"))
	for _, want := range []string{
		"UD-Q6_K_XL", // the quant the router's own pick resolves to
		"client",     // the name a client addresses it by
		"preset",     // and the section the next start writes
		"[unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL]",
		"alias = qwen",
		"parallel = 1",
	} {
		if !strings.Contains(pane, want) {
			t.Errorf("the detail pane reads\n%s\nwant it to carry %q", pane, want)
		}
	}
}

// A model the preset could not carry says why, in the words whoever included it
// has to act on. Nothing was composed for it, so there is no section to show.
func TestASkippedModelsPaneCarriesItsReason(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{routerModels: heldModels()})
	frame = frame.reselect(2) // gemma, qwen, typo — sorted

	pane := plain(strings.Join(frame.routerDetail(80, 20), "\n"))
	if !strings.Contains(pane, "written more than once") {
		t.Errorf("the detail pane reads\n%s\nwant the reason the preset left it out", pane)
	}
	if strings.Contains(pane, "alias =") {
		t.Errorf("the detail pane reads\n%s\nwant no preset section for a model that got none", pane)
	}
}

// The router is a server cria started, so the status box shows it wherever the
// user is standing — and the keys that act on the box act on it.
func TestTheRouterIsAServerInTheStatusBox(t *testing.T) {
	fake := &fakeServers{routerFound: true, routerRecord: routerRecord(), routerModels: heldModels()}
	frame, _, _ := testFrameOn(t, newTestHost(fake))
	frame = frame.observed(frame.refresh().(snapshotMsg))

	drawn := plain(frame.frame())
	for _, want := range []string{"router", "pid 7788", ":11437"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the status box does not carry the router's %q:\n%s", want, drawn)
		}
	}

	// One live server, so s acts rather than asking which.
	frame, cmd := press(t, frame, typed('s'))
	if msg := run(t, cmd); msg == nil {
		t.Fatal("s on the router's row did nothing")
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != string(config.BackendRouter) {
		t.Errorf("cria stopped %v, want the router", fake.stopped)
	}
	_ = frame
}

// The router's log is the one every model under it logs through, and it is read
// the way every log here is: raw, off the record's own path
// (docs/cria.md, principle 6).
func TestTheRouterLogIsReachableFromTheBox(t *testing.T) {
	fake := &fakeServers{routerFound: true, routerRecord: routerRecord()}
	frame, _, _ := testFrameOn(t, newTestHost(fake))
	frame = frame.observed(frame.refresh().(snapshotMsg))

	frame, _ = press(t, frame, typed('l'))
	if !frame.log.open || frame.log.path != routerRecord().LogPath {
		t.Errorf("l opened %+v, want the router's own log", frame.log)
	}
}

// Restart and bench mean nothing for the router: a restart replays one entry's
// combination, and a bench measures one model's server (docs/specs/SERVE.md).
func TestRestartAndBenchDoNotAimAtTheRouter(t *testing.T) {
	fake := &fakeServers{routerFound: true, routerRecord: routerRecord()}
	frame, _, _ := testFrameOn(t, newTestHost(fake))
	frame = frame.observed(frame.refresh().(snapshotMsg))

	for _, action := range []pickAction{pickRestart, pickBench} {
		if rows := frame.pickable(action); len(rows) != 0 {
			t.Errorf("%s can be aimed at rows %v, want none — the only server here is the router", action.verb(), rows)
		}
	}
	for _, action := range []pickAction{pickStop, pickKill, pickLog} {
		if rows := frame.pickable(action); len(rows) != 1 {
			t.Errorf("%s can be aimed at rows %v, want the router's own", action.verb(), rows)
		}
	}
}

// The picker in the router's view edits the router's own store: the same box and
// the same keys over the same axes, writing the combination the router holds the
// model under rather than the one a bare start composes with
// (docs/plans/engines/OVERVIEW.md, ruling 2).
func TestThePickerInTheRouterViewWritesTheRoutersOwnStore(t *testing.T) {
	fake := &fakeServers{routerModels: heldModels()}
	frame, _, root := routerFrame(t, fake)

	// The router's store as `cria router include` left it: qwen held on q6.
	dir := serve.RouterStateDir(root)
	held := picks.Router{Models: map[string]config.Selection{"qwen": {"quant": "q6", "layout": "chat"}}}
	if err := picks.SaveRouter(dir, held); err != nil {
		t.Fatalf("writing the router's store: %v", err)
	}

	frame = frame.reselect(1) // qwen
	frame, _ = press(t, frame, typed('p'))
	if frame.picker == nil || !frame.picker.router {
		t.Fatalf("p in the router's view opened %+v, want the picker over the router's own picks", frame.picker)
	}
	frame, _ = press(t, frame, right)

	read, err := picks.LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's store back: %v", err)
	}
	if read.Picks("qwen")["quant"] != "q8" {
		t.Errorf("the router holds qwen under %v, want the pick rolled to q8", read.Picks("qwen"))
	}
	// The entry's own picks are untouched: one entry, two combinations.
	if _, err := os.Stat(filepath.Join(root, "choices.json")); !os.IsNotExist(err) {
		t.Errorf("the router's picker wrote the entry's own picks store too (%v)", err)
	}
}
