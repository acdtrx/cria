package serve

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/procs"
)

// validRecord is what cria writes: every field of the contract
// docs/specs/SERVE.md names, spelled the way the file spells it. The strict
// tests below mutate one thing about it at a time.
const validRecord = `{
  "entry_id": "qwen",
  "backend": "llama",
  "repo": "unsloth/Qwen3-30B-A3B-GGUF",
  "quant": "UD-Q4_K_XL",
  "host": "0.0.0.0",
  "port": 8080,
  "pid": 4242,
  "identity": {
    "command": "/opt/homebrew/bin/llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0 --port 8080",
    "started_at": "Tue Aug 18 14:57:30 2026"
  },
  "command": ["/opt/homebrew/bin/llama-server", "-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL", "--host", "0.0.0.0", "--port", "8080"],
  "log_path": "/home/u/.local/state/cria/logs/qwen-20260818-145730.log",
  "launched_at": "2026-08-18T14:57:30.123456+02:00"
}`

// A start writes down everything a later cria invocation needs, and reading it
// back gives the same server.
func TestRecordRoundTrip(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	entry := llamaEntry()
	written, _ := startOne(t, manager, host, entry, 4242)

	loaded, found, err := manager.loadRecord(entry.ID)
	if err != nil || !found {
		t.Fatalf("loading the record of %s: found=%v, err=%v", entry.ID, found, err)
	}

	if !loaded.LaunchedAt.Equal(written.LaunchedAt) {
		t.Errorf("launch time came back %s, want %s", loaded.LaunchedAt, written.LaunchedAt)
	}
	// The launch time is the only field a comparison has to be careful with: it
	// loses its monotonic reading on the way through JSON, which is precisely
	// what Equal knows and == does not.
	loaded.LaunchedAt = written.LaunchedAt
	if !reflect.DeepEqual(loaded, written) {
		t.Errorf("the record came back as\n  %+v\nwant\n  %+v", loaded, written)
	}

	if written.EntryID != entry.ID || written.Backend != entry.Backend || written.Repo != entry.Repo ||
		written.Quant != entry.Quant || written.Host != entry.Host || written.Port != entry.Port {
		t.Errorf("the record does not describe the entry it started: %+v", written)
	}
	if written.PID != 4242 || written.Identity == (procs.Identity{}) {
		t.Errorf("the record does not identify the process it started: %+v", written)
	}
	if filepath.Dir(written.LogPath) != manager.logsRoot() {
		t.Errorf("the log path is %s, want it under %s", written.LogPath, manager.logsRoot())
	}
}

