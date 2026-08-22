package serve

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/tools"
)

// The liveness rule, in every combination that can reach it: the pid has to
// exist and still be the process cria recorded (docs/specs/SERVE.md).
func TestLiveness(t *testing.T) {
	recorded := procs.Identity{
		Command:   "/opt/homebrew/bin/llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0 --port 8080",
		StartedAt: "Tue Aug 18 14:57:30 2026",
	}
	record := Record{EntryID: "qwen", PID: 4242, Identity: recorded}

	cases := []struct {
		name string
		host *fakeHost
		want bool
	}{
		{
			name: "the recorded process is still there",
			host: &fakeHost{alive: map[int]procs.Identity{4242: recorded}},
			want: true,
		},
		{
			name: "the pid is gone",
			host: &fakeHost{},
			want: false,
		},
		{
			name: "the pid was handed to something else",
			host: &fakeHost{alive: map[int]procs.Identity{4242: {Command: "/usr/bin/vim notes.txt", StartedAt: "Wed Aug 19 09:00:00 2026"}}},
			want: false,
		},
		{
			name: "the same command line, started again at another moment",
			host: &fakeHost{alive: map[int]procs.Identity{4242: {Command: recorded.Command, StartedAt: "Wed Aug 19 09:00:00 2026"}}},
			want: false,
		},
		{
			name: "an exited child the operating system has not reaped yet",
			host: &fakeHost{alive: map[int]procs.Identity{4242: {Command: "<defunct>", StartedAt: recorded.StartedAt}}},
			want: false,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manager := newManager(t, test.host)
			live, err := manager.Live(record)
			if err != nil {
				t.Fatalf("judging liveness: %v", err)
			}
			if live != test.want {
				t.Errorf("live=%v, want %v", live, test.want)
			}
		})
	}
}

// A process table cria cannot read fails the listing. "Everything stopped" is a
// plausible-looking answer and it would be a lie (CODING-RULES §4).
func TestAnUnreadableProcessTableFailsTheListing(t *testing.T) {
	manager := newManager(t, &fakeHost{failWith: errors.New("ps did not answer within 3s")})
	writeRecordFile(t, manager, "qwen", validRecord)

	if _, err := manager.List(); err == nil {
		t.Fatal("a listing succeeded while the process table could not be read")
	}
}

// A start is (entry, selection): the picked options reach the command line, the
// effective model is what the picks resolved to, and the record says which
// combination is running (docs/specs/SERVE.md).
func TestAStartComposesAndRecordsItsPicks(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	entry := choicesEntry()
	selection := config.Selection{"quant": "q6", "context": "long"}

	record, spawner := startPicked(t, manager, host, entry, selection, 4242)

	want := []string{
		"/opt/homebrew/bin/llama-server",
		"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL",
		"--host", "0.0.0.0",
		"--port", "8080",
		"--ctx-size", "16384",
		"--n-cpu-moe", "12",
		"--cache-type-k", "f16",
	}
	if !slices.Equal(spawner.last().Command, want) {
		t.Errorf("the launch ran\n  %v\nwant\n  %v", spawner.last().Command, want)
	}
	if !slices.Equal(record.Command, want) {
		t.Errorf("the record holds\n  %v\nwant the argv that was spawned", record.Command)
	}
	if record.Quant != "UD-Q6_K_XL" || record.Repo != entry.Repo {
		t.Errorf("the record serves %s:%s, want the picked quantization", record.Repo, record.Quant)
	}
	if !maps.Equal(record.Selection, selection) {
		t.Errorf("the record's picks are %v, want %v", record.Selection, selection)
	}

	// And they survive the round trip through the state file, which is where a
	// later invocation reads the combination from.
	loaded, found, err := manager.loadRecord(entry.ID)
	if err != nil || !found {
		t.Fatalf("loading the record: found=%v, err=%v", found, err)
	}
	if !maps.Equal(loaded.Selection, selection) {
		t.Errorf("the record came back with the picks %v, want %v", loaded.Selection, selection)
	}
}

// A flat entry picks nothing, and its record says nothing: the field is absent
// from the file, and what comes back is a record with no selection at all.
func TestAFlatEntryRecordsNoSelection(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	if record.Selection != nil {
		t.Errorf("a flat entry recorded the picks %v", record.Selection)
	}
	written, err := os.ReadFile(manager.recordPath("qwen"))
	if err != nil {
		t.Fatalf("reading the record file: %v", err)
	}
	if strings.Contains(string(written), "selection") {
		t.Errorf("a flat entry's record file carries a selection key:\n%s", written)
	}
}

