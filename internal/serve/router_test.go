package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/tools"
)

// routerConfig is engines/router.toml as config.Load hands it over: a port of its
// own, the defaults every model it serves starts from, and the router process's
// own flags.
func routerConfig() config.RouterConfig {
	return config.RouterConfig{
		Path:       "/home/u/.config/cria/engines/router.toml",
		Port:       11434,
		Host:       "0.0.0.0",
		Args:       []string{"-ngl", "99", "-fa", "on"},
		RouterArgs: []string{"--models-max", "2"},
	}
}

// routerReport is a tool check whose llama-server can run as a router — the
// second question the report answers about the one binary (docs/specs/TOOLS.md).
func routerReport() tools.Report {
	report := usableReport()
	report.LlamaServer.Router = true
	return report
}

// startRouter starts the router through a fake spawner and leaves the process
// table holding the pid it reports.
func startRouter(t *testing.T, manager *Manager, host *fakeHost, router config.RouterConfig, pid int) (Record, *fakeSpawner) {
	t.Helper()
	spawner := &fakeSpawner{pid: pid}
	manager.spawn = spawner.launch
	if host.alive == nil {
		host.alive = map[int]procs.Identity{}
	}
	host.alive[pid] = identityOf("/opt/homebrew/bin/llama-server --models-preset " +
		filepath.Join(manager.engineRoot(config.BackendRouter), presetFile))

	record, err := manager.StartRouter(router, routerReport())
	if err != nil {
		t.Fatalf("starting the router: %v", err)
	}
	return record, spawner
}

// The router is launched as llama-server in router mode: the preset cria
// composed, the address cria owns, and the file's own flags verbatim after them.
func TestTheRouterIsLaunchedFromTheComposedPreset(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)

	record, spawner := startRouter(t, manager, host, routerConfig(), 4242)

	preset := filepath.Join(manager.engineRoot(config.BackendRouter), presetFile)
	want := []string{
		"/opt/homebrew/bin/llama-server",
		"--models-preset", preset,
		"--host", "0.0.0.0",
		"--port", "11434",
		"--models-max", "2",
	}
	if got := strings.Join(spawner.last().Command, " "); got != strings.Join(want, " ") {
		t.Errorf("the router was launched as\n%s\nwant\n%s", got, strings.Join(want, " "))
	}

	if record.EntryID != string(config.BackendRouter) || record.Backend != config.BackendRouter {
		t.Errorf("the record names entry %q on backend %q, want the engine's own id", record.EntryID, record.Backend)
	}
	if record.Preset != preset {
		t.Errorf("the record serves from %q, want the composed preset %q", record.Preset, preset)
	}
	if record.Repo != "" || record.Quant != "" {
		t.Errorf("the record names model %q:%q; the router serves the models its preset lists", record.Repo, record.Quant)
	}
	if record.PID != 4242 || record.Port != 11434 || record.Host != "0.0.0.0" {
		t.Errorf("the record is %+v, want the pid, port and bind the launch used", record)
	}
}

// The composed preset is state, written whole at every start: what the engine
// file says now is what the router serves.
func TestTheRouterPresetIsWrittenAtEveryStart(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	router := routerConfig()

	record, _ := startRouter(t, manager, host, router, 4242)

	preset := filepath.Join(manager.engineRoot(config.BackendRouter), presetFile)
	if got := readFile(t, preset); got != "[*]\nngl = 99\nfa = on\n" {
		t.Errorf("the preset is\n%q\nwant the engine file's args as its defaults section", got)
	}

	// The engine file changes, and the next start serves what it says now.
	if err := manager.StopRouter(record); err != nil {
		t.Fatalf("stopping the router: %v", err)
	}
	router.Args = []string{"-ngl", "50"}
	startRouter(t, manager, host, router, 4343)

	if got := readFile(t, preset); got != "[*]\nngl = 50\n" {
		t.Errorf("the preset is\n%q\nwant the engine file as it reads now", got)
	}
}

