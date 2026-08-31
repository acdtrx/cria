package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/picks"
	"cria/internal/procs"
	"cria/internal/serve"
	"cria/internal/tools"
)

// routerTree is a tree whose engines/router.toml declares a router: a port of its
// own, beside the entries' 8080.
func routerTree() *config.Tree {
	tree := testTree()
	tree.Router = config.RouterConfig{
		Path:       "/home/u/.config/cria/engines/router.toml",
		Port:       11434,
		Host:       "0.0.0.0",
		Args:       []string{"-ngl", "99"},
		RouterArgs: []string{"--models-max", "2"},
	}
	return tree
}

// routerRecord is what a started router wrote down.
func routerRecord() serve.Record {
	return serve.Record{
		EntryID:    string(config.BackendRouter),
		Backend:    config.BackendRouter,
		Preset:     "/home/u/.local/state/cria/engines/router/preset.ini",
		Host:       "0.0.0.0",
		Port:       11434,
		PID:        4242,
		Identity:   procs.Identity{Command: "llama-server --models-preset ...", StartedAt: "Tue Aug 18 14:57:30 2026"},
		Command:    []string{"/opt/homebrew/bin/llama-server", "--models-preset", "/home/u/.local/state/cria/engines/router/preset.ini", "--host", "0.0.0.0", "--port", "11434", "--models-max", "2"},
		LogPath:    "/home/u/.local/state/cria/engines/router/logs/router-20260831-090000.log",
		LaunchedAt: time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC),
	}
}

// `cria router start` starts the router the engine file declares and reports what
// it started: the pid, the address, the preset it serves from, and the command
// line that is now running.
func TestRouterStartReportsWhatItStarted(t *testing.T) {
	fake := &fakeServers{routerRecord: routerRecord()}
	app, out, _ := newTestApp(routerTree(), fake)

	if code := app.run([]string{"router", "start"}, "test"); code != exitOK {
		t.Fatalf("`cria router start` exited %d, want %d", code, exitOK)
	}
	if len(fake.routerStarts) != 1 {
		t.Fatalf("cria started the router %d times, want once", len(fake.routerStarts))
	}
	if started := fake.routerStarts[0]; started.Port != 11434 || strings.Join(started.Args, " ") != "-ngl 99" {
		t.Errorf("the router was started from %+v, want the engine file the tree loaded", started)
	}
	if len(fake.asked) != 1 || fake.asked[0] != 11434 {
		t.Errorf("cria asked about ports %v, want the router's own port before it started anything", fake.asked)
	}

	for _, want := range []string{
		"started the router as pid 4242 on 0.0.0.0:11434",
		"preset /home/u/.local/state/cria/engines/router/preset.ini",
		"--models-preset",
		"log /home/u/.local/state/cria/engines/router/logs/router-20260831-090000.log",
		"cria router status",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the start printed\n%s\nwant it to name %q", out, want)
		}
	}
}

// A tree that declares no router port has no router to start, and the refusal
// names the file that would give it one.
func TestRouterStartRefusesATreeWithNoRouterPort(t *testing.T) {
	tree := testTree()
	tree.Router = config.RouterConfig{Path: "/home/u/.config/cria/engines/router.toml"}
	fake := &fakeServers{}
	app, _, errOut := newTestApp(tree, fake)

	if code := app.run([]string{"router", "start"}, "test"); code != exitFailure {
		t.Fatalf("`cria router start` exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "engines/router.toml") {
		t.Errorf("the refusal reads %q, want it to name the file that sets the port", errOut)
	}
	if len(fake.routerStarts) != 0 {
		t.Errorf("cria started the router %v after refusing", fake.routerStarts)
	}
}

// One router per host: a start while it is running is refused with the pid and
// address that hold it, before the tool or the port is asked about.
func TestRouterStartRefusesASecondRouter(t *testing.T) {
	fake := &fakeServers{routerFound: true, routerLive: true, routerRecord: routerRecord()}
	app, _, errOut := newTestApp(routerTree(), fake)

	if code := app.run([]string{"router", "start"}, "test"); code != exitFailure {
		t.Fatalf("`cria router start` exited %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"already running", "4242", "0.0.0.0:11434"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the refusal reads %q, want it to name %q", errOut, want)
		}
	}
	if len(fake.asked) != 0 {
		t.Errorf("cria asked about port %v while refusing a router that is already up", fake.asked)
	}
	if len(fake.routerStarts) != 0 {
		t.Errorf("cria started the router %v after refusing", fake.routerStarts)
	}
}