// Records are cria's own format and are validated loudly on read: an unknown
// key, a wrong type or a missing field is an error, never a silent default
// (CLAUDE.md, feature-building mode). A record cria refuses is reported, not
// skipped — it names a pid cria started.
func TestRecordsAreValidatedLoudly(t *testing.T) {
	unknownKey := strings.Replace(validRecord, `"port": 8080,`,
		`"port": 8080,`+"\n  "+`"pid_started_at": "Tue Aug 18 14:57:30 2026",`, 1)

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "an unknown key",
			content: unknownKey,
			want:    "pid_started_at",
		},
		{
			name:    "a wrong type",
			content: strings.Replace(validRecord, `"port": 8080`, `"port": "8080"`, 1),
			want:    "port",
		},
		{
			name:    "a missing field",
			content: strings.Replace(validRecord, `"repo": "unsloth/Qwen3-30B-A3B-GGUF",`, "", 1),
			want:    "repo is missing",
		},
		{
			name:    "a record naming another entry",
			content: strings.Replace(validRecord, `"entry_id": "qwen"`, `"entry_id": "llama"`, 1),
			want:    "named after entry",
		},
		{
			name:    "a backend cria cannot launch",
			content: strings.Replace(validRecord, `"backend": "llama"`, `"backend": "vllm"`, 1),
			want:    "backend",
		},
		{
			name:    "a quantization on a backend that takes none",
			content: strings.Replace(validRecord, `"backend": "llama"`, `"backend": "mlx"`, 1),
			want:    "quant",
		},
		{
			name:    "a pid that is not a process",
			content: strings.Replace(validRecord, `"pid": 4242`, `"pid": 0`, 1),
			want:    "pid",
		},
		{
			name:    "a port nothing can bind",
			content: strings.Replace(validRecord, `"port": 8080`, `"port": 70000`, 1),
			want:    "port",
		},
		{
			name:    "no launch time",
			content: strings.Replace(validRecord, `"launched_at": "2026-08-18T14:57:30.123456+02:00"`, `"launched_at": "0001-01-01T00:00:00Z"`, 1),
			want:    "launched_at is missing",
		},
		{
			name:    "more than one document",
			content: validRecord + "\n" + validRecord,
			want:    "more than one JSON document",
		},
		{
			name:    "not JSON at all",
			content: "entry_id = \"qwen\"\n",
			want:    "invalid character",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manager := newManager(t, &fakeHost{})
			writeRecordFile(t, manager, "qwen", test.content)

			listing, err := manager.List()
			if err != nil {
				t.Fatalf("listing: %v", err)
			}
			if len(listing.Servers) != 0 {
				t.Fatalf("a refused record was acted on: %+v", listing.Servers)
			}
			if len(listing.Broken) != 1 {
				t.Fatalf("the listing reported %d broken records, want 1", len(listing.Broken))
			}
			broken := listing.Broken[0]
			if broken.EntryID != "qwen" || broken.Path != manager.recordPath("qwen") {
				t.Errorf("the broken record is reported as %s at %s", broken.EntryID, broken.Path)
			}
			if !strings.Contains(broken.Err.Error(), test.want) {
				t.Errorf("the reason is %v, want it to name %q", broken.Err, test.want)
			}
		})
	}
}

// A valid record is read whatever else the directory holds, and a broken one
// never hides its neighbours.
func TestListingKeepsTheGoodRecords(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	writeRecordFile(t, manager, "qwen", validRecord)
	writeRecordFile(t, manager, "broken", "{")
	// Not a record: the directory is cria's, but it is still a directory.
	if err := os.WriteFile(filepath.Join(manager.recordsRoot(), "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing a stray file: %v", err)
	}

	listing, err := manager.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listing.Servers) != 1 || listing.Servers[0].EntryID != "qwen" {
		t.Fatalf("the listing holds %+v, want the one valid record", listing.Servers)
	}
	if len(listing.Broken) != 1 || listing.Broken[0].EntryID != "broken" {
		t.Fatalf("the listing reports %+v as broken, want the one broken record", listing.Broken)
	}
}

// A record of an entry with axes carries the picks it was composed from, and the
// strict read takes them back exactly as written (docs/specs/SERVE.md).
func TestARecordCarriesThePicksItWasComposedFrom(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	entry := choicesEntry()
	selection := config.Selection{"quant": "q6", "context": "short"}
	written, _ := startPicked(t, manager, host, entry, selection, 4242)

	loaded, found, err := manager.loadRecord(entry.ID)
	if err != nil || !found {
		t.Fatalf("loading the record of %s: found=%v, err=%v", entry.ID, found, err)
	}
	loaded.LaunchedAt = written.LaunchedAt
	if !reflect.DeepEqual(loaded, written) {
		t.Errorf("the record came back as\n  %+v\nwant\n  %+v", loaded, written)
	}

	// The picks a record holds are the launch's, not the entry's current
	// defaults: a picker that moves on afterwards never rewrites what is running.
	if !maps.Equal(loaded.Selection, selection) {
		t.Errorf("the record's picks are %v, want %v", loaded.Selection, selection)
	}
}