// A pick that names nothing is refused with entry validation, before the tool
// gate (docs/specs/SERVE.md, Start 1): the answer names the choice and the
// options it has, not the tool this host is missing.
func TestAnUnresolvableSelectionRefusesBeforeTheToolGate(t *testing.T) {
	unusable := tools.Report{LlamaServer: tools.Tool{
		Name: tools.LlamaServer, Status: tools.StatusMissing,
		Disables: "starting llama entries; they stay listed, marked unstartable",
		Fix:      "install llama.cpp so llama-server is on PATH",
	}}

	cases := map[string]struct {
		entry     config.Entry
		selection config.Selection
		want      []string
	}{
		"an option that does not exist": {
			entry:     choicesEntry(),
			selection: config.Selection{"quant": "q2", "context": "long"},
			want:      []string{`choice "quant" has no option named "q2"`, "q4, q6"},
		},
		"a choice that does not exist": {
			entry:     choicesEntry(),
			selection: config.Selection{"qunt": "q6", "context": "long"},
			want:      []string{`no choice named "qunt"`, "quant, context"},
		},
		"a choice nobody picked": {
			entry:     choicesEntry(),
			selection: config.Selection{"quant": "q6"},
			want:      []string{`nothing picked for choice "context"`, "short, long"},
		},
		"picks against an entry that has no axes": {
			entry:     llamaEntry(),
			selection: config.Selection{"quant": "q6"},
			want:      []string{"has no choices"},
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			manager := newManager(t, &fakeHost{})
			spawner := &fakeSpawner{pid: 4242}
			manager.spawn = spawner.launch

			_, err := manager.Start(test.entry, test.selection, unusable)
			if err == nil {
				t.Fatal("a start resolved a selection the entry does not have")
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "llama-server") {
				t.Errorf("the tool gate answered before the picks were resolved: %v", err)
			}
			if len(spawner.launches) != 0 {
				t.Errorf("the refused start spawned %d processes", len(spawner.launches))
			}
			if _, found, err := manager.loadRecord(test.entry.ID); found || err != nil {
				t.Errorf("the refused start left a record: found=%v, err=%v", found, err)
			}
		})
	}
}

// An entry runs once at a time (docs/specs/SERVE.md): its live record refuses
// the second start, and nothing is spawned.
func TestAnEntryRunsOnceAtATime(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	entry := llamaEntry()
	first, _ := startOne(t, manager, host, entry, 4242)

	spawner := &fakeSpawner{pid: 5150}
	manager.spawn = spawner.launch
	_, err := manager.Start(entry, nil, usableReport())
	if err == nil {
		t.Fatal("an entry started twice")
	}
	if !strings.Contains(err.Error(), "already running") || !strings.Contains(err.Error(), "4242") {
		t.Errorf("the refusal is %v, want it to name the running pid", err)
	}
	if len(spawner.launches) != 0 {
		t.Errorf("the refused start spawned %d processes", len(spawner.launches))
	}

	loaded, _, err := manager.loadRecord(entry.ID)
	if err != nil {
		t.Fatalf("loading the record: %v", err)
	}
	if loaded.PID != first.PID {
		t.Errorf("the record now names pid %d, want the running %d", loaded.PID, first.PID)
	}
}

// A record whose process is gone is the entry's crash report, and the entry's
// next start replaces it (docs/specs/SERVE.md).
func TestARestartReplacesADeadRecord(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	entry := llamaEntry()
	startOne(t, manager, host, entry, 4242)

	// The server crashed: its pid is no longer on the host.
	delete(host.alive, 4242)
	listing, err := manager.List()
	if err != nil {
		t.Fatalf("listing after the crash: %v", err)
	}
	if len(listing.Servers) != 1 || listing.Servers[0].Live {
		t.Fatalf("after a crash the listing holds %+v, want one exited server", listing.Servers)
	}
	if names := records(t, manager); !slices.Equal(names, []string{"qwen.json"}) {
		t.Fatalf("a crash left %v, want the record kept as the crash report", names)
	}

	second, _ := startOne(t, manager, host, entry, 5150)
	if second.PID != 5150 {
		t.Errorf("the restart recorded pid %d", second.PID)
	}
	if names := records(t, manager); !slices.Equal(names, []string{"qwen.json"}) {
		t.Fatalf("the restart left %v, want one record for the entry", names)
	}
	loaded, _, err := manager.loadRecord(entry.ID)
	if err != nil {
		t.Fatalf("loading the record: %v", err)
	}
	if loaded.PID != 5150 {
		t.Errorf("the record still names pid %d after a restart", loaded.PID)
	}
}

