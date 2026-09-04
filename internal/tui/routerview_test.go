package tui

import (
	"errors"
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

// routerTree is the tree the router's view is read over: the entries every list
// case needs, plus the engine file that gives the router a port of its own
// (docs/specs/SERVE.md).
func routerTree() *config.Tree {
	tree := choicesTree()
	// A third llama entry, so the list has a profile the store does not hold:
	// what the router serves is read against what it could.
	tree.Entries = append(tree.Entries, config.Entry{
		ID: "smol", Path: tree.Root + "/models/smol.toml", Backend: config.BackendLlama,
		Repo: "unsloth/SmolLM2-135M-Instruct-GGUF", Quant: "Q4_K_M", Port: 8082, Host: "0.0.0.0", Name: "smol",
	})
	tree.Router = config.RouterConfig{
		Path: tree.Root + "/engines/router.toml",
		Port: 11437,
		Host: "0.0.0.0",
		Args: []string{"-ngl", "99"},
	}
	return tree
}

// routerFrame is the frame standing in the router's view, over a host running
// one: the toggle walked to the router's position, the tree read, and the models
// composed.
func routerFrame(t *testing.T, fake *fakeServers) (model, *testHost, string) {
	t.Helper()
	frame, world, root := testFrameOn(t, newTestHost(fake))
	world.tree = routerTree()
	if fake.storeBacked {
		// The fixture composes from the store this frame writes, so a toggle's
		// answer is what the next composition reads (fakeServers.RouterModels).
		fake.routerStore = serve.RouterStateDir(root)
	}
	frame = load(t, frame)
	frame.prefs.Backend = config.BackendRouter
	frame = frame.observed(frame.refresh().(snapshotMsg))
	return frame.reselect(0), world, root
}

// routerRowsOf is the list as it is drawn, with the escapes taken out — what
// every row case below reads.
func routerRowsOf(frame model) string {
	return plain(strings.Join(frame.routerLines(listWidth, 10), "\n"))
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

// The list is every profile the router could serve, with a mark saying which of
// them it holds: a router holding nothing is a list of unmarked profiles, not an
// empty screen (docs/specs/TUI.md, amended 2026-09-05).
func TestTheRouterListShowsEveryProfileWithItsInclusionMark(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{routerModels: heldModels()})

	lines := routerRowsOf(frame)
	for _, want := range []string{
		includedMark + "  gemma", // the store holds it
		includedMark + "  qwen",
		excludedMark + "  smol", // and this one it does not
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("the list reads\n%s\nwant it to carry %q", lines, want)
		}
	}
	// mlx entries are served by another program and can never be one of a
	// llama-server router's models, so the list offers no mark for one.
	if strings.Contains(lines, "mlx-qwen") {
		t.Errorf("the list reads\n%s\nwant no mlx entry on it", lines)
	}

	// With nothing included at all, every profile is still on the list.
	empty, _, _ := routerFrame(t, &fakeServers{})
	lines = routerRowsOf(empty)
	for _, want := range []string{excludedMark + "  gemma", excludedMark + "  qwen"} {
		if !strings.Contains(lines, want) {
			t.Errorf("the list of an empty router reads\n%s\nwant it to carry %q", lines, want)
		}
	}
	if strings.Contains(lines, includedMark) {
		t.Errorf("the list of an empty router reads\n%s\nwant nothing marked as held", lines)
	}
}

