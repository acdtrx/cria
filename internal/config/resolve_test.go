package config

import (
	"reflect"
	"strings"
	"testing"
)

// Resolution, rule by rule (docs/specs/CONFIG.md, Choices). The entries are built
// here rather than loaded from a file: Resolve reads an entry the loader has
// already validated, and a case then reads as the axes it is about.

// entryVaryingOn is a launchable flat entry plus the axes a case tests.
func entryVaryingOn(args []string, choices ...Choice) Entry {
	return Entry{
		ID:      "demo",
		Backend: BackendLlama,
		Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
		Quant:   "UD-Q4_K_XL",
		Port:    8080,
		Host:    defaultBindHost,
		Name:    "demo",
		Args:    args,
		Choices: choices,
	}
}

// The three axes the composition cases pick along: one replacing the quant, one
// adding args only, one whose first option adds nothing at all.
func quantAxis() Choice {
	return Choice{Name: "quant", Options: []ChoiceOption{
		{Name: "q4", Quant: "UD-Q4_K_XL", Args: []string{"--ctx-size", "32768"}},
		{Name: "q8", Quant: "Q8_0", Args: []string{"--ctx-size", "16384"}},
	}}
}

func slotsAxis() Choice {
	return Choice{Name: "slots", Options: []ChoiceOption{
		{Name: "one"},
		{Name: "four", Args: []string{"--parallel", "4"}},
	}}
}

func offloadAxis() Choice {
	return Choice{Name: "offload", Options: []ChoiceOption{
		{Name: "gpu", Args: []string{"--n-cpu-moe", "0"}},
		{Name: "cpu", Args: []string{"--n-cpu-moe", "24"}},
	}}
}

func TestDefaultSelectionPicksTheFirstOption(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  Selection
	}{
		{
			name:  "a flat entry has nothing to pick",
			entry: entryVaryingOn(nil),
			want:  Selection{},
		},
		{
			name:  "one axis",
			entry: entryVaryingOn(nil, quantAxis()),
			want:  Selection{"quant": "q4"},
		},
		{
			name:  "every axis defaults on its own",
			entry: entryVaryingOn(nil, quantAxis(), slotsAxis(), offloadAxis()),
			want:  Selection{"quant": "q4", "slots": "one", "offload": "gpu"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DefaultSelection(test.entry)
			// DeepEqual against an empty Selection also pins the documented shape: a
			// flat entry's default is empty and never nil, so a caller can layer its
			// stored and explicit picks straight over it.
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("default selection is %v, want %v", got, test.want)
			}
		})
	}
}