// A record cria cannot read stops a start: it names a pid cria launched, and a
// second server on that entry's port is exactly what the once-at-a-time rule
// prevents.
func TestABrokenRecordRefusesAStart(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	writeRecordFile(t, manager, "qwen", "{")
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	_, err := manager.Start(llamaEntry(), nil, usableReport())
	if err == nil {
		t.Fatal("an entry with an unreadable record started")
	}
	if !strings.Contains(err.Error(), "delete the record file") {
		t.Errorf("the refusal is %v, want it to name the manual fix", err)
	}
	if len(spawner.launches) != 0 {
		t.Errorf("the refused start spawned %d processes", len(spawner.launches))
	}
}

// Everything a server prints goes to that launch's log file, raw
// (docs/specs/SERVE.md).
func TestTheLaunchWritesToItsOwnLog(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	spawner := &fakeSpawner{pid: 4242, output: "llama-server: loading model\n"}
	manager.spawn = spawner.launch
	host.alive = map[int]procs.Identity{}

	entry := llamaEntry()
	command := composedFor(t, entry, nil)
	host.alive[4242] = procs.Identity{Command: strings.Join(command, " "), StartedAt: "Tue Aug 18 14:57:30 2026"}

	record, err := manager.Start(entry, nil, usableReport())
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	content, err := os.ReadFile(record.LogPath)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if string(content) != spawner.output {
		t.Errorf("the log holds %q, want %q", content, spawner.output)
	}
	if base := filepath.Base(record.LogPath); !strings.HasPrefix(base, entry.ID+"-") || !strings.HasSuffix(base, ".log") {
		t.Errorf("the log is named %q, want <entry>-<timestamp>.log", base)
	}
}

// Logs are kept by count: the newest three launches of an entry, pruned at each
// new one (docs/specs/SERVE.md). Another entry's logs are not this entry's to
// prune, and neither is a file that carries no launch stamp.
func TestLogsArePrunedToTheNewestThree(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	if err := os.MkdirAll(manager.logsRoot(), 0o755); err != nil {
		t.Fatalf("creating %s: %v", manager.logsRoot(), err)
	}

	older := []string{
		"qwen-20260810-090000.log",
		"qwen-20260811-090000.log",
		"qwen-20260812-090000.log",
		"qwen-20260813-090000.log",
		"qwen-20260814-090000.log",
	}
	// Not this entry's: one belongs to an entry whose id starts the same way, one
	// is a hand-written note.
	untouched := []string{"qwen-mlx-20260810-090000.log", "qwen-notes.txt"}
	for _, name := range append(slices.Clone(older), untouched...) {
		if err := os.WriteFile(filepath.Join(manager.logsRoot(), name), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	kept := logs(t, manager)
	want := []string{
		"qwen-20260813-090000.log",
		"qwen-20260814-090000.log",
		filepath.Base(record.LogPath),
		"qwen-mlx-20260810-090000.log",
		"qwen-notes.txt",
	}
	slices.Sort(want)
	if !slices.Equal(kept, want) {
		t.Errorf("the log directory holds\n  %v\nwant\n  %v", kept, want)
	}
}

// A spawn that never happened leaves nothing behind: no record, and no empty log
// file to push a real crash log out of the three that are kept.
func TestAFailedSpawnLeavesNothingBehind(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	spawner := &fakeSpawner{failWith: errors.New("fork/exec: permission denied")}
	manager.spawn = spawner.launch

	if _, err := manager.Start(llamaEntry(), nil, usableReport()); err == nil {
		t.Fatal("a start reported success after the spawn failed")
	}
	if _, found, err := manager.loadRecord("qwen"); found || err != nil {
		t.Errorf("a failed start left a record: found=%v, err=%v", found, err)
	}
	if names := logs(t, manager); len(names) != 0 {
		t.Errorf("a failed start left the logs %v", names)
	}
}

// A server that failed on its first breath has no identity to record — the pid
// is gone, or it is an unreaped "<defunct>" row that is no longer the program
// cria launched. Either way the record reads as exited, with its log as the
// crash report.
func TestAServerThatDiedImmediatelyRecordsNoIdentity(t *testing.T) {
	cases := []struct {
		name string
		host *fakeHost
	}{
		{name: "the pid is already gone", host: &fakeHost{}},
		{
			name: "the pid is an unreaped child",
			host: &fakeHost{alive: map[int]procs.Identity{4242: {Command: "<defunct>", StartedAt: "Tue Aug 18 14:57:30 2026"}}},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manager := newManager(t, test.host)
			spawner := &fakeSpawner{pid: 4242, output: "error: unknown argument\n"}
			manager.spawn = spawner.launch

			record, err := manager.Start(llamaEntry(), nil, usableReport())
			if err != nil {
				t.Fatalf("starting: %v", err)
			}
			if record.Identity != (procs.Identity{}) {
				t.Errorf("the record carries the identity %+v of a process that is not the server", record.Identity)
			}
			listing, err := manager.List()
			if err != nil {
				t.Fatalf("listing: %v", err)
			}
			if len(listing.Servers) != 1 || listing.Servers[0].Live {
				t.Fatalf("the listing holds %+v, want one exited server", listing.Servers)
			}
			if listing.Servers[0].LogPath != record.LogPath {
				t.Errorf("the crash report points at %s, want %s", listing.Servers[0].LogPath, record.LogPath)
			}
		})
	}
}

// A shimmed server does not name its program in argv straight away: a uv-shimmed
// mlx_lm.server re-execs framework Python through Python.app within tens of
// milliseconds of the spawn, and the argv caught in between names neither
// program. The capture keeps looking until the pid names what cria launched —
// recording that intermediate argv writes a record matching nothing, which makes
// a healthy server read exited forever with no stop able to act on it.
func TestIdentityCaptureWaitsForTheProgramToSettle(t *testing.T) {
	host := &fakeHost{
		interim:     map[int]procs.Identity{4242: identityOf("/opt/homebrew/opt/python@3.13/bin/python3.13")},
		settleAfter: map[int]int{4242: 2},
	}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)
	looks := host.identifies[4242]

	if looks != 3 {
		t.Errorf("the capture took %d looks at the process table, want the 3 the pid needed to settle", looks)
	}
	if record.Identity != host.alive[4242] {
		t.Errorf("the record carries %+v, want the settled identity %+v", record.Identity, host.alive[4242])
	}
	live, err := manager.Live(record)
	if err != nil {
		t.Fatalf("judging liveness: %v", err)
	}
	if !live {
		t.Error("the started server reads as exited, which is the bug this capture exists to prevent")
	}
}