// A store cria could not read leaves the marks unknown rather than claiming
// every profile is out of the router (CODING-RULES §4).
func TestAnUnreadableStoreLeavesTheMarksUnknown(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{routerModelErr: errors.New("models.json is unreadable")})

	lines := routerRowsOf(frame)
	if !strings.Contains(lines, unknownMark+"  gemma") {
		t.Errorf("the list reads\n%s\nwant the mark cria has not earned an answer for", lines)
	}
	bar := plain(renderKeybar(200, frame.groups()...))
	if strings.Contains(bar, "␣ include") || strings.Contains(bar, "␣ exclude") {
		t.Errorf("the bar reads %q, want no checkbox over a store cria could not read", bar)
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
	// gemma is included and could not be carried: a profile row that reads
	// `skipped` rather than a footnote (docs/specs/TUI.md).
	models := heldModels()
	models.Served = models.Served[:1] // qwen alone
	models.Skipped = append(models.Skipped, engine.Skipped{
		ID:     "gemma",
		Reason: "--ctx-size is written more than once, and a preset key holds one value; write it once",
	})

	frame, _, _ := routerFrame(t, &fakeServers{routerModels: models})
	frame = frame.reselect(0) // gemma, qwen, smol, typo

	if lines := routerRowsOf(frame); !strings.Contains(lines, "gemma  "+routerNoSection) {
		t.Errorf("the list reads\n%s\nwant gemma to read as skipped", lines)
	}

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

// space is the checkbox: it holds the profile under the cursor, at its config
// defaults, and the write lands at the keypress — leaving the view is never a
// discard (docs/specs/TUI.md, amended 2026-09-05).
func TestSpaceIncludesTheProfileUnderTheCursor(t *testing.T) {
	frame, _, root := routerFrame(t, &fakeServers{storeBacked: true})
	dir := serve.RouterStateDir(root)

	frame = frame.reselect(1) // gemma, qwen, smol, typo
	frame, _ = press(t, frame, space)

	held, err := picks.LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's store back: %v", err)
	}
	if !held.Holds("qwen") {
		t.Fatalf("the router holds %v, want qwen included by the keypress", held.Models)
	}
	if picked := held.Picks("qwen"); len(picked) != 0 {
		t.Errorf("qwen was included under %v, want the entry's config defaults — an empty selection", picked)
	}

	// The mark moves at the keypress rather than at the next tick.
	if lines := routerRowsOf(frame); !strings.Contains(lines, includedMark+"  qwen") {
		t.Errorf("the list reads\n%s\nwant qwen marked as held right away", lines)
	}
	// And the second press takes it back out, dropping what it was held under.
	frame, _ = press(t, frame, space)
	held, err = picks.LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's store back: %v", err)
	}
	if held.Holds("qwen") {
		t.Errorf("the router still holds %v after the second press", held.Models)
	}
	if lines := routerRowsOf(frame); !strings.Contains(lines, excludedMark+"  qwen") {
		t.Errorf("the list reads\n%s\nwant qwen unmarked again", lines)
	}
}

// Excluding drops the picks the router held the entry under: that is what
// exclude means, and it is what the CLI verb does with the same call
// (docs/specs/CLI.md).
func TestExcludingDropsTheRoutersPicksForTheEntry(t *testing.T) {
	frame, _, root := routerFrame(t, &fakeServers{storeBacked: true})
	dir := serve.RouterStateDir(root)
	if err := picks.SaveRouter(dir, picks.Router{Models: map[string]config.Selection{
		"qwen": {"quant": "q6", "layout": "chat"},
	}}); err != nil {
		t.Fatalf("writing the router's store: %v", err)
	}
	frame = frame.observed(frame.refresh().(snapshotMsg))

	frame = frame.reselect(1) // qwen
	frame, _ = press(t, frame, space)

	held, err := picks.LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's store back: %v", err)
	}
	if held.Holds("qwen") {
		t.Errorf("the router still holds qwen under %v", held.Picks("qwen"))
	}

	// Including it again starts from the config defaults, not from what it was
	// held under before.
	frame, _ = press(t, frame, space)
	held, err = picks.LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's store back: %v", err)
	}
	if picked := held.Picks("qwen"); len(picked) != 0 {
		t.Errorf("qwen came back held under %v, want the config defaults", picked)
	}
}

// A toggle moves the mark and nothing else: the columns beside it stand still,
// because a row that reflows under a keypress is a row nobody can read
// (docs/specs/TUI.md).
func TestTheInclusionMarkDoesNotReflowTheRow(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{storeBacked: true})
	frame = frame.reselect(1)

	before := frame.routerLines(listWidth, 10)
	frame, _ = press(t, frame, space)
	after := frame.routerLines(listWidth, 10)

	if len(before) != len(after) {
		t.Fatalf("the list went from %d lines to %d under one toggle", len(before), len(after))
	}
	reference := "unsloth/Qwen3-30B-A3B-GGUF"
	for i := range before {
		was, now := plain(before[i]), plain(after[i])
		if len(was) != len(now) {
			t.Errorf("row %d changed width under the toggle:\n%q\n%q", i, was, now)
		}
		// The id and the reference sit in the same cells before and after: the
		// mark and the state word are the only things a toggle moves.
		if strings.Index(was, "qwen") != strings.Index(now, "qwen") ||
			strings.Index(was, reference) != strings.Index(now, reference) {
			t.Errorf("row %d moved under the toggle:\n%q\n%q", i, was, now)
		}
	}
}

