package picks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cria/internal/config"
)

// The entries the cases pick along are built here rather than loaded from a
// file: the store reads entries the loader has already validated, so a case
// reads as the axes it is about.

func quantAxis() config.Choice {
	return config.Choice{Name: "quant", Options: []config.ChoiceOption{
		{Name: "q4", Quant: "UD-Q4_K_XL"},
		{Name: "q6", Quant: "Q6_K"},
		{Name: "q8", Quant: "Q8_0"},
	}}
}

func slotsAxis() config.Choice {
	return config.Choice{Name: "slots", Options: []config.ChoiceOption{
		{Name: "one"},
		{Name: "four", Args: []string{"--parallel", "4"}},
	}}
}

func entryVaryingOn(id string, choices ...config.Choice) config.Entry {
	return config.Entry{
		ID:      id,
		Backend: config.BackendLlama,
		Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
		Quant:   "UD-Q4_K_XL",
		Name:    id,
		Choices: choices,
	}
}

// What the picker wrote survives the program: a change is written, and the next
// launch reads it back.
func TestPicksRoundTrip(t *testing.T) {
	root := t.TempDir()
	saved := Picks{
		"qwen":  {"quant": "q8", "slots": "four"},
		"gemma": {"quant": "q4"},
	}

	if err := Save(root, saved); err != nil {
		t.Fatalf("saving the picks: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("loading the picks: %v", err)
	}
	if !reflect.DeepEqual(loaded, saved) {
		t.Errorf("the picks came back as %v, want %v", loaded, saved)
	}
}

// The file is the shape docs/specs/CONFIG.md names — entry id, choice, option —
// written in sorted order, so a store that did not change reads as a file that
// did not change.
func TestPicksAreWrittenAsSortedJSON(t *testing.T) {
	root := t.TempDir()
	saved := Picks{
		"qwen":  {"quant": "q8", "ctx": "long"},
		"gemma": {"slots": "four"},
	}
	if err := Save(root, saved); err != nil {
		t.Fatalf("saving the picks: %v", err)
	}

	data, err := os.ReadFile(picksPath(root))
	if err != nil {
		t.Fatalf("reading the picks file: %v", err)
	}
	want := "{\n  \"gemma\": {\n    \"slots\": \"four\"\n  },\n  \"qwen\": {\n    \"ctx\": \"long\",\n    \"quant\": \"q8\"\n  }\n}\n"
	if string(data) != want {
		t.Errorf("the file reads %q, want %q", data, want)
	}
}

// An empty store is an empty object: cria writes only files cria can read back,
// and a null would not be one.
func TestEmptyPicksAreWrittenAsAnEmptyObject(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, nil); err != nil {
		t.Fatalf("saving the picks: %v", err)
	}

	data, err := os.ReadFile(picksPath(root))
	if err != nil {
		t.Fatalf("reading the picks file: %v", err)
	}
	if string(data) != "{}\n" {
		t.Errorf("the file reads %q, want %q", data, "{}\n")
	}
	loaded, err := Load(root)
	if err != nil {
		t.Errorf("an empty store reported %v, want no error", err)
	}
	if !reflect.DeepEqual(loaded, Picks{}) {
		t.Errorf("an empty store came back as %v, want no picks", loaded)
	}
}

// The file is written where docs/specs/CONFIG.md puts it: the state directory,
// not the config tree.
func TestPicksAreWrittenToTheStateDirectory(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, Picks{"qwen": {"quant": "q8"}}); err != nil {
		t.Fatalf("saving the picks: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "choices.json")); err != nil {
		t.Errorf("choices.json is not in the state root: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the state root: %v", err)
	}
	// The temporary file the atomic write goes through must not survive it.
	for _, entry := range entries {
		if entry.Name() != "choices.json" {
			t.Errorf("the state root also holds %q", entry.Name())
		}
	}
}

// Nothing picked yet is not a problem to report: every entry runs its config
// defaults, which is the whole answer.
func TestMissingPicksAreEmptyAndSilent(t *testing.T) {
	loaded, err := Load(t.TempDir())
	if err != nil {
		t.Errorf("a missing picks file reported %v, want no error", err)
	}
	if !reflect.DeepEqual(loaded, Picks{}) {
		t.Errorf("a missing picks file gave %v, want no picks", loaded)
	}
}