// The router's port is checked like any other, and both refusals are the
// entries' own: a managed server is stopped by name, a foreign one is reported
// and left alone (docs/specs/SERVE.md).
func TestRouterStartRefusesABusyPort(t *testing.T) {
	tests := []struct {
		name string
		use  serve.PortUse
		want []string
	}{
		{
			name: "a server cria started",
			use:  serve.PortUse{Managed: &serve.Server{Record: serve.Record{EntryID: "qwen", PID: 999, Port: 11434}, Live: true}},
			want: []string{"port 11434 is already serving qwen", "pid 999", "stop qwen first"},
		},
		{
			name: "a process cria did not start",
			use:  serve.PortUse{Holders: []serve.Holder{{PID: 777, Command: "llama-server --port 11434", WorkingDir: "/home/u"}}},
			want: []string{"cria did not start", "pid 777", "llama-server --port 11434", "/home/u", "engines/router.toml"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{use: test.use}
			app, _, errOut := newTestApp(routerTree(), fake)

			if code := app.run([]string{"router", "start"}, "test"); code != exitFailure {
				t.Fatalf("`cria router start` exited %d, want %d", code, exitFailure)
			}
			for _, want := range test.want {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("the refusal reads %q, want it to name %q", errOut, want)
				}
			}
			if len(fake.routerStarts) != 0 {
				t.Errorf("cria started the router %v onto a busy port", fake.routerStarts)
			}
		})
	}
}

// A llama-server that cannot run as a router refuses the start before the port
// is even asked about, with what the binary lacks (docs/specs/TOOLS.md).
func TestRouterStartRefusesALlamaServerWithoutRouterMode(t *testing.T) {
	fake := &fakeServers{}
	app, _, errOut := newTestApp(routerTree(), fake)
	app.tools = func(config.Settings) tools.Report {
		report := usableReport()
		report.LlamaServer.Router = false
		return report
	}

	if code := app.run([]string{"router", "start"}, "test"); code != exitFailure {
		t.Fatalf("`cria router start` exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), tools.RouterFlag) {
		t.Errorf("the refusal reads %q, want it to name the flag the build lacks", errOut)
	}
	if len(fake.asked) != 0 || len(fake.routerStarts) != 0 {
		t.Errorf("cria asked about ports %v and started %v after a refused tool", fake.asked, fake.routerStarts)
	}
}

// A bare `cria router` reports: the verb that changes nothing is the one you get
// without typing one.
func TestBareRouterReportsItsStatus(t *testing.T) {
	fake := &fakeServers{
		routerFound: true, routerLive: true, routerRecord: routerRecord(),
		routerPhase: serve.PhaseRunning,
		health:      serve.Health{URL: "http://127.0.0.1:11434/health", Green: true, Status: 200, Detail: "200 OK"},
	}
	app, out, _ := newTestApp(routerTree(), fake)

	if code := app.run([]string{"router"}, "test"); code != exitOK {
		t.Fatalf("`cria router` exited %d, want %d", code, exitOK)
	}
	for _, want := range []string{"router  running  router", "pid 4242 on 0.0.0.0:11434", "health http://127.0.0.1:11434/health: 200 OK", "preset ", "log "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the status reads\n%s\nwant it to hold %q", out, want)
		}
	}
}

// A host with no router says so and exits non-zero: the question `cria router
// status` answers is whether one is up.
func TestRouterStatusOnAHostWithNoRouter(t *testing.T) {
	app, out, _ := newTestApp(routerTree(), &fakeServers{})

	if code := app.run([]string{"router", "status"}, "test"); code != exitFailure {
		t.Fatalf("`cria router status` exited %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"no router", "cria router start"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report reads %q, want it to hold %q", out, want)
		}
	}
}

// An exited router is a crash report, and the status says so with the log to
// read it from — non-zero, because nothing is serving.
func TestRouterStatusReportsAnExitedRouter(t *testing.T) {
	fake := &fakeServers{
		routerFound: true, routerRecord: routerRecord(),
		routerPhase: serve.PhaseExited,
	}
	app, out, _ := newTestApp(routerTree(), fake)

	if code := app.run([]string{"router", "status"}, "test"); code != exitFailure {
		t.Fatalf("`cria router status` exited %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"exited", "is gone", "log /home/u/.local/state/cria/engines/router/logs/router-20260831-090000.log"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report reads\n%s\nwant it to hold %q", out, want)
		}
	}
}

