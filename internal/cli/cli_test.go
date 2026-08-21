package cli

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/serve"
	"cria/internal/tools"
)

// fakeServers is the manager a component test drives: what the state directory
// holds, who holds a port, and what a started server reports on each
// observation. It is the whole seam between this package and a running host, so
// every refusal, every exit code and every rendered document is exercised with
// no server, no port and no state directory.
type fakeServers struct {
	listing   serve.Listing       // what List answers
	snapshots serve.StatusListing // what Snapshots answers
	use       serve.PortUse       // who holds the port a start asks about
	record    serve.Record        // the record Start hands back
	phases    []serve.Phase       // the phases successive Snapshots report; the last one repeats
	progress  serve.Progress      // carried by a snapshot whose phase is downloading
	health    serve.Health        // carried by every snapshot of a started server

	// Who the operating system says is listening on the started server's port.
	// Nil means the record's own pid, which is the normal case: a test only
	// scripts this when it is about attribution.
	listeners    []int
	listenersSet bool

	started  []config.Entry // the entries Start was called with, in order
	stopped  []string       // the entries Stop was called for, in order
	warmed   []string       // the servers Warm was asked to load, in order
	benched  []string       // the servers Bench was asked to measure, in order
	specs    []serve.BenchSpec
	asked    []int // the ports PortUse was asked about, in order
	observed int   // how many snapshots the wait took

	// What a sweep comes back with, and the progress it reports on the way. A
	// test that only cares that a bench ran leaves both empty.
	benchSizes []serve.BenchSize
	benchSteps []serve.BenchStep

	// onWarm runs while Warm is in flight, so a test can read what cria had
	// printed by the time it started loading the weights.
	onWarm func()

	startErr     error
	stopErr      error
	listErr      error
	snapshotErr  error
	portErr      error
	listenersErr error
	warmErr      error
}

func (f *fakeServers) Start(entry config.Entry, _ tools.Report) (serve.Record, error) {
	f.started = append(f.started, entry)
	if f.startErr != nil {
		return serve.Record{}, f.startErr
	}
	return f.record, nil
}

func (f *fakeServers) Stop(record serve.Record) error {
	f.stopped = append(f.stopped, record.EntryID)
	return f.stopErr
}

func (f *fakeServers) List() (serve.Listing, error) {
	if f.listErr != nil {
		return serve.Listing{}, f.listErr
	}
	return f.listing, nil
}

// Running answers off the same listing List serves: the live server with that
// entry id, if the fixture holds one.
func (f *fakeServers) Running(entryID string) (serve.Server, bool, error) {
	for _, server := range f.listing.Servers {
		if server.Live && server.EntryID == entryID {
			return server, true, nil
		}
	}
	return serve.Server{}, false, nil
}

func (f *fakeServers) Snapshots() (serve.StatusListing, error) {
	if f.snapshotErr != nil {
		return serve.StatusListing{}, f.snapshotErr
	}
	return f.snapshots, nil
}

// Snapshot walks the phases a test scripted, one per observation, and repeats
// the last one forever — which is what a start that never settles looks like.
func (f *fakeServers) Snapshot(record serve.Record) (serve.Status, error) {
	if f.snapshotErr != nil {
		return serve.Status{}, f.snapshotErr
	}
	phase := serve.PhaseStarting
	if len(f.phases) > 0 {
		phase = f.phases[min(f.observed, len(f.phases)-1)]
	}
	f.observed++

	status := serve.Status{Record: record, Phase: phase, Health: f.health, Uptime: time.Second}
	if phase == serve.PhaseDownloading {
		status.Progress = f.progress
	}
	return status, nil
}

// ListensOn answers with the pids a test scripted, defaulting to the record's
// own pid — the host where the server cria started is the one holding its port.
func (f *fakeServers) ListensOn(record serve.Record) (bool, []int, error) {
	if f.listenersErr != nil {
		return false, nil, f.listenersErr
	}
	pids := f.listeners
	if !f.listenersSet {
		pids = []int{record.PID}
	}
	return slices.Contains(pids, record.PID), pids, nil
}

// Warm answers the way a loaded server does — the completion came back — unless
// a test scripted otherwise. Only the mlx records reach it: the rule about which
// backends are warmed is serve's (docs/specs/SERVE.md).
func (f *fakeServers) Warm(record serve.Record) error {
	if !serve.LoadsLazily(record.Backend) {
		return nil
	}
	f.warmed = append(f.warmed, record.EntryID)
	if f.onWarm != nil {
		f.onWarm()
	}
	return f.warmErr
}

