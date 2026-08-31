package cli

import (
	"strings"
	"testing"
	"time"

	"cria/internal/config"
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
