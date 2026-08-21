package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cria/internal/config"
)

// What the UI remembers survives the program: a change is written, and the next
// launch reads it back.
func TestPrefsRoundTrip(t *testing.T) {
	root := t.TempDir()
	saved := prefs{Backend: config.BackendMLX, LastStarted: "qwen"}

	if err := savePrefs(root, saved); err != nil {
		t.Fatalf("saving the preferences: %v", err)
	}
	loaded, err := loadPrefs(root)
	if err != nil {
		t.Fatalf("loading the preferences: %v", err)
	}
	if !reflect.DeepEqual(loaded, saved) {
		t.Errorf("the preferences came back as %+v, want %+v", loaded, saved)
	}
}

// Groups are remembered like the rest of the preferences, in the order the user
// put them in — that order is what the list renders — and a group standing empty
// comes back empty rather than disappearing.
func TestPrefsGroupsRoundTrip(t *testing.T) {
	root := t.TempDir()
	saved := prefs{
		Backend:     config.BackendMLX,
		LastStarted: "qwen",
		Groups: []entryGroup{
			{Name: "daily", Entries: []string{"qwen", "gemma"}},
			{Name: "macmini candidates", Entries: []string{}},
		},
	}

	if err := savePrefs(root, saved); err != nil {
		t.Fatalf("saving the preferences: %v", err)
	}
	loaded, err := loadPrefs(root)
	if err != nil {
		t.Fatalf("loading the preferences: %v", err)
	}
	if !reflect.DeepEqual(loaded, saved) {
		t.Errorf("the preferences came back as %+v, want %+v", loaded, saved)
	}
}

// A preferences value with no groups writes the file it wrote before groups
// existed: the key is absent, not an empty array, so nothing about an unused
// feature shows up in the file.
func TestPrefsWithoutGroupsOmitTheKey(t *testing.T) {
	root := t.TempDir()
	if err := savePrefs(root, prefs{Backend: config.BackendMLX, LastStarted: "qwen"}); err != nil {
		t.Fatalf("saving the preferences: %v", err)
	}

	data, err := os.ReadFile(prefsPath(root))
	if err != nil {
		t.Fatalf("reading the preferences file: %v", err)
	}
	want := "{\n  \"backend\": \"mlx\",\n  \"last_started\": \"qwen\"\n}\n"
	if string(data) != want {
		t.Errorf("the file reads %q, want %q", data, want)
	}
}

// The file is written where docs/specs/TUI.md puts it: the state directory, not
// the config tree.
func TestPrefsAreWrittenToTheStateDirectory(t *testing.T) {
	root := t.TempDir()
	if err := savePrefs(root, defaultPrefs()); err != nil {
		t.Fatalf("saving the preferences: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "ui.json")); err != nil {
		t.Errorf("ui.json is not in the state root: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the state root: %v", err)
	}
	// The temporary file the atomic write goes through must not survive it.
	for _, entry := range entries {
		if entry.Name() != "ui.json" {
			t.Errorf("the state root also holds %q", entry.Name())
		}
	}
}

// A first launch has no file, and that is not a problem to report: the defaults
// are the whole answer.
func TestMissingPrefsAreDefaults(t *testing.T) {
	loaded, err := loadPrefs(t.TempDir())
	if err != nil {
		t.Errorf("a missing preferences file reported %v, want no error", err)
	}
	if !reflect.DeepEqual(loaded, defaultPrefs()) {
		t.Errorf("a missing preferences file gave %+v, want %+v", loaded, defaultPrefs())
	}
}

// A preferences file cria cannot read is reported and reset: it is machine-owned
// state, so a broken one is never worth refusing to start over, and every way it
// can be broken says so out loud rather than defaulting silently.
func TestBrokenPrefsResetLoudly(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		contains string
	}{
		{name: "not JSON at all", file: "backend = llama\n", contains: "unreadable"},
		{name: "a key cria does not know", file: `{"backend":"llama","theme":"dark"}`, contains: "theme"},
		{name: "a field of the wrong type", file: `{"backend":42}`, contains: "unreadable"},
		{name: "a backend cria cannot launch", file: `{"backend":"vllm"}`, contains: `backend is "vllm"`},
		{name: "two documents in one file", file: `{"backend":"llama"}{"backend":"mlx"}`, contains: "more than one JSON document"},
		{name: "a group with no name", file: `{"backend":"llama","groups":[{"name":"","entries":[]}]}`, contains: "no name"},
		{name: "two groups of the same name", file: `{"backend":"llama","groups":[{"name":"daily","entries":[]},{"name":"daily","entries":[]}]}`, contains: `two groups are named "daily"`},
		{name: "an entry filed in two groups", file: `{"backend":"llama","groups":[{"name":"daily","entries":["qwen"]},{"name":"tests","entries":["qwen"]}]}`, contains: `entry "qwen" is in both`},
		{name: "an entry filed twice in one group", file: `{"backend":"llama","groups":[{"name":"daily","entries":["qwen","qwen"]}]}`, contains: `holds entry "qwen" twice`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(prefsPath(root), []byte(test.file), 0o644); err != nil {
				t.Fatalf("writing the broken preferences file: %v", err)
			}

			loaded, err := loadPrefs(root)
			if err == nil {
				t.Fatal("a broken preferences file was accepted silently")
			}
			if !strings.Contains(err.Error(), test.contains) {
				t.Errorf("the failure reads %q, want it to name %q", err, test.contains)
			}
			if !reflect.DeepEqual(loaded, defaultPrefs()) {
				t.Errorf("a broken preferences file gave %+v, want the defaults %+v", loaded, defaultPrefs())
			}
		})
	}
}

// The toggle is a toggle: neither backend is a dead end.
func TestBackendToggleAlternates(t *testing.T) {
	llama := prefs{Backend: config.BackendLlama}
	if llama.other() != config.BackendMLX {
		t.Errorf("llama toggles to %q, want %q", llama.other(), config.BackendMLX)
	}
	mlx := prefs{Backend: config.BackendMLX}
	if mlx.other() != config.BackendLlama {
		t.Errorf("mlx toggles to %q, want %q", mlx.other(), config.BackendLlama)
	}
}