// The router's state is the engine's, not an entry's: its preset, its record and
// its logs live under the engine's own folder, and the entries' records are left
// alone (OVERVIEW ruling 2).
func TestTheRoutersStateLivesUnderItsEngine(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)

	record, _ := startRouter(t, manager, host, routerConfig(), 4242)

	engineRoot := manager.engineRoot(config.BackendRouter)
	for _, path := range []string{
		filepath.Join(engineRoot, presetFile),
		filepath.Join(engineRoot, recordFile),
		record.LogPath,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the router left nothing at %s: %v", path, err)
		}
	}
	if dir := filepath.Dir(record.LogPath); dir != filepath.Join(engineRoot, engineLogsDir) {
		t.Errorf("the router's log went to %s, want its own engine's log directory", dir)
	}

	// Nothing of the router's is filed among the entries' servers.
	if _, err := os.Stat(manager.recordsRoot()); !os.IsNotExist(err) {
		t.Errorf("the entries' records directory exists after a router start (%v); the router is not an entry", err)
	}

	// And a later invocation finds it again by reading that record back.
	server, found, err := manager.RouterServer()
	if err != nil || !found {
		t.Fatalf("reading the router's record back: found=%v err=%v", found, err)
	}
	if !server.Live {
		t.Error("the router reads as exited while its pid is still the process cria launched")
	}
	read := server.Record
	if !read.LaunchedAt.Equal(record.LaunchedAt) {
		t.Errorf("the record read back was launched at %s, want %s", read.LaunchedAt, record.LaunchedAt)
	}
	read.LaunchedAt, record.LaunchedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(read, record) {
		t.Errorf("the record read back is %+v, want the one the start wrote: %+v", read, record)
	}
}

// One router per host: a second start is refused while the first is running,
// naming the pid and port that hold it.
func TestASecondRouterIsRefused(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	startRouter(t, manager, host, routerConfig(), 4242)

	_, err := manager.StartRouter(routerConfig(), routerReport())
	if err == nil {
		t.Fatal("a second router was started while the first was running")
	}
	for _, want := range []string{"already running", "4242", "11434"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %v, want it to name %q", err, want)
		}
	}
}

// A llama-server that takes no router flags is refused before anything is
// spawned: the honest answer is what the binary lacks, not a server that dies on
// its first breath (docs/specs/TOOLS.md).
func TestARouterlessLlamaServerRefusesTheStart(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	_, err := manager.StartRouter(routerConfig(), usableReport())
	if err == nil {
		t.Fatal("the router started on a build that takes no router flags")
	}
	for _, want := range []string{tools.RouterFlag, "upgrade llama.cpp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %v, want it to name %q", err, want)
		}
	}
	if len(spawner.launches) != 0 {
		t.Errorf("cria spawned %v after refusing the start", spawner.launches)
	}
}

// A tree that declares no router port has no router, and the refusal names the
// file that would give it one — never default_port, which is the entries' own.
func TestARouterWithNoPortRefusesTheStart(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	router := routerConfig()
	router.Port = 0

	_, err := manager.StartRouter(router, routerReport())
	if err == nil {
		t.Fatal("the router started without a port")
	}
	for _, want := range []string{router.Path, "port"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %v, want it to name %q", err, want)
		}
	}
	if len(spawner.launches) != 0 {
		t.Errorf("cria spawned %v after refusing the start", spawner.launches)
	}
}

// Args that cannot be written as preset keys refuse the start before the host has
// changed: nothing is spawned, and no preset is left behind saying less than the
// file asked for.
func TestArgsThatCannotBecomeAPresetRefuseTheStart(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	router := routerConfig()
	router.Args = []string{"--override-kv", "a=int:1", "--override-kv", "b=int:2"}

	_, err := manager.StartRouter(router, routerReport())
	if err == nil {
		t.Fatal("the router started from args that cannot be written as preset keys")
	}
	if !strings.Contains(err.Error(), "--override-kv") {
		t.Errorf("the refusal reads %v, want it to name the flag", err)
	}
	if len(spawner.launches) != 0 {
		t.Errorf("cria spawned %v after refusing the start", spawner.launches)
	}
	if _, err := os.Stat(filepath.Join(manager.engineRoot(config.BackendRouter), presetFile)); !os.IsNotExist(err) {
		t.Errorf("a preset was written for a start cria refused (%v)", err)
	}
}