// A picks file cria cannot read is reported and set aside: it is machine-owned
// state, so a broken one is never worth refusing to launch over, and every way it
// can be broken says so out loud rather than defaulting silently.
func TestBrokenPicksResetLoudly(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		contains string
	}{
		{name: "not JSON at all", file: "qwen.quant = q4\n", contains: "unreadable"},
		{name: "a JSON array", file: `[{"qwen":{"quant":"q4"}}]`, contains: "unreadable"},
		{name: "an entry that is not a set of picks", file: `{"qwen":"q4"}`, contains: "unreadable"},
		{name: "a pick of the wrong type", file: `{"qwen":{"quant":4}}`, contains: "unreadable"},
		{name: "two documents in one file", file: `{"qwen":{"quant":"q4"}}{"gemma":{"quant":"q8"}}`, contains: "more than one JSON document"},
		{name: "a null document", file: `null`, contains: "no picks object"},
		{name: "a pick under an empty entry id", file: `{"":{"quant":"q4"}}`, contains: "empty entry id"},
		{name: "a pick for an unnamed choice", file: `{"qwen":{"":"q4"}}`, contains: "unnamed choice"},
		{name: "a choice picking nothing", file: `{"qwen":{"quant":""}}`, contains: `picks nothing for choice "quant"`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(picksPath(root), []byte(test.file), 0o644); err != nil {
				t.Fatalf("writing the broken picks file: %v", err)
			}

			loaded, err := Load(root)
			if err == nil {
				t.Fatal("a broken picks file was accepted silently")
			}
			if !strings.Contains(err.Error(), test.contains) {
				t.Errorf("the failure reads %q, want it to name %q", err, test.contains)
			}
			if !reflect.DeepEqual(loaded, Picks{}) {
				t.Errorf("a broken picks file gave %v, want no picks", loaded)
			}
		})
	}
}

// An entry id the tree does not hold is not what "broken" means: the store is
// read before the tree is, and a pick left over from a deleted entry file is
// stale state that Prune drops on the next write.
func TestPicksForUnknownEntriesLoadFine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(picksPath(root), []byte(`{"ghost":{"quant":"q4"}}`), 0o644); err != nil {
		t.Fatalf("writing the picks file: %v", err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("a pick for an unknown entry reported %v, want no error", err)
	}
	if want := (Picks{"ghost": {"quant": "q4"}}); !reflect.DeepEqual(loaded, want) {
		t.Errorf("the picks came back as %v, want %v", loaded, want)
	}
}