// The config default is a selection like any other: resolving under it composes
// the first option of every axis.
func TestResolveUnderTheDefaultSelection(t *testing.T) {
	entry := entryVaryingOn([]string{"--jinja"}, quantAxis(), slotsAxis())

	launch, err := Resolve(entry, DefaultSelection(entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := Launch{
		Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
		Quant: "UD-Q4_K_XL",
		Args:  []string{"--jinja", "--ctx-size", "32768"},
	}
	if !reflect.DeepEqual(launch, want) {
		t.Errorf("the default launch is\n  %+v\nwant\n  %+v", launch, want)
	}
}

func TestResolveComposes(t *testing.T) {
	tests := []struct {
		name      string
		entry     Entry
		selection Selection
		want      Launch
	}{
		{
			name:      "a flat entry resolves to itself under no selection",
			entry:     entryVaryingOn([]string{"--jinja", "--ctx-size", "16384"}),
			selection: nil,
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
				Args:  []string{"--jinja", "--ctx-size", "16384"},
			},
		},
		{
			name:      "a flat entry resolves to itself under an empty selection",
			entry:     entryVaryingOn(nil),
			selection: Selection{},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
			},
		},
		{
			name:      "three axes append in file order, after the entry's own args",
			entry:     entryVaryingOn([]string{"--jinja"}, quantAxis(), slotsAxis(), offloadAxis()),
			selection: Selection{"quant": "q8", "slots": "four", "offload": "cpu"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "Q8_0",
				Args: []string{
					"--jinja",
					"--ctx-size", "16384",
					"--parallel", "4",
					"--n-cpu-moe", "24",
				},
			},
		},
		{
			name:      "the selection, not the file order, decides which option is read",
			entry:     entryVaryingOn(nil, quantAxis(), slotsAxis(), offloadAxis()),
			selection: Selection{"quant": "q4", "slots": "four", "offload": "gpu"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
				Args: []string{
					"--ctx-size", "32768",
					"--parallel", "4",
					"--n-cpu-moe", "0",
				},
			},
		},
		{
			name:      "a picked option replaces the entry's quant",
			entry:     entryVaryingOn(nil, quantAxis()),
			selection: Selection{"quant": "q8"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "Q8_0",
				Args:  []string{"--ctx-size", "16384"},
			},
		},
		{
			name: "a picked option replaces the entry's repo — an mlx quantization is one",
			entry: entryVaryingOn(nil, Choice{Name: "quant", Options: []ChoiceOption{
				{Name: "4bit", Repo: "mlx-community/Qwen3-30B-A3B-4bit"},
				{Name: "8bit", Repo: "mlx-community/Qwen3-30B-A3B-8bit"},
			}}),
			selection: Selection{"quant": "8bit"},
			want: Launch{
				Repo:  "mlx-community/Qwen3-30B-A3B-8bit",
				Quant: "UD-Q4_K_XL",
			},
		},
		{
			name: "an option may replace the repo and the quant at once",
			entry: entryVaryingOn([]string{"--jinja"}, Choice{Name: "context", Options: []ChoiceOption{
				{Name: "short"},
				{Name: "long", Repo: "unsloth/Qwen3-30B-A3B-128K-GGUF", Quant: "Q8_0", Args: []string{"--ctx-size", "131072"}},
			}}),
			selection: Selection{"context": "long"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-128K-GGUF",
				Quant: "Q8_0",
				Args:  []string{"--jinja", "--ctx-size", "131072"},
			},
		},
		{
			name:      "an option that only names itself changes nothing",
			entry:     entryVaryingOn([]string{"--jinja"}, slotsAxis()),
			selection: Selection{"slots": "one"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
				Args:  []string{"--jinja"},
			},
		},
		{
			name:      "an entry without a quant stays without one until an option sets it",
			entry:     Entry{ID: "demo", Backend: BackendLlama, Repo: "org/name", Choices: []Choice{slotsAxis()}},
			selection: Selection{"slots": "four"},
			want: Launch{
				Repo: "org/name",
				Args: []string{"--parallel", "4"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			launch, err := Resolve(test.entry, test.selection)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(launch, test.want) {
				t.Errorf("the launch is\n  %+v\nwant\n  %+v", launch, test.want)
			}
		})
	}
}

// A refusal is read by whoever typed the pick, so each one names the entry, what
// it could not find, and the names that would have worked.
func TestResolveRefuses(t *testing.T) {
	tests := []struct {
		name      string
		entry     Entry
		selection Selection
		want      []string
		notWant   []string
	}{
		{
			name:      "a choice the entry does not have",
			entry:     entryVaryingOn(nil, quantAxis(), slotsAxis()),
			selection: Selection{"quant": "q4", "slots": "one", "ctx": "long"},
			want:      []string{"demo", `no choice named "ctx"`, "quant, slots"},
		},
		{
			name:      "a misspelled choice is the misspelling, not an unpicked axis",
			entry:     entryVaryingOn(nil, quantAxis()),
			selection: Selection{"qunt": "q4"},
			want:      []string{"demo", `no choice named "qunt"`, "its choices are: quant"},
			notWant:   []string{"nothing picked"},
		},
		{
			name:      "an option the choice does not have",
			entry:     entryVaryingOn(nil, quantAxis()),
			selection: Selection{"quant": "q6"},
			want:      []string{"demo", `choice "quant"`, `no option named "q6"`, "q4, q8"},
		},
		{
			name:      "a choice left unpicked",
			entry:     entryVaryingOn(nil, quantAxis(), slotsAxis()),
			selection: Selection{"slots": "four"},
			want:      []string{"demo", `nothing picked for choice "quant"`, "q4, q8"},
		},
		{
			name:      "an empty selection against an entry that has axes",
			entry:     entryVaryingOn(nil, quantAxis()),
			selection: Selection{},
			want:      []string{"demo", `nothing picked for choice "quant"`, "q4, q8"},
		},
		{
			name:      "a flat entry has nothing to pick",
			entry:     entryVaryingOn([]string{"--jinja"}),
			selection: Selection{"quant": "q8"},
			want:      []string{"demo", "has no choices", "quant"},
		},
		{
			name:      "a flat entry names every pick it was handed",
			entry:     entryVaryingOn(nil),
			selection: Selection{"quant": "q8", "slots": "four"},
			want:      []string{"demo", "has no choices", "quant, slots"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			launch, err := Resolve(test.entry, test.selection)
			if err == nil {
				t.Fatalf("the selection resolved to %+v, want a refusal naming %v", launch, test.want)
			}
			if !reflect.DeepEqual(launch, Launch{}) {
				t.Errorf("a refused resolution also returned %+v, want the zero Launch", launch)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal is %q, want it to name %s", err, want)
				}
			}
			for _, notWant := range test.notWant {
				if strings.Contains(err.Error(), notWant) {
					t.Errorf("the refusal is %q, want it not to say %s", err, notWant)
				}
			}
		})
	}
}

// Resolution reads the entry; it never writes to it. Composing into the entry's
// own args slice would leak one launch's picks into the next, since the loaded
// tree outlives any single start.
func TestResolveLeavesTheEntryUntouched(t *testing.T) {
	// Spare capacity is what makes the aliasing possible: appending into it writes
	// past the entry's own length instead of allocating.
	args := make([]string, 0, 8)
	args = append(args, "--jinja")
	entry := entryVaryingOn(args, slotsAxis())

	four, err := Resolve(entry, Selection{"slots": "four"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	one, err := Resolve(entry, Selection{"slots": "one"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if want := []string{"--jinja", "--parallel", "4"}; !reflect.DeepEqual(four.Args, want) {
		t.Errorf("the first launch's args became %v, want %v", four.Args, want)
	}
	if want := []string{"--jinja"}; !reflect.DeepEqual(one.Args, want) {
		t.Errorf("the second launch's args are %v, want %v", one.Args, want)
	}
	if want := []string{"--jinja"}; !reflect.DeepEqual(entry.Args, want) {
		t.Errorf("the entry's own args became %v, want %v", entry.Args, want)
	}
}