// A stop is the ordinary escalation, and it leaves the record gone. The preset
// stays: it is what the router that just stopped was serving from, and the next
// start writes it again.
func TestStoppingTheRouterRemovesItsRecord(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	record, _ := startRouter(t, manager, host, routerConfig(), 4242)

	if err := manager.StopRouter(record); err != nil {
		t.Fatalf("stopping the router: %v", err)
	}
	if want := []string{"TERM 4242"}; strings.Join(host.sent, ",") != strings.Join(want, ",") {
		t.Errorf("cria sent %v, want %v", host.sent, want)
	}
	if _, err := os.Stat(manager.routerRecordPath()); !os.IsNotExist(err) {
		t.Errorf("the router's record survived a stop (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(manager.engineRoot(config.BackendRouter), presetFile)); err != nil {
		t.Errorf("the preset the stopped router served from is gone: %v", err)
	}

	if _, found, err := manager.RouterServer(); found || err != nil {
		t.Errorf("a stopped router still has a record: found=%v err=%v", found, err)
	}
}

// The router's phase is read from its own health endpoint, and the answer means
// what it means for every server: green is running, red is starting until it has
// answered once and unhealthy after, and a pid that is no longer cria's is
// exited (docs/specs/SERVE.md).
func TestTheRoutersPhaseIsReadFromItsHealthEndpoint(t *testing.T) {
	host := &fakeHost{costs: map[int]procs.Stats{4242: {RSSBytes: 128, CPUPercent: 4}}}
	manager := newManager(t, host)
	record, _ := startRouter(t, manager, host, routerConfig(), 4242)

	var asked string
	green := false
	manager.probe = func(url string) Health {
		asked = url
		return Health{URL: url, Green: green, Status: 503, Detail: "503 Service Unavailable"}
	}

	// Not answering yet, and it never has: starting, not unhealthy.
	status, err := manager.RouterSnapshot(record)
	if err != nil {
		t.Fatalf("observing the router: %v", err)
	}
	if want := "http://127.0.0.1:11434/health"; asked != want {
		t.Errorf("cria probed %q, want %q", asked, want)
	}
	if status.Phase != PhaseStarting {
		t.Errorf("a router that has never answered is %q, want %q", status.Phase, PhaseStarting)
	}
	if status.Stats.RSSBytes != 128 {
		t.Errorf("the observation reports %+v, want what the process table said the pid costs", status.Stats)
	}

	// A router with no model loaded answers its health endpoint: that is running.
	green = true
	if status, err = manager.RouterSnapshot(record); err != nil {
		t.Fatalf("observing the router: %v", err)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("a router whose health endpoint answers is %q, want %q", status.Phase, PhaseRunning)
	}

	// It stops answering after it has answered: that is a server in trouble.
	green = false
	if status, err = manager.RouterSnapshot(record); err != nil {
		t.Fatalf("observing the router: %v", err)
	}
	if status.Phase != PhaseUnhealthy {
		t.Errorf("a router that stopped answering is %q, want %q", status.Phase, PhaseUnhealthy)
	}

	// And a pid that is no longer the process cria launched is the crash report.
	delete(host.alive, 4242)
	if status, err = manager.RouterSnapshot(record); err != nil {
		t.Fatalf("observing the router: %v", err)
	}
	if status.Phase != PhaseExited {
		t.Errorf("a router whose process is gone is %q, want %q", status.Phase, PhaseExited)
	}
	if status.Health != (Health{}) {
		t.Errorf("an exited router was probed: %+v", status.Health)
	}
}

// The router's logs are kept by count the way an entry's are — the newest three
// launches — and in its own directory, where no entry's logs can be pruned by it
// or prune it.
func TestTheRoutersLogsAreKeptToTheNewestThree(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	logsRoot := filepath.Join(manager.engineRoot(config.BackendRouter), engineLogsDir)
	if err := os.MkdirAll(logsRoot, 0o755); err != nil {
		t.Fatalf("creating %s: %v", logsRoot, err)
	}

	older := []string{
		"router-20260810-090000.log",
		"router-20260811-090000.log",
		"router-20260812-090000.log",
		"router-20260813-090000.log",
	}
	for _, name := range older {
		if err := os.WriteFile(filepath.Join(logsRoot, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	record, _ := startRouter(t, manager, host, routerConfig(), 4242)

	kept := names(t, logsRoot)
	want := []string{
		"router-20260812-090000.log",
		"router-20260813-090000.log",
		filepath.Base(record.LogPath),
	}
	slices.Sort(want)
	if !slices.Equal(kept, want) {
		t.Errorf("the router's log directory holds\n  %v\nwant\n  %v", kept, want)
	}
}

// names lists what a directory holds, in the order the filesystem reports it.
func names(t *testing.T, dir string) []string {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var found []string
	for _, file := range files {
		found = append(found, file.Name())
	}
	return found
}

// readFile is a file's whole content, for the byte comparisons the composed
// preset is held to.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