// The three layers, one per case: the config defaults, the stored picks over
// them, the explicit picks over both — and a stored pick that names nothing the
// entry has any more falling back rather than failing.
func TestMergeLayers(t *testing.T) {
	tests := []struct {
		name     string
		entry    config.Entry
		stored   config.Selection
		explicit config.Selection
		want     config.Selection
	}{
		{
			name:  "a flat entry has nothing to pick",
			entry: entryVaryingOn("demo"),
			want:  config.Selection{},
		},
		{
			name:  "nothing stored, nothing explicit: the config defaults",
			entry: entryVaryingOn("demo", quantAxis(), slotsAxis()),
			want:  config.Selection{"quant": "q4", "slots": "one"},
		},
		{
			name:   "a stored pick overrides the config default",
			entry:  entryVaryingOn("demo", quantAxis(), slotsAxis()),
			stored: config.Selection{"quant": "q8"},
			want:   config.Selection{"quant": "q8", "slots": "one"},
		},
		{
			name:     "an explicit pick overrides a stored one",
			entry:    entryVaryingOn("demo", quantAxis(), slotsAxis()),
			stored:   config.Selection{"quant": "q8"},
			explicit: config.Selection{"quant": "q6"},
			want:     config.Selection{"quant": "q6", "slots": "one"},
		},
		{
			name:     "an explicit pick overrides the config default with nothing stored",
			entry:    entryVaryingOn("demo", quantAxis(), slotsAxis()),
			explicit: config.Selection{"slots": "four"},
			want:     config.Selection{"quant": "q4", "slots": "four"},
		},
		{
			name:     "the layers are read per choice, not per selection",
			entry:    entryVaryingOn("demo", quantAxis(), slotsAxis()),
			stored:   config.Selection{"quant": "q8"},
			explicit: config.Selection{"slots": "four"},
			want:     config.Selection{"quant": "q8", "slots": "four"},
		},
		{
			name:   "a stored pick for a choice the entry no longer has is skipped",
			entry:  entryVaryingOn("demo", quantAxis(), slotsAxis()),
			stored: config.Selection{"ctx": "long", "quant": "q8"},
			want:   config.Selection{"quant": "q8", "slots": "one"},
		},
		{
			name:   "a stored pick for an option the choice no longer has falls back to the default",
			entry:  entryVaryingOn("demo", quantAxis(), slotsAxis()),
			stored: config.Selection{"quant": "q5"},
			want:   config.Selection{"quant": "q4", "slots": "one"},
		},
		{
			name:   "a stored pick against an entry that lost its axes is skipped, not refused",
			entry:  entryVaryingOn("demo"),
			stored: config.Selection{"quant": "q8"},
			want:   config.Selection{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection, err := Merge(test.entry, test.stored, test.explicit)
			if err != nil {
				t.Fatalf("Merge: %v", err)
			}
			if !reflect.DeepEqual(selection, test.want) {
				t.Errorf("the merged selection is %v, want %v", selection, test.want)
			}
			// The merge feeds Resolve, so what it produces has to resolve.
			if _, err := config.Resolve(test.entry, selection); err != nil {
				t.Errorf("the merged selection does not resolve: %v", err)
			}
		})
	}
}

// An explicit pick naming nothing is the caller's mistake, and it is refused in
// Resolve's own words — the entry, what was not found, and the names that would
// have worked.
func TestMergeRefusesExplicitPicks(t *testing.T) {
	tests := []struct {
		name     string
		entry    config.Entry
		stored   config.Selection
		explicit config.Selection
		want     []string
	}{
		{
			name:     "a choice the entry does not have",
			entry:    entryVaryingOn("demo", quantAxis(), slotsAxis()),
			explicit: config.Selection{"qunt": "q4"},
			want:     []string{"demo", `no choice named "qunt"`, "quant, slots"},
		},
		{
			name:     "an option the choice does not have",
			entry:    entryVaryingOn("demo", quantAxis()),
			explicit: config.Selection{"quant": "q9"},
			want:     []string{"demo", `choice "quant"`, `no option named "q9"`, "q4, q6, q8"},
		},
		{
			name:     "a pick against a flat entry",
			entry:    entryVaryingOn("demo"),
			explicit: config.Selection{"quant": "q8"},
			want:     []string{"demo", "has no choices", "quant"},
		},
		{
			name:     "a stale stored pick does not shield a bad explicit one",
			entry:    entryVaryingOn("demo", quantAxis(), slotsAxis()),
			stored:   config.Selection{"quant": "gone"},
			explicit: config.Selection{"slots": "eight"},
			want:     []string{"demo", `choice "slots"`, `no option named "eight"`, "one, four"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection, err := Merge(test.entry, test.stored, test.explicit)
			if err == nil {
				t.Fatalf("the picks merged to %v, want a refusal naming %v", selection, test.want)
			}
			if selection != nil {
				t.Errorf("a refused merge also returned %v, want no selection", selection)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal is %q, want it to name %s", err, want)
				}
			}
		})
	}
}

// Merging reads the store; it never writes to it. The merged selection outlives
// the call — it is handed to a start — so sharing a map with the store would let
// one launch's picks rewrite what the picker recorded.
func TestMergeLeavesItsInputsAlone(t *testing.T) {
	entry := entryVaryingOn("demo", quantAxis(), slotsAxis())
	stored := config.Selection{"quant": "q8"}
	explicit := config.Selection{"slots": "four"}

	selection, err := Merge(entry, stored, explicit)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	selection["quant"] = "q4"
	selection["ctx"] = "long"

	if want := (config.Selection{"quant": "q8"}); !reflect.DeepEqual(stored, want) {
		t.Errorf("the stored picks became %v, want %v", stored, want)
	}
	if want := (config.Selection{"slots": "four"}); !reflect.DeepEqual(explicit, want) {
		t.Errorf("the explicit picks became %v, want %v", explicit, want)
	}
}