// Bench answers with the sizes a test scripted, reporting whatever progress it
// gave along the way. The sweep itself is serve's — what is exercised here is
// which server the command picked, what it asked for, and how it printed the
// answer.
func (f *fakeServers) Bench(record serve.Record, spec serve.BenchSpec, report func(serve.BenchStep)) serve.BenchResult {
	f.benched = append(f.benched, record.EntryID)
	f.specs = append(f.specs, spec)
	for _, step := range f.benchSteps {
		report(step)
	}
	return serve.BenchResult{
		Record:    record,
		StartedAt: time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC),
		Spec:      spec,
		Sizes:     f.benchSizes,
	}
}

func (f *fakeServers) PortUse(port int) (serve.PortUse, error) {
	f.asked = append(f.asked, port)
	if f.portErr != nil {
		return serve.PortUse{}, f.portErr
	}
	return f.use, nil
}

// newTestApp builds an invocation over fakes, with the --wait windows wound down
// to milliseconds: the wait is about which phase settles it, and a real
// two-minute budget would only make the suite slow.
func newTestApp(tree *config.Tree, fake *fakeServers) (*app, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return &app{
		out:            out,
		err:            errOut,
		tree:           func() (*config.Tree, error) { return tree, nil },
		tools:          func(config.Settings) tools.Report { return usableReport() },
		servers:        func() (servers, error) { return fake, nil },
		memoryMB:       func() (int, error) { return 16384, nil }, // a 16 GiB machine
		tui:            func() error { return nil },
		poll:           time.Millisecond,
		startWindow:    200 * time.Millisecond,
		downloadWindow: 400 * time.Millisecond,
		progressEvery:  time.Millisecond,
	}, out, errOut
}

// usableReport is a tool check that found both servers, so the start gate opens.
func usableReport() tools.Report {
	return tools.Report{
		LlamaServer: tools.Tool{Name: tools.LlamaServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/llama-server", Build: 9000},
		MLXLMServer: tools.Tool{Name: tools.MLXLMServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/mlx_lm.server"},
		HF:          tools.Tool{Name: tools.HF, Status: tools.StatusFound, Path: "/opt/homebrew/bin/hf"},
	}
}

// testTree is a loaded config tree holding one llama entry.
func testTree() *config.Tree {
	return &config.Tree{
		Root: "/home/u/.config/cria",
		Entries: []config.Entry{{
			ID:      "qwen",
			Path:    "/home/u/.config/cria/models/qwen.toml",
			Backend: config.BackendLlama,
			Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
			Quant:   "UD-Q4_K_XL",
			Host:    "0.0.0.0",
			Port:    8080,
			Name:    "Qwen3 30B",
			Args:    []string{"--ctx-size", "16384"},
		}},
	}
}

// testRecord is what cria writes down when it launches the tree's entry.
func testRecord() serve.Record {
	return serve.Record{
		EntryID:    "qwen",
		Backend:    config.BackendLlama,
		Repo:       "unsloth/Qwen3-30B-A3B-GGUF",
		Quant:      "UD-Q4_K_XL",
		Host:       "0.0.0.0",
		Port:       8080,
		PID:        4242,
		Identity:   procs.Identity{Command: "/opt/homebrew/bin/llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", StartedAt: "Tue Aug 18 14:57:30 2026"},
		Command:    []string{"/opt/homebrew/bin/llama-server", "-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", "--host", "0.0.0.0", "--port", "8080"},
		LogPath:    "/home/u/.local/state/cria/logs/qwen-20260818-145730.log",
		LaunchedAt: time.Date(2026, 8, 18, 14, 57, 30, 0, time.UTC),
	}
}

// Routing: the whole v1 surface, and what an unroutable command line costs
// (docs/specs/CLI.md).
func TestRouting(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		want     int
		contains string
	}{
		{name: "the version is printed", args: []string{"--version"}, want: exitOK, contains: "cria 9.9.9-test"},
		{name: "docs prints the config schema", args: []string{"docs"}, want: exitOK, contains: "backend"},
		{name: "an unknown subcommand names the valid set", args: []string{"serve"}, want: exitUsage, contains: "valid subcommands are: start, stop, status, bench, list, new, edit, docs, wired-limit, update"},
		{name: "start needs an entry id", args: []string{"start"}, want: exitUsage, contains: "usage: cria start <id> [--wait]"},
		{name: "start takes one entry id", args: []string{"start", "a", "b"}, want: exitUsage, contains: "one entry at a time (got a, b)"},
		{name: "start refuses a flag it does not know", args: []string{"start", "qwen", "--now"}, want: exitUsage, contains: "unknown flag --now"},
		{name: "stop takes one entry id", args: []string{"stop", "a", "b"}, want: exitUsage, contains: "one entry at a time (got a, b)"},
		{name: "stop refuses a flag it does not know", args: []string{"stop", "--all"}, want: exitUsage, contains: "unknown flag --all"},
		{name: "status takes no arguments", args: []string{"status", "qwen"}, want: exitUsage, contains: "takes no arguments (got qwen)"},
		{name: "status refuses a flag it does not know", args: []string{"status", "--yaml"}, want: exitUsage, contains: "unknown flag --yaml"},
		{name: "new needs an entry id", args: []string{"new"}, want: exitUsage, contains: "usage: cria new <id> [--llama|--mlx]"},
		{name: "new takes one entry id", args: []string{"new", "a", "b"}, want: exitUsage, contains: "one entry at a time (got a, b)"},
		{name: "new refuses both backends at once", args: []string{"new", "a", "--llama", "--mlx"}, want: exitUsage, contains: "name different backends"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app, out, errOut := newTestApp(testTree(), &fakeServers{})
			code := app.run(test.args, "9.9.9-test")
			if code != test.want {
				t.Errorf("exit code %d, want %d (stderr: %s)", code, test.want, errOut)
			}
			printed := out.String() + errOut.String()
			if !strings.Contains(printed, test.contains) {
				t.Errorf("cria printed %q, want it to contain %q", printed, test.contains)
			}
		})
	}
}