// A stop reports what it stopped; a stop with nothing recorded says there is
// nothing to stop.
func TestRouterStop(t *testing.T) {
	fake := &fakeServers{routerFound: true, routerLive: true, routerRecord: routerRecord()}
	app, out, _ := newTestApp(routerTree(), fake)

	if code := app.run([]string{"router", "stop"}, "test"); code != exitOK {
		t.Fatalf("`cria router stop` exited %d, want %d", code, exitOK)
	}
	if strings.Join(fake.stopped, ",") != "router" {
		t.Errorf("cria stopped %v, want the router", fake.stopped)
	}
	if !strings.Contains(out.String(), "stopped the router (pid 4242 on 0.0.0.0:11434)") {
		t.Errorf("the stop printed %q", out)
	}

	empty := &fakeServers{}
	app, _, errOut := newTestApp(routerTree(), empty)
	if code := app.run([]string{"router", "stop"}, "test"); code != exitFailure {
		t.Fatalf("stopping a router that was never started exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "no record of a router") {
		t.Errorf("the refusal reads %q", errOut)
	}
	if len(empty.stopped) != 0 {
		t.Errorf("cria stopped %v with no record to act on", empty.stopped)
	}
}

// A verb cria does not have is a command line it cannot route, and it says what
// the verbs are.
func TestRouterRefusesAVerbItDoesNotHave(t *testing.T) {
	for _, args := range [][]string{{"router", "restart"}, {"router", "start", "stop"}, {"router", "--wait"}} {
		fake := &fakeServers{}
		app, _, errOut := newTestApp(routerTree(), fake)

		if code := app.run(args, "test"); code != exitUsage {
			t.Errorf("`cria %s` exited %d, want %d", strings.Join(args, " "), code, exitUsage)
		}
		if !strings.Contains(errOut.String(), routerSynopsis) {
			t.Errorf("the refusal of %v reads %q, want it to name the usage", args, errOut)
		}
		if len(fake.routerStarts) != 0 {
			t.Errorf("cria started the router from %v", args)
		}
	}
}

// routerApp is an app over a router store held in memory: what the verbs read,
// and what they wrote. The store is the one piece of cria's state the CLI edits,
// so a test reads the file that would have been written off this.
func routerApp(tree *config.Tree, fake *fakeServers, held picks.Router) (*app, *bytes.Buffer, *bytes.Buffer, *picks.Router) {
	app, out, errOut := newTestApp(tree, fake)
	stored := &held
	app.routerModels = func() (picks.Router, error) { return *stored, nil }
	app.saveRouterModels = func(written picks.Router) error { *stored = written; return nil }
	return app, out, errOut, stored
}

// `cria router include <id> [choice=option ...]` holds one of the tree's entries
// under the router, in the combination it names — the same `choice=option`
// vocabulary a start takes, stored here because under the router the combination
// is the state.
func TestRouterIncludeHoldsAnEntryUnderThePicksItNames(t *testing.T) {
	tree := routerTree()
	tree.Entries = append(tree.Entries, choicesEntry())
	app, out, _, stored := routerApp(tree, &fakeServers{}, picks.Router{})

	if code := app.run([]string{"router", "include", "qwen-choices", "quant=q6"}, "test"); code != exitOK {
		t.Fatalf("`cria router include` exited %d, want %d", code, exitOK)
	}
	if !stored.Holds("qwen-choices") {
		t.Fatalf("the router holds %v, want the entry that was included", stored.IDs())
	}
	if picked := stored.Picks("qwen-choices"); picked["quant"] != "q6" {
		t.Errorf("qwen-choices is held under %v, want the combination the command named", picked)
	}
	for _, want := range []string{"included qwen-choices in the router", "quant=q6", `address it as "qwen-choices"`, "restart"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the include printed\n%s\nwant it to say %q", out, want)
		}
	}

	// Running it again is how the combination is changed: one verb settles which
	// models the router holds and what each is held as.
	if code := app.run([]string{"router", "include", "qwen-choices", "quant=q4"}, "test"); code != exitOK {
		t.Fatalf("re-including exited %d, want %d", code, exitOK)
	}
	if picked := stored.Picks("qwen-choices"); picked["quant"] != "q4" {
		t.Errorf("qwen-choices is held under %v, want the combination named second", picked)
	}
	if !strings.Contains(out.String(), "qwen-choices was already in the router") {
		t.Errorf("re-including printed\n%s\nwant it to say the entry was already held", out)
	}
}