// Pruning is what keeps the file honest: it records what the tree can still
// answer for, and nothing else.
func TestPrune(t *testing.T) {
	tree := &config.Tree{
		Entries: []config.Entry{
			entryVaryingOn("qwen", quantAxis(), slotsAxis()),
			entryVaryingOn("gemma", quantAxis()),
			entryVaryingOn("flat"),
		},
		Broken: []config.BrokenEntry{
			{ID: "typo", Path: "/models/typo.toml", Err: &config.KeyError{Key: "quant", Reason: "want a string"}},
		},
	}

	tests := []struct {
		name   string
		stored Picks
		tree   *config.Tree
		want   Picks
	}{
		{
			name:   "picks the tree still answers for are kept",
			stored: Picks{"qwen": {"quant": "q8", "slots": "four"}, "gemma": {"quant": "q6"}},
			tree:   tree,
			want:   Picks{"qwen": {"quant": "q8", "slots": "four"}, "gemma": {"quant": "q6"}},
		},
		{
			name:   "an entry whose file is gone is dropped",
			stored: Picks{"qwen": {"quant": "q8"}, "ghost": {"quant": "q4"}},
			tree:   tree,
			want:   Picks{"qwen": {"quant": "q8"}},
		},
		{
			name:   "a choice the entry no longer has is dropped",
			stored: Picks{"qwen": {"quant": "q8", "ctx": "long"}},
			tree:   tree,
			want:   Picks{"qwen": {"quant": "q8"}},
		},
		{
			name:   "an option the choice no longer has is dropped",
			stored: Picks{"qwen": {"quant": "q5", "slots": "four"}},
			tree:   tree,
			want:   Picks{"qwen": {"slots": "four"}},
		},
		{
			name:   "an entry left with no picks goes with them",
			stored: Picks{"gemma": {"quant": "q5"}, "qwen": {"slots": "one"}},
			tree:   tree,
			want:   Picks{"qwen": {"slots": "one"}},
		},
		{
			name:   "an entry that lost its axes keeps no picks",
			stored: Picks{"flat": {"quant": "q8"}},
			tree:   tree,
			want:   Picks{},
		},
		{
			name:   "a broken entry file keeps every pick it had",
			stored: Picks{"typo": {"quant": "q8", "ctx": "long"}},
			tree:   tree,
			want:   Picks{"typo": {"quant": "q8", "ctx": "long"}},
		},
		{
			name:   "a tree cria has not read prunes nothing",
			stored: Picks{"ghost": {"quant": "q4"}, "qwen": {"ctx": "long"}},
			tree:   nil,
			want:   Picks{"ghost": {"quant": "q4"}, "qwen": {"ctx": "long"}},
		},
		{
			name:   "an empty store prunes to an empty store",
			stored: Picks{},
			tree:   tree,
			want:   Picks{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pruned := Prune(test.stored, test.tree)
			if !reflect.DeepEqual(pruned, test.want) {
				t.Errorf("the pruned picks are %v, want %v", pruned, test.want)
			}
		})
	}
}

// Nothing of the argument is kept: the pruned store is what gets written, while
// the caller may still be reading the one it handed over.
func TestPruneLeavesTheArgumentAlone(t *testing.T) {
	tree := &config.Tree{Entries: []config.Entry{entryVaryingOn("qwen", quantAxis(), slotsAxis())}}
	stored := Picks{"qwen": {"quant": "q8", "ctx": "long"}}

	pruned := Prune(stored, tree)
	pruned["qwen"]["quant"] = "q4"
	delete(pruned, "qwen")

	want := Picks{"qwen": {"quant": "q8", "ctx": "long"}}
	if !reflect.DeepEqual(stored, want) {
		t.Errorf("the store became %v, want %v", stored, want)
	}
}