// The selection is read off the record file itself, both ways: a file that
// carries picks hands them over, and a file without the key reads as a flat
// entry's rather than as a broken record — records are transient, and nothing
// migrates them (CLAUDE.md, feature-building mode).
func TestASelectionIsReadOffTheRecordFile(t *testing.T) {
	withPicks := strings.Replace(validRecord, `"quant": "UD-Q4_K_XL",`,
		`"quant": "UD-Q4_K_XL",`+"\n  "+`"selection": {"quant": "q4", "context": "long"},`, 1)

	cases := map[string]struct {
		content string
		want    config.Selection
	}{
		"a record that names its picks":  {content: withPicks, want: config.Selection{"quant": "q4", "context": "long"}},
		"a record with no selection key": {content: validRecord, want: nil},
		"a record whose selection is {}": {content: strings.Replace(validRecord, `"quant": "UD-Q4_K_XL",`, `"quant": "UD-Q4_K_XL",`+"\n  "+`"selection": {},`, 1), want: config.Selection{}},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			manager := newManager(t, &fakeHost{})
			writeRecordFile(t, manager, "qwen", test.content)

			listing, err := manager.List()
			if err != nil {
				t.Fatalf("listing: %v", err)
			}
			if len(listing.Broken) != 0 {
				t.Fatalf("the record was refused: %v", listing.Broken[0].Err)
			}
			if len(listing.Servers) != 1 {
				t.Fatalf("the listing holds %+v, want the one record", listing.Servers)
			}
			if !maps.Equal(listing.Servers[0].Selection, test.want) {
				t.Errorf("the record's picks read as %v, want %v", listing.Servers[0].Selection, test.want)
			}
		})
	}
}

// A record whose process was gone before cria could look at it carries no
// identity. That is a legitimate record — it reads as exited, which is the truth
// about it — so the strict read must accept it.
func TestARecordWithoutAnIdentityReadsAsExited(t *testing.T) {
	manager := newManager(t, &fakeHost{alive: map[int]procs.Identity{
		4242: {Command: "something else entirely", StartedAt: "Tue Aug 18 14:57:30 2026"},
	}})
	writeRecordFile(t, manager, "qwen", strings.Replace(validRecord, `"identity": {
    "command": "/opt/homebrew/bin/llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0 --port 8080",
    "started_at": "Tue Aug 18 14:57:30 2026"
  },`, "", 1))

	listing, err := manager.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listing.Broken) != 0 {
		t.Fatalf("a record without an identity was refused: %v", listing.Broken[0].Err)
	}
	if len(listing.Servers) != 1 || listing.Servers[0].Live {
		t.Fatalf("a record without an identity reads as %+v, want one exited server", listing.Servers)
	}
}

// A state root nothing has been started under holds no servers, which is a fresh
// host rather than a failure.
func TestListingAFreshHost(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	listing, err := manager.List()
	if err != nil {
		t.Fatalf("listing a state root that does not exist: %v", err)
	}
	if len(listing.Servers) != 0 || len(listing.Broken) != 0 {
		t.Errorf("a fresh host reported %+v", listing)
	}
}

// The record shape is the same for both backends; an mlx server records no
// quantization because its repo is one.
func TestMLXRecord(t *testing.T) {
	entry := config.Entry{
		ID: "qwen-mlx", Backend: config.BackendMLX,
		Repo: "mlx-community/Qwen3-30B-A3B-4bit", Host: "0.0.0.0", Port: 8080,
	}
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, entry, 5150)

	if record.Quant != "" {
		t.Errorf("an mlx record carries a quantization: %q", record.Quant)
	}
	loaded, found, err := manager.loadRecord(entry.ID)
	if err != nil || !found {
		t.Fatalf("loading the record of %s: found=%v, err=%v", entry.ID, found, err)
	}
	if loaded.Backend != config.BackendMLX || loaded.Repo != entry.Repo {
		t.Errorf("the record came back as %+v", loaded)
	}
}

// writeRecordFile puts a record file on disk exactly as given, without going
// through the writer — these tests are about what the reader accepts.
func writeRecordFile(t *testing.T, manager *Manager, entryID, content string) {
	t.Helper()
	if err := os.MkdirAll(manager.recordsRoot(), 0o755); err != nil {
		t.Fatalf("creating %s: %v", manager.recordsRoot(), err)
	}
	if err := os.WriteFile(manager.recordPath(entryID), []byte(content), 0o644); err != nil {
		t.Fatalf("writing the record of %s: %v", entryID, err)
	}
}
