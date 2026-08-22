package cli

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/picks"
	"cria/internal/serve"
	"cria/internal/tools"
)

// A start that nothing refuses spawns the entry and prints what was launched —
// the composed command line included, since that is the entry's documentation
// (docs/specs/CONFIG.md).
func TestStartSpawnsAndReportsTheLaunch(t *testing.T) {
	fake := &fakeServers{record: testRecord()}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.start([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.started) != 1 || fake.started[0].ID != "qwen" {
		t.Fatalf("cria started %+v, want the one entry that was named", fake.started)
	}
	if len(fake.asked) != 1 || fake.asked[0] != 8080 {
		t.Errorf("cria checked ports %v, want the entry's own port before the spawn", fake.asked)
	}

	printed := out.String()
	for _, want := range []string{"started qwen as pid 4242 on 0.0.0.0:8080", "llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", "qwen-20260818-145730.log"} {
		if !strings.Contains(printed, want) {
			t.Errorf("cria printed %q, want it to contain %q", printed, want)
		}
	}
}

// A start launches an entry under picks: the config defaults, which is what a
// bare `cria start` composes with (docs/specs/CONFIG.md).
func TestStartCarriesTheEntrysDefaultPicks(t *testing.T) {
	tree := testTree()
	tree.Entries = append(tree.Entries, choicesEntry())
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)

	if code := app.start([]string{"qwen-choices"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q4"}) {
		t.Errorf("the start carried the picks %v, want the entry's first option per axis", fake.picked)
	}

	// A flat entry carries none, which is what makes its launch what it always was.
	fake = &fakeServers{record: testRecord()}
	app, _, errOut = newTestApp(tree, fake)
	if code := app.start([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || len(fake.picked[0]) != 0 {
		t.Errorf("a flat entry's start carried the picks %v, want none", fake.picked)
	}
}

// A pick that names nothing is refused with entry validation, ahead of both
// gates (docs/specs/SERVE.md, Start 1): the answer names the valid options, and
// no tool was execed and no port asked to produce it.
func TestStartRefusesAnUnresolvableSelectionBeforeTheGates(t *testing.T) {
	tree := testTree()
	entry := choicesEntry()
	tree.Entries = append(tree.Entries, entry)
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)

	checked := 0
	app.tools = func(settings config.Settings) tools.Report {
		checked++
		return usableReport()
	}

	if code := app.startEntry(tree, entry, config.Selection{"quant": "q8"}, false); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{`has no option named "q8"`, "q4, q6"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
	if checked != 0 || len(fake.asked) != 0 || len(fake.started) != 0 {
		t.Errorf("cria checked the tools %d times, asked about ports %v and started %+v before resolving the picks",
			checked, fake.asked, fake.started)
	}
}

// An id that names no entry lists the ids that do — the answer to "what did I
// mistype" (docs/specs/CLI.md).
func TestStartRefusesAnUnknownEntry(t *testing.T) {
	tree := testTree()
	tree.Entries = append(tree.Entries, config.Entry{ID: "gemma", Backend: config.BackendLlama, Repo: "ggml-org/gemma-3-4b-it-GGUF", Port: 8080, Host: "0.0.0.0"})
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)

	if code := app.start([]string{"qwn"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), `no entry named "qwn"`) || !strings.Contains(errOut.String(), "available entries: qwen, gemma") {
		t.Errorf("cria printed %q, want the unknown id and the ids that exist", errOut)
	}
	if len(fake.started) != 0 {
		t.Errorf("cria started %+v for an id that names nothing", fake.started)
	}
}

// An entry whose file is broken reports its own failure: the author needs the
// offending key and the file, not a list of the entries that happen to parse
// (docs/specs/CONFIG.md).
func TestStartReportsABrokenEntry(t *testing.T) {
	tree := testTree()
	tree.Broken = []config.BrokenEntry{{
		ID:   "gemma",
		Path: "/home/u/.config/cria/models/gemma.toml",
		Err:  &config.KeyError{Key: "port", Reason: "required: this entry sets no port and config.toml sets no default_port"},
	}}
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)

	if code := app.start([]string{"gemma"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"models/gemma.toml", `key "port"`, "fix that file"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
	if len(fake.asked) != 0 {
		t.Errorf("cria checked a port for an entry that never loaded: %v", fake.asked)
	}
}

// The tool gate comes before the port check: a host without llama-server has to
// hear about llama-server (docs/specs/SERVE.md).
func TestStartRefusesWhenTheToolIsUnusable(t *testing.T) {
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(testTree(), fake)
	app.tools = func(config.Settings) tools.Report {
		report := usableReport()
		report.LlamaServer = tools.Tool{
			Name:     tools.LlamaServer,
			Status:   tools.StatusMissing,
			Disables: "starting llama entries; they stay listed, marked unstartable",
			Fix:      "install llama.cpp so llama-server is on PATH",
		}
		return report
	}

	if code := app.start([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"llama-server is missing", "install llama.cpp"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
	if len(fake.asked) != 0 || len(fake.started) != 0 {
		t.Errorf("cria went on to check a port (%v) or spawn (%+v) with no usable tool", fake.asked, fake.started)
	}
}

// A port held by a server cria started is the refusal with a fix in it: stop
// that entry (docs/specs/SERVE.md). Both phrasings are the same branch — the
// entry being started, and another one already on its port.
func TestStartRefusesAPortAManagedServerHolds(t *testing.T) {
	cases := []struct {
		name    string
		holder  string
		wantAll []string
	}{
		{
			name:    "the entry itself is already running",
			holder:  "qwen",
			wantAll: []string{"qwen is already running as pid 4242 on port 8080", "stop it first"},
		},
		{
			name:    "another entry is serving on that port",
			holder:  "gemma",
			wantAll: []string{"port 8080 is already serving gemma (pid 4242)", "stop gemma first"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			held := testRecord()
			held.EntryID = test.holder
			fake := &fakeServers{
				record: testRecord(),
				use:    serve.PortUse{Managed: &serve.Server{Record: held, Live: true}},
			}
			app, _, errOut := newTestApp(testTree(), fake)

			if code := app.start([]string{"qwen"}); code != exitFailure {
				t.Fatalf("exit code %d, want %d", code, exitFailure)
			}
			for _, want := range test.wantAll {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if len(fake.started) != 0 {
				t.Errorf("cria spawned onto a busy port: %+v", fake.started)
			}
		})
	}
}

// A port held by anything else is reported with the pid, the command line and
// the working directory — and left alone: the kill is the TUI's offer, never the
// CLI's (docs/specs/SERVE.md).
func TestStartRefusesAForeignPortHolder(t *testing.T) {
	fake := &fakeServers{
		record: testRecord(),
		use: serve.PortUse{Holders: []serve.Holder{{
			PID:        9001,
			Command:    "/opt/homebrew/bin/llama-server -m gemma.gguf --port 8080",
			WorkingDir: "/Users/someone/models",
		}}},
	}
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.start([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{
		"port 8080 is held by a process cria did not start",
		"pid 9001",
		"llama-server -m gemma.gguf",
		"working directory /Users/someone/models",
		"models/qwen.toml",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
	if len(fake.started) != 0 {
		t.Errorf("cria spawned onto a foreign process's port: %+v", fake.started)
	}
}

// A holder whose details could not be read still refuses the start: the pid is
// what the port is taken by, and it is named.
func TestStartRefusesAHolderItCannotDescribe(t *testing.T) {
	fake := &fakeServers{record: testRecord(), use: serve.PortUse{Holders: []serve.Holder{{PID: 9001}}}}
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.start([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "pid 9001") || !strings.Contains(errOut.String(), "unreadable") {
		t.Errorf("cria printed %q, want the pid and what could not be read", errOut)
	}
}

// --wait watches the start until the phase settles: running is the answer the
// caller asked for (docs/specs/CLI.md).
func TestStartWaitsForRunning(t *testing.T) {
	fake := &fakeServers{
		record: testRecord(),
		phases: []serve.Phase{serve.PhaseStarting, serve.PhaseStarting, serve.PhaseRunning},
		health: serve.Health{URL: "http://127.0.0.1:8080/health", Green: true, Status: 200, Detail: "200 OK"},
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.start([]string{"qwen", "--wait"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if fake.observed != 3 {
		t.Errorf("the wait took %d observations, want the three the phases scripted", fake.observed)
	}
	if !strings.Contains(out.String(), "qwen is running") || !strings.Contains(out.String(), "200 OK") {
		t.Errorf("cria printed %q, want the verdict and what the health endpoint answered", out)
	}
}

// A green port is not on its own proof that cria's own server answered it, so
// the wait asks who is listening before it green-lights the start. The three
// answers are: cria's pid holds the port (green), someone else does (refused,
// both pids named), and the question could not be asked (green, with the note —
// the health signal is primary, attribution is corroboration).
func TestStartWaitVerifiesTheListener(t *testing.T) {
	cases := []struct {
		name         string
		listeners    []int
		listenersSet bool
		listenersErr error
		want         int
		contains     []string
	}{
		{
			name:     "the port is held by the server cria started",
			want:     exitOK,
			contains: []string{"qwen is running"},
		},
		{
			name:         "the port is held by another process",
			listeners:    []int{9001},
			listenersSet: true,
			want:         exitFailure,
			contains:     []string{"port :8080 answers", "not the server cria started", "pid 4242", "listener(s) 9001"},
		},
		{
			name:         "nothing is listening at the moment cria looked",
			listeners:    []int{},
			listenersSet: true,
			want:         exitFailure,
			contains:     []string{"listener(s) none"},
		},
		{
			name:         "the port could not be attributed",
			listenersErr: errors.New("lsof did not answer within 3s"),
			want:         exitOK,
			contains:     []string{"qwen is running", "note: cannot confirm that pid 4242 is what answers on port 8080", "lsof did not answer"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{
				record:       testRecord(),
				phases:       []serve.Phase{serve.PhaseRunning},
				health:       serve.Health{URL: "http://127.0.0.1:8080/health", Green: true, Status: 200, Detail: "200 OK"},
				listeners:    test.listeners,
				listenersSet: test.listenersSet,
				listenersErr: test.listenersErr,
			}
			app, out, errOut := newTestApp(testTree(), fake)

			if code := app.start([]string{"qwen", "--wait"}); code != test.want {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, test.want, errOut)
			}
			printed := out.String() + errOut.String()
			for _, want := range test.contains {
				if !strings.Contains(printed, want) {
					t.Errorf("cria printed %q, want it to contain %q", printed, want)
				}
			}
			if test.want == exitFailure && strings.Contains(out.String(), "is running") {
				t.Errorf("cria green-lit a start whose port another process answers: %q", out)
			}
		})
	}
}

// An mlx server is running as soon as it answers, but it has not loaded a single
// weight yet: --wait means ready, so cria sends the completion that loads them
// and the verdict waits for the answer. A llama server has loaded its model
// before it answers at all, so nothing is sent for one.
func TestStartWaitWarmsAnMLXServer(t *testing.T) {
	cases := []struct {
		name     string
		backend  config.Backend
		wantWarm bool
	}{
		{name: "an mlx server", backend: config.BackendMLX, wantWarm: true},
		{name: "a llama server", backend: config.BackendLlama},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			record := testRecord()
			record.Backend = test.backend
			fake := &fakeServers{
				record: record,
				phases: []serve.Phase{serve.PhaseRunning},
				health: serve.Health{URL: "http://127.0.0.1:8080/v1/models", Green: true, Status: 200, Detail: "200 OK"},
			}
			app, out, errOut := newTestApp(testTree(), fake)

			// What stdout held when the load began: the verdict must not be in it
			// yet — a caller reading "is running" is told the server is ready.
			var whenWarming string
			fake.onWarm = func() { whenWarming = out.String() }

			if code := app.start([]string{"qwen", "--wait"}); code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
			}
			if warmed := len(fake.warmed) == 1; warmed != test.wantWarm {
				t.Fatalf("cria warmed %v, want the warm to be %v for a %s server", fake.warmed, test.wantWarm, test.backend)
			}

			const said = "loading model weights (mlx loads lazily; this can take a while)…"
			if got := strings.Contains(errOut.String(), said); got != test.wantWarm {
				t.Errorf("stderr reads %q, want the loading line to be %v", errOut, test.wantWarm)
			}
			if !test.wantWarm {
				return
			}
			if strings.Contains(whenWarming, "is running") {
				t.Errorf("cria green-lit the start before the weights were loaded: %q", whenWarming)
			}
			if !strings.Contains(out.String(), "qwen is running") {
				t.Errorf("cria printed %q, want the verdict once the completion answered", out)
			}
			// The line is progress on stderr, not the answer: stdout stays what a
			// script reads.
			if strings.Contains(out.String(), "loading model weights") {
				t.Errorf("cria printed the loading line on stdout: %q", out)
			}
		})
	}
}

// A warm that does not come back fails the start: cria asked for a completion
// and never got one. The server may well still be up — the refusal says so, and
// points at the log rather than claiming it died.
func TestStartWaitFailsWhenTheWeightsDoNotLoad(t *testing.T) {
	record := testRecord()
	record.Backend = config.BackendMLX
	fake := &fakeServers{
		record:  record,
		phases:  []serve.Phase{serve.PhaseRunning},
		health:  serve.Health{URL: "http://127.0.0.1:8080/v1/models", Green: true, Status: 200, Detail: "200 OK"},
		warmErr: errors.New("qwen did not load its weights: http://127.0.0.1:8080/v1/completions: no answer within 15m0s"),
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.start([]string{"qwen", "--wait"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}
	for _, want := range []string{"did not load its weights", "no answer within 15m0s", "may still be up", testRecord().LogPath} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
	if strings.Contains(out.String(), "is running") {
		t.Errorf("cria green-lit a start whose weights never loaded: %q", out)
	}
}

// A start that does not wait leaves the weights unloaded, and says so with the
// command that loads them now.
func TestStartWithoutWaitNotesTheLazyLoad(t *testing.T) {
	cases := []struct {
		name     string
		backend  config.Backend
		wantNote bool
	}{
		{name: "an mlx server", backend: config.BackendMLX, wantNote: true},
		{name: "a llama server", backend: config.BackendLlama},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			record := testRecord()
			record.Backend = test.backend
			fake := &fakeServers{record: record}
			app, out, errOut := newTestApp(testTree(), fake)

			if code := app.start([]string{"qwen"}); code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
			}
			const note = "note: mlx loads model weights on the first request; `cria start qwen --wait` loads them now"
			if got := strings.Contains(errOut.String(), note); got != test.wantNote {
				t.Errorf("stderr reads %q, want the note to be %v for a %s server", errOut, test.wantNote, test.backend)
			}
			if len(fake.warmed) != 0 {
				t.Errorf("a start that was not asked to wait loaded %v", fake.warmed)
			}
			// The note is an aside: the answer a script reads stays on stdout.
			if strings.Contains(out.String(), "note:") {
				t.Errorf("cria printed the note on stdout: %q", out)
			}
		})
	}
}

// A start that fails while cria waits exits non-zero with the log path: the log
// is the only crash evidence there is (docs/specs/SERVE.md).
func TestStartWaitReportsAFailedStart(t *testing.T) {
	cases := []struct {
		name     string
		phase    serve.Phase
		contains string
	}{
		{name: "a server that exited", phase: serve.PhaseExited, contains: "it exited"},
		{name: "a server that stopped answering", phase: serve.PhaseUnhealthy, contains: "it stopped answering"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{
				record: testRecord(),
				phases: []serve.Phase{test.phase},
				health: serve.Health{URL: "http://127.0.0.1:8080/health", Status: 503, Detail: "503 Service Unavailable"},
			}
			app, _, errOut := newTestApp(testTree(), fake)

			if code := app.start([]string{"qwen", "--wait"}); code != exitFailure {
				t.Fatalf("exit code %d, want %d", code, exitFailure)
			}
			if !strings.Contains(errOut.String(), test.contains) {
				t.Errorf("cria printed %q, want it to contain %q", errOut, test.contains)
			}
			if !strings.Contains(errOut.String(), testRecord().LogPath) {
				t.Errorf("cria printed %q, want the log path a crash is read from", errOut)
			}
		})
	}
}

// A start that never settles is bound by its window: the wait ends naming the
// phase it is stuck in, non-zero, with the log to look at.
func TestStartWaitTimesOut(t *testing.T) {
	fake := &fakeServers{record: testRecord(), phases: []serve.Phase{serve.PhaseStarting}}
	app, _, errOut := newTestApp(testTree(), fake)
	app.startWindow = 20 * time.Millisecond

	if code := app.start([]string{"qwen", "--wait"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "still starting after") {
		t.Errorf("cria printed %q, want the phase it gave up in", errOut)
	}
}

// A downloading start prints where the fetch has got to, and buys the download
// budget: a model coming over the network must not be cut off by the window a
// cached model is judged against.
func TestStartWaitFollowsADownload(t *testing.T) {
	fake := &fakeServers{
		record:   testRecord(),
		phases:   []serve.Phase{serve.PhaseDownloading, serve.PhaseDownloading, serve.PhaseRunning},
		progress: serve.Progress{Bytes: 3 << 30, Total: 12 << 30, Known: true},
		health:   serve.Health{URL: "http://127.0.0.1:8080/health", Green: true, Status: 200, Detail: "200 OK"},
	}
	app, out, errOut := newTestApp(testTree(), fake)
	// A window that has already run out for a start, so only the download's own
	// budget can carry this wait to its verdict.
	app.startWindow = time.Nanosecond
	app.downloadWindow = 2 * time.Second

	if code := app.start([]string{"qwen", "--wait"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !strings.Contains(out.String(), "downloading 3.0 GiB of 12.0 GiB (25%)") {
		t.Errorf("cria printed %q, want the bytes fetched against the total", out)
	}
	if !strings.Contains(out.String(), "qwen is running") {
		t.Errorf("cria printed %q, want the verdict the wait settled on", out)
	}
}

// storedIn points an invocation at a picks store on disk. Every test that is
// about the store uses a temp state root: what cria writes there is exactly what
// is being asserted, and nothing of the machine's own state is read or touched.
func storedIn(a *app, root string) {
	a.picksStore = func() (picks.Picks, error) { return picks.Load(root) }
}

// writeStoredPicks puts a picks store in a state root, spelled by hand — the
// file cria wrote on some earlier run (internal/picks).
func writeStoredPicks(t *testing.T, root, document string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "choices.json"), []byte(document), 0o644); err != nil {
		t.Fatalf("writing the picks store: %v", err)
	}
}

// A pick on the command line is what the start composes with, typed on either
// side of the id like the flag it shares the line with (docs/specs/CLI.md).
func TestStartTakesPicksFromTheCommandLine(t *testing.T) {
	tree := testTree()
	tree.Entries = append(tree.Entries, choicesEntry())

	for _, args := range [][]string{{"qwen-choices", "quant=q6"}, {"quant=q6", "qwen-choices"}} {
		fake := &fakeServers{record: testRecord()}
		app, _, errOut := newTestApp(tree, fake)

		if code := app.start(args); code != exitOK {
			t.Fatalf("`cria start %s` exited %d, want %d (stderr: %s)", strings.Join(args, " "), code, exitOK, errOut)
		}
		if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q6"}) {
			t.Errorf("`cria start %s` carried the picks %v, want the one that was typed",
				strings.Join(args, " "), fake.picked)
		}
	}

	// Several picks compose together, and an axis nobody named keeps its config
	// default.
	tree = testTree()
	tree.Entries = append(tree.Entries, pickyEntry())
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)

	if code := app.start([]string{"qwen-choices", "quant=q6", "layout=coding"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q6", "layout": "coding"}) {
		t.Errorf("the start carried the picks %v, want both of the ones that were typed", fake.picked)
	}
}

// A bare start launches what was picked last: the store's picks over the config
// defaults, and an explicit pick over both — per axis, so naming one leaves the
// others where they were (docs/specs/CONFIG.md, Choices).
func TestStartLaunchesTheStoredPicks(t *testing.T) {
	root := t.TempDir()
	writeStoredPicks(t, root, `{"qwen-choices": {"quant": "q6"}}`)

	tree := testTree()
	tree.Entries = append(tree.Entries, pickyEntry())

	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)
	storedIn(app, root)

	if code := app.start([]string{"qwen-choices"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q6", "layout": "chat"}) {
		t.Errorf("a bare start carried the picks %v, want the stored quant and the default layout", fake.picked)
	}

	fake = &fakeServers{record: testRecord()}
	app, _, errOut = newTestApp(tree, fake)
	storedIn(app, root)

	if code := app.start([]string{"qwen-choices", "layout=coding"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q6", "layout": "coding"}) {
		t.Errorf("the start carried the picks %v, want the typed layout over the stored quant", fake.picked)
	}

	// A stored pick the entry no longer holds is not an error: the config default
	// stands in for it, and the launch goes ahead.
	writeStoredPicks(t, root, `{"qwen-choices": {"quant": "q8"}}`)
	fake = &fakeServers{record: testRecord()}
	app, _, errOut = newTestApp(tree, fake)
	storedIn(app, root)

	if code := app.start([]string{"qwen-choices"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q4", "layout": "chat"}) {
		t.Errorf("a start under a stale stored pick carried %v, want the config defaults", fake.picked)
	}
}

// A pick on the command line writes nothing. It composes one launch and is gone
// after it, so an agent's experiment never changes what the next bare start
// launches (docs/specs/CONFIG.md, Choices).
func TestStartNeverWritesThePicksStore(t *testing.T) {
	tree := testTree()
	tree.Entries = append(tree.Entries, choicesEntry())

	// A state root with no store at all: a start under an explicit pick must not
	// be what creates one.
	root := t.TempDir()
	fake := &fakeServers{record: testRecord()}
	app, _, errOut := newTestApp(tree, fake)
	storedIn(app, root)

	if code := app.start([]string{"qwen-choices", "quant=q6"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	left, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the state root: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("the start left %d file(s) in the state root, want the store untouched: %v", len(left), left)
	}

	// And a store that was already there comes back byte for byte: the pick that
	// overrode it changed the launch, not the file.
	document := `{"qwen-choices": {"quant": "q6"}}`
	writeStoredPicks(t, root, document)

	fake = &fakeServers{record: testRecord()}
	app, _, errOut = newTestApp(tree, fake)
	storedIn(app, root)

	if code := app.start([]string{"qwen-choices", "quant=q4"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q4"}) {
		t.Errorf("the start carried the picks %v, want the one-shot pick over the stored one", fake.picked)
	}
	after, err := os.ReadFile(filepath.Join(root, "choices.json"))
	if err != nil {
		t.Fatalf("reading the picks store back: %v", err)
	}
	if string(after) != document {
		t.Errorf("the picks store reads %q, want it exactly as it was: %q", after, document)
	}
}

// A picks store cria cannot read is an aside, not a refusal: the launch goes
// ahead on the config defaults, and the note is what says they may not be what
// was picked last (docs/specs/CONFIG.md, Choices).
func TestStartNotesABrokenPicksStore(t *testing.T) {
	root := t.TempDir()
	writeStoredPicks(t, root, "{ this was hand-edited")

	tree := testTree()
	tree.Entries = append(tree.Entries, choicesEntry())
	fake := &fakeServers{record: testRecord()}
	app, out, errOut := newTestApp(tree, fake)
	storedIn(app, root)

	if code := app.start([]string{"qwen-choices"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q4"}) {
		t.Errorf("the start carried the picks %v, want the config defaults", fake.picked)
	}
	for _, want := range []string{"note:", "unreadable", "using the config defaults"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q on stderr, want it to contain %q", errOut, want)
		}
	}
	if !strings.Contains(out.String(), "started qwen") {
		t.Errorf("cria printed %q on stdout, want the launch the note did not stop", out)
	}
}

// A pick cria cannot read as one is a command line it cannot route: it costs the
// usage code and names the form it wanted (docs/specs/CLI.md).
func TestStartRefusesAPickItCannotRead(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains string
	}{
		{
			name:     "the same choice picked twice",
			args:     []string{"qwen-choices", "quant=q4", "quant=q6"},
			contains: `choice "quant" is picked twice (q4 and q6)`,
		},
		{
			name:     "a pick with no choice",
			args:     []string{"qwen-choices", "=q4"},
			contains: `"=q4" is not a pick`,
		},
		{
			name:     "a pick with no option",
			args:     []string{"qwen-choices", "quant="},
			contains: `"quant=" is not a pick`,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tree := testTree()
			tree.Entries = append(tree.Entries, choicesEntry())
			fake := &fakeServers{record: testRecord()}
			app, _, errOut := newTestApp(tree, fake)

			if code := app.start(test.args); code != exitUsage {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitUsage, errOut)
			}
			for _, want := range []string{test.contains, "usage: " + startSynopsis} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if len(fake.started) != 0 {
				t.Errorf("cria started %+v on a command line it could not read", fake.started)
			}
		})
	}
}

// A pick that reads as one but names nothing the entry has is a failure, not a
// usage error: the command line was routable, the entry just has no such
// combination — and the refusal names what it does have (docs/specs/CLI.md).
func TestStartRefusesPicksTheEntryDoesNotHave(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		wants []string
	}{
		{
			name:  "a choice the entry has not",
			args:  []string{"qwen-choices", "qunt=q4"},
			wants: []string{`has no choice named "qunt"`, "its choices are: quant"},
		},
		{
			name:  "an option the choice has not",
			args:  []string{"qwen-choices", "quant=q8"},
			wants: []string{`has no option named "q8"`, "its options are: q4, q6"},
		},
		{
			name:  "a pick against a flat entry",
			args:  []string{"qwen", "quant=q4"},
			wants: []string{"has no choices", "quant"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tree := testTree()
			tree.Entries = append(tree.Entries, choicesEntry())
			fake := &fakeServers{record: testRecord()}
			app, _, errOut := newTestApp(tree, fake)

			if code := app.start(test.args); code != exitFailure {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
			}
			for _, want := range test.wants {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if len(fake.asked) != 0 || len(fake.started) != 0 {
				t.Errorf("cria asked about ports %v and started %+v for a pick that names nothing",
					fake.asked, fake.started)
			}
		})
	}
}