// An inclusion whose profile the tree no longer declares stays on the list: it
// is never auto-pruned (docs/specs/SERVE.md), and the row is how it is seen and
// taken back out.
func TestAHeldIdWithNoProfileIsStillARow(t *testing.T) {
	frame, _, root := routerFrame(t, &fakeServers{storeBacked: true})
	dir := serve.RouterStateDir(root)
	if err := picks.SaveRouter(dir, picks.Router{Models: map[string]config.Selection{
		"renamed": {},
	}}); err != nil {
		t.Fatalf("writing the router's store: %v", err)
	}
	frame = frame.observed(frame.refresh().(snapshotMsg))

	lines := routerRowsOf(frame)
	if !strings.Contains(lines, includedMark+"  renamed") || !strings.Contains(lines, "no llama entry named renamed") {
		t.Errorf("the list reads\n%s\nwant the held id with no profile, and why it has none", lines)
	}

	// It trails the profiles, and the only thing it can do is come out.
	rows := frame.routerRows()
	ghost := rows[len(rows)-1]
	if ghost.id != "renamed" || !ghost.included || ghost.includable() {
		t.Fatalf("the last row is %+v, want the held id nothing declares", ghost)
	}
	frame = frame.reselect(len(rows) - 1)
	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "␣ exclude") || strings.Contains(bar, "␣ include") {
		t.Errorf("the bar reads %q on a held id with no profile, want the way out alone", bar)
	}

	frame, _ = press(t, frame, space)
	held, err := picks.LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's store back: %v", err)
	}
	if held.Holds("renamed") {
		t.Errorf("the router still holds %v", held.Models)
	}
	if lines := routerRowsOf(frame); strings.Contains(lines, "renamed") {
		t.Errorf("the list reads\n%s\nwant the row gone with the inclusion", lines)
	}
}

// The running router's word is drawn onto a row it still serves, whatever the
// store now says: the store is the next start, the running router is right now
// (docs/specs/TUI.md).
func TestAnExcludedProfileTheRouterStillServesShowsItsState(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{
		storeBacked:  true,
		routerFound:  true,
		routerRecord: routerRecord(),
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL", Aliases: []string{"qwen"}, State: engine.RouterLoaded, Phase: serve.PhaseRunning},
		}},
	})

	lines := routerRowsOf(frame)
	if !strings.Contains(lines, excludedMark+"  qwen   loaded") {
		t.Errorf("the list reads\n%s\nwant the running router's word on a row the store no longer holds", lines)
	}
	// A profile neither the store nor the running router holds says nothing in
	// that column: the mark is the whole answer.
	if !strings.Contains(lines, excludedMark+"  smol   "+strings.Repeat(" ", len(routerNotRunning))) {
		t.Errorf("the list reads\n%s\nwant an empty state column on a profile nothing serves", lines)
	}
}

// p edits an inclusion. On a profile the router does not hold there is no
// combination to edit — the key is not drawn and does nothing, the answer a flat
// entry gets (docs/specs/TUI.md).
func TestPicksAreOfferedOnlyOnAnIncludedProfile(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{routerModels: heldModels()})

	frame = frame.reselect(2) // smol: a profile the router does not hold
	bar := plain(renderKeybar(200, frame.groups()...))
	if strings.Contains(bar, "p picks") {
		t.Errorf("the bar reads %q on an excluded profile, want no picker there", bar)
	}
	frame, _ = press(t, frame, typed('p'))
	if frame.picker != nil {
		t.Errorf("p opened %+v on a profile the router does not hold", frame.picker)
	}
	if !strings.Contains(bar, "␣ include") {
		t.Errorf("the bar reads %q, want the key that would put it in the router", bar)
	}

	// On an included one it is the picker over the router's own axes.
	frame = frame.reselect(1) // qwen
	bar = plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "p picks") || !strings.Contains(bar, "␣ exclude") {
		t.Errorf("the bar reads %q on an included profile, want its picks and the way out", bar)
	}
}

// The pane of a profile the router does not hold reads the entry through its
// config defaults, which is what including it would hold it under, and says so.
func TestAnExcludedProfilesPaneReadsTheEntryThroughItsDefaults(t *testing.T) {
	frame, _, _ := routerFrame(t, &fakeServers{routerModels: heldModels()})
	frame = frame.reselect(2) // smol

	pane := plain(strings.Join(frame.routerDetail(80, 20), "\n"))
	for _, want := range []string{"not included", "unsloth/SmolLM2-135M-Instruct-GGUF", "smol.toml"} {
		if !strings.Contains(pane, want) {
			t.Errorf("the pane reads\n%s\nwant it to carry %q", pane, want)
		}
	}
	if strings.Contains(pane, "alias =") {
		t.Errorf("the pane reads\n%s\nwant no preset section for a model nothing composed", pane)
	}
}

