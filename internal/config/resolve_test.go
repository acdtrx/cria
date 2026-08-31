package config

import (
	"reflect"
	"strings"
	"testing"
)

// Resolution, rule by rule (docs/specs/CONFIG.md, Choices). The entries are built
// here rather than loaded from a file: Resolve reads an entry the loader has
// already validated, and a case then reads as the axes it is about.

// args spells an args list the way a file writes it, so a case reads as the
// lines its entry holds.
func args(lines ...string) []Arg {
	list := make([]Arg, 0, len(lines))
	for _, line := range lines {
		arg, err := parseArg(line)
		if err != nil {
			panic("test wrote an args line the schema refuses: " + err.Error())
		}
		list = append(list, arg)
	}
	return list
}

// entryVaryingOn is a launchable flat entry plus the axes a case tests.
func entryVaryingOn(entryArgs []Arg, choices ...Choice) Entry {
	return Entry{
		ID:      "demo",
		Backend: BackendLlama,
		Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
		Quant:   "UD-Q4_K_XL",
		Port:    8080,
		Host:    defaultBindHost,
		Name:    "demo",
		Args:    entryArgs,
		Choices: choices,
	}
}

// The three axes the composition cases pick along: one replacing the quant, one
// adding args only, one whose first option adds nothing at all.
func quantAxis() Choice {
	return Choice{Name: "quant", Options: []ChoiceOption{
		{Name: "q4", Quant: "UD-Q4_K_XL", Args: args("ctx-size = 32768")},
		{Name: "q8", Quant: "Q8_0", Args: args("ctx-size = 16384")},
	}}
}

func slotsAxis() Choice {
	return Choice{Name: "slots", Options: []ChoiceOption{
		{Name: "one"},
		{Name: "four", Args: args("parallel = 4")},
	}}
}