// Only the entries the router's own program serves can be included, and the
// refusal names why rather than saying "no".
func TestRouterIncludeRefusesAnEntryTheRouterCannotServe(t *testing.T) {
	tree := routerTree()
	mlx := choicesEntry()
	mlx.ID, mlx.Backend, mlx.Choices = "qwen-mlx", config.BackendMLX, nil
	mlx.Repo = "mlx-community/Qwen3-30B-A3B-4bit"
	tree.Entries = append(tree.Entries, mlx)
	app, _, errOut, stored := routerApp(tree, &fakeServers{}, picks.Router{})

	if code := app.run([]string{"router", "include", "qwen-mlx"}, "test"); code != exitFailure {
		t.Fatalf("including an mlx entry exited %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"mlx_lm.server", "llama-server", "llama"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the refusal reads\n%s\nwant it to name %q", errOut, want)
		}
	}
	if len(stored.IDs()) != 0 {
		t.Errorf("the router holds %v after a refused include", stored.IDs())
	}
}

// An id that names no entry is refused with the ids that do exist — the same
// answer a start gives, since the question is the same one.
func TestRouterIncludeRefusesAnUnknownEntry(t *testing.T) {
	app, _, errOut, stored := routerApp(routerTree(), &fakeServers{}, picks.Router{})

	if code := app.run([]string{"router", "include", "nope"}, "test"); code != exitFailure {
		t.Fatalf("including an unknown entry exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "no entry named \"nope\"") || !strings.Contains(errOut.String(), "qwen") {
		t.Errorf("the refusal reads\n%s\nwant it to name the entries that do exist", errOut)
	}
	if len(stored.IDs()) != 0 {
		t.Errorf("the router holds %v after a refused include", stored.IDs())
	}
}

// `cria router exclude <id>` drops one model, and it never reads the tree: an
// entry whose file was renamed away is exactly the one that has to be droppable.
func TestRouterExcludeDropsWhatTheRouterHolds(t *testing.T) {
	held := picks.Router{}
	held.Include("qwen", nil)
	held.Include("renamed-away", nil)
	app, out, errOut, stored := routerApp(routerTree(), &fakeServers{}, held)

	if code := app.run([]string{"router", "exclude", "renamed-away"}, "test"); code != exitOK {
		t.Fatalf("`cria router exclude` exited %d, want %d", code, exitOK)
	}
	if stored.Holds("renamed-away") || !stored.Holds("qwen") {
		t.Errorf("the router holds %v, want only the entry that was not excluded", stored.IDs())
	}
	if !strings.Contains(out.String(), "excluded renamed-away from the router") {
		t.Errorf("the exclude printed\n%s\nwant it to name what it dropped", out)
	}

	// Excluding what the router does not hold is an answer, not a silent success.
	if code := app.run([]string{"router", "exclude", "renamed-away"}, "test"); code != exitFailure {
		t.Fatalf("excluding an entry the router does not hold exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "does not hold") {
		t.Errorf("the refusal reads\n%s\nwant it to say the router does not hold it", errOut)
	}
}

// `cria router models` is what the next start would serve, composed from the
// store and the tree: it needs no running router, and a model that could not be
// composed is listed with its reason rather than costing the listing its code.
func TestRouterModelsListsWhatTheRouterWouldServe(t *testing.T) {
	fake := &fakeServers{routerModels: serve.RouterModels{
		Served: []serve.ServedModel{
			{ID: "qwen", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
			{ID: "qwen-choices", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q6_K_XL", Selection: config.Selection{"quant": "q6"}},
		},
		Skipped: []engine.Skipped{{ID: "qwen-mlx", Reason: "mlx entries are served by mlx_lm.server"}},
	}}
	app, out, _, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "models"}, "test"); code != exitOK {
		t.Fatalf("`cria router models` exited %d, want %d", code, exitOK)
	}
	for _, want := range []string{
		"qwen          unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"qwen-choices  unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL  quant=q6",
		"qwen-mlx  skipped: mlx entries are served by mlx_lm.server",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the listing printed\n%s\nwant the line %q", out, want)
		}
	}
}

// A router holding nothing says so, and points at the verb that changes it.
func TestRouterModelsSaysWhenTheRouterHoldsNothing(t *testing.T) {
	app, out, _, _ := routerApp(routerTree(), &fakeServers{}, picks.Router{})

	if code := app.run([]string{"router", "models"}, "test"); code != exitOK {
		t.Fatalf("`cria router models` exited %d, want %d", code, exitOK)
	}
	if !strings.Contains(out.String(), "holds no models") || !strings.Contains(out.String(), "cria router include") {
		t.Errorf("the listing printed\n%s\nwant it to say the router holds nothing and how to change that", out)
	}
}

// `cria router status` reports the supervisor and then each model it holds: the
// name a client sends, the router's own word for what that model is doing, and
// the reference it lists.
func TestRouterStatusShowsWhatEachModelIsDoing(t *testing.T) {
	fake := &fakeServers{
		routerRecord: routerRecord(), routerFound: true, routerLive: true,
		health: serve.Health{URL: "http://127.0.0.1:11434/health", Green: true, Status: 200, Detail: "200 OK"},
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", Aliases: []string{"qwen"}, State: "loaded", Phase: serve.PhaseRunning},
			{Model: "LiquidAI/LFM2.5-2.6B-GGUF:Q8_0", Aliases: []string{"lfm"}, State: "unloaded"},
		}},
	}
	app, out, _, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "status"}, "test"); code != exitOK {
		t.Fatalf("`cria router status` exited %d, want %d", code, exitOK)
	}
	for _, want := range []string{
		"router  running  router",
		"qwen  loaded    unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"lfm   unloaded  LiquidAI/LFM2.5-2.6B-GGUF:Q8_0",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the status printed\n%s\nwant the line %q", out, want)
		}
	}
}

