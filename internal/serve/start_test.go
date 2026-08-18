package serve

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/procs"
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

// An entry runs once at a time (docs/specs/SERVE.md): its live record refuses
// the second start, and nothing is spawned.
func TestAnEntryRunsOnceAtATime(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	entry := llamaEntry()
	first, _ := startOne(t, manager, host, entry, 4242)

	spawner := &fakeSpawner{pid: 5150}
	manager.spawn = spawner.launch
	_, err := manager.Start(entry, usableReport())
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

	_, err := manager.Start(llamaEntry(), usableReport())
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
	command, err := ComposedCommand(entry, usableReport())
	if err != nil {
		t.Fatalf("composing: %v", err)
	}
	host.alive[4242] = procs.Identity{Command: strings.Join(command, " "), StartedAt: "Tue Aug 18 14:57:30 2026"}

	record, err := manager.Start(entry, usableReport())
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

	if _, err := manager.Start(llamaEntry(), usableReport()); err == nil {
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

			record, err := manager.Start(llamaEntry(), usableReport())
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

// A process table that fails right after the spawn is a broken host, and it is
// reported as one — with the pid and the log of the server that is now running
// unidentified.
func TestAnUnreadableProcessTableIsReportedAfterTheSpawn(t *testing.T) {
	manager := newManager(t, &fakeHost{failWith: errors.New("ps did not answer within 3s")})
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	record, err := manager.Start(llamaEntry(), usableReport())
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