func offloadAxis() Choice {
	return Choice{Name: "offload", Options: []ChoiceOption{
		{Name: "gpu", Args: args("n-cpu-moe = 0")},
		{Name: "cpu", Args: args("n-cpu-moe = 24")},
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
	entry := entryVaryingOn(args("jinja = true"), quantAxis(), slotsAxis())

	launch, err := Resolve(entry, DefaultSelection(entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := Launch{
		Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
		Quant: "UD-Q4_K_XL",
		Args:  args("jinja = true", "ctx-size = 32768"),
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
			entry:     entryVaryingOn(args("jinja = true", "ctx-size = 16384")),
			selection: nil,
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
				Args:  args("jinja = true", "ctx-size = 16384"),
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
			name:      "three axes follow the entry's own keys, in file order",
			entry:     entryVaryingOn(args("jinja = true"), quantAxis(), slotsAxis(), offloadAxis()),
			selection: Selection{"quant": "q8", "slots": "four", "offload": "cpu"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "Q8_0",
				Args:  args("jinja = true", "ctx-size = 16384", "parallel = 4", "n-cpu-moe = 24"),
			},
		},
		{
			name:      "the selection, not the file order, decides which option is read",
			entry:     entryVaryingOn(nil, quantAxis(), slotsAxis(), offloadAxis()),
			selection: Selection{"quant": "q4", "slots": "four", "offload": "gpu"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
				Args:  args("ctx-size = 32768", "parallel = 4", "n-cpu-moe = 0"),
			},
		},
		{
			name:      "a picked option replaces the entry's quant",
			entry:     entryVaryingOn(nil, quantAxis()),
			selection: Selection{"quant": "q8"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "Q8_0",
				Args:  args("ctx-size = 16384"),
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
			entry: entryVaryingOn(args("jinja = true"), Choice{Name: "context", Options: []ChoiceOption{
				{Name: "short"},
				{Name: "long", Repo: "unsloth/Qwen3-30B-A3B-128K-GGUF", Quant: "Q8_0", Args: args("ctx-size = 131072")},
			}}),
			selection: Selection{"context": "long"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-128K-GGUF",
				Quant: "Q8_0",
				Args:  args("jinja = true", "ctx-size = 131072"),
			},
		},
		{
			name:      "an option that only names itself changes nothing",
			entry:     entryVaryingOn(args("jinja = true"), slotsAxis()),
			selection: Selection{"slots": "one"},
			want: Launch{
				Repo:  "unsloth/Qwen3-30B-A3B-GGUF",
				Quant: "UD-Q4_K_XL",
				Args:  args("jinja = true"),
			},
		},
		{
			name:      "an entry without a quant stays without one until an option sets it",
			entry:     Entry{ID: "demo", Backend: BackendLlama, Repo: "org/name", Choices: []Choice{slotsAxis()}},
			selection: Selection{"slots": "four"},
			want: Launch{
				Repo: "org/name",
				Args: args("parallel = 4"),
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
			entry:     entryVaryingOn(args("jinja = true")),
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
	own := make([]Arg, 0, 8)
	own = append(own, args("jinja = true")...)
	entry := entryVaryingOn(own, slotsAxis())

	four, err := Resolve(entry, Selection{"slots": "four"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	one, err := Resolve(entry, Selection{"slots": "one"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if want := args("jinja = true", "parallel = 4"); !reflect.DeepEqual(four.Args, want) {
		t.Errorf("the first launch's args became %v, want %v", four.Args, want)
	}
	if want := args("jinja = true"); !reflect.DeepEqual(one.Args, want) {
		t.Errorf("the second launch's args are %v, want %v", one.Args, want)
	}
	if want := args("jinja = true"); !reflect.DeepEqual(entry.Args, want) {
		t.Errorf("the entry's own args became %v, want %v", entry.Args, want)
	}
}

// The levels of one launch, least specific first: what the engine serves
// everything with, what the entry declares, what the picks change. A key set at
// more than one level takes the most specific value and keeps the place of the
// first line that mentioned it, so an override changes the value and not the
// order the file is read in (docs/specs/CONFIG.md).
func TestResolveMergesTheLevelsBySpecificity(t *testing.T) {
	tests := []struct {
		name       string
		engineArgs []Arg
		entryArgs  []Arg
		choices    []Choice
		selection  Selection
		want       []Arg
	}{
		{
			name:       "an engine's keys come first, and the entry's follow",
			engineArgs: args("gpu-layers = 99", "flash-attn = on"),
			entryArgs:  args("ctx-size = 16384"),
			want:       args("gpu-layers = 99", "flash-attn = on", "ctx-size = 16384"),
		},
		{
			name:       "the entry overrides a key its engine sets",
			engineArgs: args("gpu-layers = 99", "flash-attn = on"),
			entryArgs:  args("gpu-layers = 40"),
			want:       args("gpu-layers = 40", "flash-attn = on"),
		},
		{
			name:      "a picked option overrides a key the entry sets",
			entryArgs: args("ctx-size = 16384", "jinja = true"),
			choices: []Choice{{Name: "ctx", Options: []ChoiceOption{
				{Name: "long", Args: args("ctx-size = 65536")},
			}}},
			selection: Selection{"ctx": "long"},
			want:      args("ctx-size = 65536", "jinja = true"),
		},
		{
			name:       "a pick outranks the entry, which outranks the engine",
			engineArgs: args("ctx-size = 4096"),
			entryArgs:  args("ctx-size = 16384"),
			choices: []Choice{{Name: "ctx", Options: []ChoiceOption{
				{Name: "long", Args: args("ctx-size = 65536")},
			}}},
			selection: Selection{"ctx": "long"},
			want:      args("ctx-size = 65536"),
		},
		{
			name:       "an engine's keys reach an entry that declares none of its own",
			engineArgs: args("log-level = INFO"),
			want:       args("log-level = INFO"),
		},
		{
			name:      "an entry with no engine file carries only its own",
			entryArgs: args("ctx-size = 16384"),
			want:      args("ctx-size = 16384"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := entryVaryingOn(test.entryArgs, test.choices...)
			entry.EngineArgs = test.engineArgs

			launch, err := Resolve(entry, test.selection)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(launch.Args, test.want) {
				t.Errorf("the launch's args are\n  %v\nwant\n  %v", launch.Args, test.want)
			}
		})
	}
}

// An override reads one value; the level it came from is not smuggled along
// beside it. Resolving twice under different picks proves the merge holds no
// state from the launch before.
func TestAnOverriddenKeyIsOneKey(t *testing.T) {
	entry := entryVaryingOn(args("ctx-size = 16384"), Choice{Name: "ctx", Options: []ChoiceOption{
		{Name: "short"},
		{Name: "long", Args: args("ctx-size = 65536")},
	}})
	entry.EngineArgs = args("ctx-size = 4096")

	long, err := Resolve(entry, Selection{"ctx": "long"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := args("ctx-size = 65536"); !reflect.DeepEqual(long.Args, want) {
		t.Errorf("the picked launch's args are %v, want %v", long.Args, want)
	}

	short, err := Resolve(entry, Selection{"ctx": "short"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := args("ctx-size = 16384"); !reflect.DeepEqual(short.Args, want) {
		t.Errorf("the unpicked launch's args are %v, want %v", short.Args, want)
	}
	if want := args("ctx-size = 4096"); !reflect.DeepEqual(entry.EngineArgs, want) {
		t.Errorf("the engine's own args became %v, want %v", entry.EngineArgs, want)
	}
}