// A pid that names the program on the first look is not asked twice: the wait is
// for a process that has not settled, not a cost every start pays.
func TestIdentityCaptureAsksOnceWhenTheFirstAnswerNamesTheProgram(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	startOne(t, manager, host, llamaEntry(), 4242)

	if looks := host.identifies[4242]; looks != 1 {
		t.Errorf("the capture took %d looks at the process table, want 1", looks)
	}
}

// A pid that goes while cria is still waiting for it to settle ends the wait
// there: the server failed on its first breath, and the record takes no identity
// — which is the truth about it.
func TestIdentityCaptureStopsWhenThePIDIsGone(t *testing.T) {
	host := &fakeHost{
		interim:     map[int]procs.Identity{4242: identityOf("/opt/homebrew/opt/python@3.13/bin/python3.13")},
		settleAfter: map[int]int{4242: 2},
	}
	manager := newManager(t, host)
	spawner := &fakeSpawner{pid: 4242, output: "error: unknown argument\n"}
	manager.spawn = spawner.launch

	record, err := manager.Start(llamaEntry(), nil, usableReport())
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	if record.Identity != (procs.Identity{}) {
		t.Errorf("the record carries the identity %+v of a process that is not the server", record.Identity)
	}
	if looks := host.identifies[4242]; looks != 3 {
		t.Errorf("the capture took %d looks, want it to stop at the 3rd, where the pid was gone", looks)
	}
}

// A process table that fails right after the spawn is a broken host, and it is
// reported as one — with the pid and the log of the server that is now running
// unidentified.
func TestAnUnreadableProcessTableIsReportedAfterTheSpawn(t *testing.T) {
	manager := newManager(t, &fakeHost{failWith: errors.New("ps did not answer within 3s")})
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	record, err := manager.Start(llamaEntry(), nil, usableReport())
	if err == nil {
		t.Fatal("a start whose pid could not be identified reported success")
	}
	if !strings.Contains(err.Error(), "4242") || !strings.Contains(err.Error(), record.LogPath) {
		t.Errorf("the error is %v, want it to name the pid and the log", err)
	}
	if _, found, err := manager.loadRecord("qwen"); !found || err != nil {
		t.Errorf("the started server was not recorded: found=%v, err=%v", found, err)
	}
}

// The launch time is the record's own, and the log file is named after it.
func TestTheRecordCarriesItsLaunchTime(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	before := time.Now().Truncate(time.Second)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	if record.LaunchedAt.Before(before) || record.LaunchedAt.After(time.Now()) {
		t.Errorf("the launch time is %s, outside the moment of the launch", record.LaunchedAt)
	}
	at, ok := launchStamp(filepath.Base(record.LogPath), "qwen")
	if !ok {
		t.Fatalf("the log name %s carries no launch stamp", record.LogPath)
	}
	if !at.Equal(record.LaunchedAt.Truncate(time.Second)) {
		t.Errorf("the log is stamped %s, want the launch time %s", at, record.LaunchedAt)
	}
}