// The router is started from its own view: the bar teaches the key while there
// is none running, and the start is the one `cria router start` runs.
func TestTheRouterViewStartsTheRouter(t *testing.T) {
	fake := &fakeServers{storeBacked: true}
	frame, _, _ := routerFrame(t, fake)

	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "⏎ start router") {
		t.Errorf("the bar reads %q with no router running, want the key that starts one", bar)
	}

	frame, cmd := press(t, frame, tea.KeyPressMsg{Code: tea.KeyEnter})
	if verb := frame.pending.verb(string(config.BackendRouter)); verb != verbStarting {
		t.Errorf("the box says %q while the start runs, want %q", verb, verbStarting)
	}
	msg, ok := run(t, cmd).(routerStartedMsg)
	if !ok {
		t.Fatalf("⏎ answered with a %T, want the router's start", run(t, cmd))
	}
	if msg.err != nil {
		t.Fatalf("the start refused: %v", msg.err)
	}
	if fake.startedRouter != 1 {
		t.Errorf("cria started the router %d times, want once", fake.startedRouter)
	}
	if fake.asked[len(fake.asked)-1] != 11437 {
		t.Errorf("the start asked about port %v, want the one the engine file names", fake.asked)
	}

	frame, _ = frame.routerStarted(msg)
	if frame.alert.text != "" {
		t.Errorf("the start left %q under the box; the box itself says a router is up", frame.alert.text)
	}

	// With one running, there is nothing to start and the key is gone.
	frame = frame.observed(frame.refresh().(snapshotMsg))
	bar = plain(renderKeybar(200, frame.groups()...))
	if strings.Contains(bar, "⏎ start router") {
		t.Errorf("the bar reads %q with a router running, want no second start", bar)
	}
	if !strings.Contains(bar, "s stop") {
		t.Errorf("the bar reads %q, want the stop that ends it", bar)
	}
}

// A start that cannot happen says why on the line under the box, in the words
// the CLI refuses in — before anything on the host has changed.
func TestTheRouterStartRefusesOnScreen(t *testing.T) {
	t.Run("no port in the engine file", func(t *testing.T) {
		frame, world, _ := routerFrame(t, &fakeServers{})
		world.tree = choicesTree() // no engines/router.toml, so no port
		frame = load(t, frame)

		frame, cmd := press(t, frame, tea.KeyPressMsg{Code: tea.KeyEnter})
		frame, _ = frame.routerStarted(run(t, cmd).(routerStartedMsg))
		if !strings.Contains(frame.alert.text, "no router port") || !frame.alert.bad {
			t.Errorf("the line under the box reads %q, want the missing port", frame.alert.text)
		}
	})

	t.Run("the port is held by something else", func(t *testing.T) {
		fake := &fakeServers{use: serve.PortUse{Holders: []serve.Holder{{PID: 991, Command: "llama-server -hf x"}}}}
		frame, _, _ := routerFrame(t, fake)

		frame, cmd := press(t, frame, tea.KeyPressMsg{Code: tea.KeyEnter})
		frame, _ = frame.routerStarted(run(t, cmd).(routerStartedMsg))
		if fake.startedRouter != 0 {
			t.Error("cria spawned a router onto a port something else holds")
		}
		for _, want := range []string{"11437", "991", "router.toml"} {
			if !strings.Contains(frame.alert.text, want) {
				t.Errorf("the refusal reads %q, want it to carry %q", frame.alert.text, want)
			}
		}
	})

	t.Run("the start itself refused", func(t *testing.T) {
		frame, _, _ := routerFrame(t, &fakeServers{routerStartErr: errors.New("cannot compose the router's preset")})

		frame, cmd := press(t, frame, tea.KeyPressMsg{Code: tea.KeyEnter})
		frame, _ = frame.routerStarted(run(t, cmd).(routerStartedMsg))
		if !strings.Contains(frame.alert.text, "cannot compose the router's preset") {
			t.Errorf("the line under the box reads %q, want what the start refused with", frame.alert.text)
		}
	})
}