// A router that cannot be asked what it holds says why, and the status still
// reports the supervisor it could observe.
func TestRouterStatusSaysWhyItCouldNotListTheModels(t *testing.T) {
	fake := &fakeServers{
		routerRecord: routerRecord(), routerFound: true, routerLive: true,
		routerChildren: serve.RouterChildren{Detail: "http://127.0.0.1:11434/models: connection refused"},
	}
	app, out, _, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "status"}, "test"); code != exitOK {
		t.Fatalf("`cria router status` exited %d, want %d", code, exitOK)
	}
	if !strings.Contains(out.String(), "connection refused") {
		t.Errorf("the status printed\n%s\nwant it to say why the models could not be listed", out)
	}
}

// `cria router load <id>` and `cria router unload <id>` act on one of the models
// the router says it holds, addressed by the name it answers to.
func TestRouterLoadAndUnloadActOnOneModel(t *testing.T) {
	fake := &fakeServers{
		routerRecord: routerRecord(), routerFound: true, routerLive: true,
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", Aliases: []string{"qwen"}, State: "unloaded"},
		}},
	}
	app, out, _, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "load", "qwen"}, "test"); code != exitOK {
		t.Fatalf("`cria router load` exited %d, want %d", code, exitOK)
	}
	if code := app.run([]string{"router", "unload", "qwen"}, "test"); code != exitOK {
		t.Fatalf("`cria router unload` exited %d, want %d", code, exitOK)
	}
	if strings.Join(fake.loaded, ",") != "qwen" || strings.Join(fake.unloaded, ",") != "qwen" {
		t.Errorf("cria loaded %v and unloaded %v, want the model the command named", fake.loaded, fake.unloaded)
	}
	if !strings.Contains(out.String(), "loaded qwen") || !strings.Contains(out.String(), "unloaded qwen") {
		t.Errorf("the verbs printed\n%s\nwant each to say what it did", out)
	}
}

// A name the router does not hold is refused before anything is sent, naming
// what it does hold: the difference between "include it and restart" and a bare
// 404 from somebody else's endpoint.
func TestRouterLoadRefusesAModelTheRouterDoesNotHold(t *testing.T) {
	fake := &fakeServers{
		routerRecord: routerRecord(), routerFound: true, routerLive: true,
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", Aliases: []string{"qwen"}, State: "loaded"},
		}},
	}
	app, _, errOut, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "load", "lfm"}, "test"); code != exitFailure {
		t.Fatalf("loading a model the router does not hold exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "does not hold \"lfm\"") || !strings.Contains(errOut.String(), "it holds qwen") {
		t.Errorf("the refusal reads\n%s\nwant it to name what the router holds instead", errOut)
	}
	if len(fake.loaded) != 0 {
		t.Errorf("cria sent %v after refusing the load", fake.loaded)
	}
}