// Bare `cria` is the TUI, and a TUI that could not open reports why and exits
// like any other refusal.
func TestBareInvocationOpensTheTUI(t *testing.T) {
	app, _, _ := newTestApp(testTree(), &fakeServers{})
	opened := false
	app.tui = func() error {
		opened = true
		return nil
	}
	if code := app.run(nil, "9.9.9-test"); code != exitOK {
		t.Errorf("exit code %d, want %d", code, exitOK)
	}
	if !opened {
		t.Error("bare `cria` did not open the TUI")
	}

	app, _, errOut := newTestApp(testTree(), &fakeServers{})
	app.tui = func() error { return errors.New("cannot locate the home directory") }
	if code := app.run(nil, "9.9.9-test"); code != exitFailure {
		t.Errorf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "cannot locate the home directory") {
		t.Errorf("cria printed %q, want the failure the TUI met", errOut)
	}
}

// A flag may come before or after the entry id: a scripted caller should not
// have to know which order cria prefers.
func TestFlagsAreAcceptedInEitherOrder(t *testing.T) {
	for _, args := range [][]string{{"--wait", "qwen"}, {"qwen", "--wait"}} {
		fake := &fakeServers{record: testRecord(), phases: []serve.Phase{serve.PhaseRunning}}
		app, out, errOut := newTestApp(testTree(), fake)

		if code := app.start(args); code != exitOK {
			t.Fatalf("`cria start %s` exited %d (stderr: %s)", strings.Join(args, " "), code, errOut)
		}
		if !strings.Contains(out.String(), "is running") {
			t.Errorf("`cria start %s` printed %q, want the wait's verdict", strings.Join(args, " "), out)
		}
	}
}

// A subcommand that cannot reach the state directory fails naming why, rather
// than reporting an empty host.
func TestStateDirectoryFailuresAreReported(t *testing.T) {
	unreachable := func() (servers, error) { return nil, errors.New("cannot locate the home directory") }
	cases := map[string]func(*app) int{
		"start":  func(a *app) int { return a.start([]string{"qwen"}) },
		"stop":   func(a *app) int { return a.stop(nil) },
		"status": func(a *app) int { return a.status(nil) },
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			app, _, errOut := newTestApp(testTree(), &fakeServers{})
			app.servers = unreachable
			if code := run(app); code != exitFailure {
				t.Errorf("exit code %d, want %d", code, exitFailure)
			}
			if !strings.Contains(errOut.String(), "cannot locate the home directory") {
				t.Errorf("cria printed %q, want the failure it met", errOut)
			}
		})
	}
}