// With no router running there is nothing to load into, and the refusal names
// the verb that changes that.
func TestRouterLoadRefusesWithNoRouterRunning(t *testing.T) {
	app, _, errOut, _ := routerApp(routerTree(), &fakeServers{}, picks.Router{})

	if code := app.run([]string{"router", "unload", "qwen"}, "test"); code != exitFailure {
		t.Fatalf("unloading with no router exited %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "no router is running") || !strings.Contains(errOut.String(), "cria router start") {
		t.Errorf("the refusal reads\n%s\nwant it to say there is no router and how to start one", errOut)
	}
}

// Unloading a model that is answering somebody would cut that answer off, so it
// is refused — the gate a validation puts in front of displacing a busy server,
// with the same override.
func TestRouterUnloadRefusesAModelThatIsAnswering(t *testing.T) {
	fake := &fakeServers{
		routerRecord: routerRecord(), routerFound: true, routerLive: true,
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", Aliases: []string{"qwen"}, State: "loaded"},
		}},
		routerGeneration: serve.Generation{Busy: serve.BusyGenerating},
	}
	app, _, errOut, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "unload", "qwen"}, "test"); code != exitFailure {
		t.Fatalf("unloading a busy model exited %d, want %d", code, exitFailure)
	}
	if len(fake.unloaded) != 0 {
		t.Errorf("cria unloaded %v while it was answering a request", fake.unloaded)
	}
	if !strings.Contains(errOut.String(), "answering a request right now") {
		t.Errorf("the refusal reads\n%s\nwant it to say the model is mid-answer", errOut)
	}

	// The operator answering for it is what lets the unload through, and cria says
	// what it is doing over the top of.
	if code := app.run([]string{"router", "unload", "qwen", ignoreBusyFlag}, "test"); code != exitOK {
		t.Fatalf("unloading with %s exited %d, want %d", ignoreBusyFlag, code, exitOK)
	}
	if strings.Join(fake.unloaded, ",") != "qwen" {
		t.Errorf("cria unloaded %v, want the model the override named", fake.unloaded)
	}
	if !strings.Contains(errOut.String(), "mid-answer") {
		t.Errorf("the override printed\n%s\nwant a note saying what it cut off", errOut)
	}
}

// A signal cria cannot read is neither busy nor idle: the unload goes ahead with
// the risk named, the way a validation proceeds over an unverifiable holder.
func TestRouterUnloadProceedsWhenItCannotTell(t *testing.T) {
	fake := &fakeServers{
		routerRecord: routerRecord(), routerFound: true, routerLive: true,
		routerChildren: serve.RouterChildren{Children: []serve.RouterChild{
			{Model: "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", Aliases: []string{"qwen"}, State: "loaded"},
		}},
		routerGeneration: serve.Generation{Busy: serve.BusyUnverifiable, Detail: "the endpoint is not enabled"},
	}
	app, _, errOut, _ := routerApp(routerTree(), fake, picks.Router{})

	if code := app.run([]string{"router", "unload", "qwen"}, "test"); code != exitOK {
		t.Fatalf("unloading a model cria cannot judge exited %d, want %d", code, exitOK)
	}
	if strings.Join(fake.unloaded, ",") != "qwen" {
		t.Errorf("cria unloaded %v, want the model that was named", fake.unloaded)
	}
	if !strings.Contains(errOut.String(), "cannot tell whether qwen is generating") {
		t.Errorf("the unload printed\n%s\nwant the risk named", errOut)
	}
}

// The verbs that take a model take exactly one, and a verb the subcommand does
// not have is a command line cria cannot route.
func TestRouterVerbsRefuseWhatTheyCannotRoute(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "a verb that does not exist", args: []string{"router", "reload"}, want: "no such verb"},
		{name: "an argument to a verb that takes none", args: []string{"router", "status", "qwen"}, want: "takes no arguments"},
		{name: "two entries to include", args: []string{"router", "include", "qwen", "gemma"}, want: "one entry at a time"},
		{name: "nothing to include", args: []string{"router", "include"}, want: "one entry at a time"},
		{name: "two models to load", args: []string{"router", "load", "qwen", "lfm"}, want: "one model at a time"},
		{name: "a flag the subcommand does not have", args: []string{"router", "unload", "qwen", "--force"}, want: "unknown flag"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, _, errOut, _ := routerApp(routerTree(), &fakeServers{}, picks.Router{})
			if code := app.run(test.args, "test"); code != exitUsage {
				t.Fatalf("`cria %s` exited %d, want %d", strings.Join(test.args, " "), code, exitUsage)
			}
			if !strings.Contains(errOut.String(), test.want) {
				t.Errorf("the refusal reads\n%s\nwant it to say %q", errOut, test.want)
			}
		})
	}
}
