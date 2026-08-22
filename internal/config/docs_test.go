package config

import (
	"path"
	"strings"
	"testing"
)

// The docs are generated from the schema definitions, so these tests read the
// definitions too: nothing here spells a key name or a rules line of its own —
// that would be the second source the design exists to avoid.

// TestDocsNamesEveryDefinedKey is the coverage guard: a key added to a schema
// shows up in `cria docs` or this fails.
func TestDocsNamesEveryDefinedKey(t *testing.T) {
	page := Docs()
	for _, name := range definedKeyNames() {
		if !strings.Contains(page, name) {
			t.Errorf("`cria docs` never names key %q", name)
		}
	}
}

// The examples are the templates agents copy, so every key a backend takes must
// appear in that backend's example at its example value — the keys of a nested
// block under that block's own header.
func TestDocsExamplesShowEveryKeyOfTheirBackend(t *testing.T) {
	var walk func(t *testing.T, s schema, prefix, example string, backend Backend)
	walk = func(t *testing.T, s schema, prefix, example string, backend Backend) {
		for _, k := range s {
			if k.onlyBackend != "" && k.onlyBackend != backend {
				if strings.Contains(example, k.name+" =") {
					t.Errorf("the %q example sets %q, a key only the %q backend takes", backend, k.name, k.onlyBackend)
				}
				continue
			}
			if k.kind.holdsKeys() {
				if header := k.header(prefix); !strings.Contains(example, header) {
					t.Errorf("the %q example has no %s block", backend, header)
				}
				walk(t, k.keys, prefix+k.name+".", example, backend)
				continue
			}
			if assignment := k.name + " = " + k.exampleFor(backend); !strings.Contains(example, assignment) {
				t.Errorf("the %q example does not hold %q", backend, assignment)
			}
		}
	}

	for _, backend := range []Backend{BackendLlama, BackendMLX} {
		t.Run(string(backend), func(t *testing.T) {
			example := ExampleEntry(backend)
			walk(t, entrySchema, "", example, backend)
			if !strings.Contains(Docs(), example) {
				t.Errorf("the %q example is not part of the `cria docs` page", backend)
			}
		})
	}
}

// An axis is opt-in structure, so the example carries it commented out: what
// `cria new` writes is a launchable flat entry, and uncommenting the block is all
// it takes to start varying the entry.
func TestDocsExamplesCarryTheAxisCommentedOut(t *testing.T) {
	for _, backend := range []Backend{BackendLlama, BackendMLX} {
		t.Run(string(backend), func(t *testing.T) {
			example := ExampleEntry(backend)
			for _, k := range entrySchema {
				if !k.kind.holdsKeys() {
					continue
				}
				if !strings.Contains(example, "# "+k.header("")) {
					t.Errorf("the %q example does not offer a commented-out %s block", backend, k.header(""))
				}
				if strings.Contains(example, "\n"+k.header("")) {
					t.Errorf("the %q example declares a live %s block; the template must stay flat", backend, k.header(""))
				}
			}

			entry, err := loadOne(t, "", example)
			if err != nil {
				t.Fatalf("the %q example was refused: %v", backend, err)
			}
			if len(entry.Choices) != 0 {
				t.Errorf("the %q example loaded with axes %+v, want a flat entry", backend, entry.Choices)
			}

			uncommented, err := loadOne(t, "", uncommentAxis(example))
			if err != nil {
				t.Fatalf("the %q example with its axis uncommented was refused: %v", backend, err)
			}
			if len(uncommented.Choices) != 1 || len(uncommented.Choices[0].Options) != 1 {
				t.Errorf("the uncommented %q example loaded axes %+v, want one axis of one option", backend, uncommented.Choices)
			}
		})
	}
}

// uncommentAxis takes the comment markers off the block an example ends with —
// what someone does by hand when they start varying an entry.
func uncommentAxis(example string) string {
	lines := strings.Split(example, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "# [[") {
			start = i
			break
		}
	}
	if start < 0 {
		return example
	}
	for i := start; i < len(lines); i++ {
		if lines[i] == "#" {
			lines[i] = ""
			continue
		}
		lines[i] = strings.TrimPrefix(lines[i], "# ")
	}
	return strings.Join(lines, "\n")
}

func TestDocsSettingsExampleShowsEveryTreeKey(t *testing.T) {
	example := exampleSettings()
	for _, k := range treeSchema {
		if k.kind == kindTable {
			if !strings.Contains(example, k.header("")) {
				t.Errorf("the config.toml example has no %s section", k.header(""))
			}
			for _, sub := range k.keys {
				if !strings.Contains(example, sub.name+" = "+sub.example) {
					t.Errorf("the config.toml example does not hold %q", sub.name+" = "+sub.example)
				}
			}
			continue
		}
		if !strings.Contains(example, k.name+" = "+k.example) {
			t.Errorf("the config.toml example does not hold %q", k.name+" = "+k.example)
		}
	}
	if !strings.Contains(Docs(), example) {
		t.Errorf("the config.toml example is not part of the `cria docs` page")
	}
}

// The one-source rule, proved by moving the source: a key invented here appears
// in the page — table, rules and example alike — without docs.go knowing it.
func TestDocsFollowsTheDefinitions(t *testing.T) {
	original := entrySchema
	t.Cleanup(func() { entrySchema = original })
	entrySchema = append(append(schema{}, original...), key{
		name:    "invented_key",
		kind:    kindString,
		rules:   "a key invented by a test to prove the docs follow the definitions",
		example: `"invented"`,
	})

	page := Docs()
	for _, want := range []string{
		"invented_key",
		"a key invented by a test to prove the docs follow the definitions",
		`invented_key = "invented"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("`cria docs` does not hold %q after the definition was added", want)
		}
	}
}

// The examples are copied into real trees, so they must load as real trees: what
// the page prints is written to a config tree and read back through the loader.
func TestDocsExamplesLoadAsAConfigTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		settingsFile: exampleSettings(),
		path.Join(entriesDir, "llama-example.toml"): ExampleEntry(BackendLlama),
		path.Join(entriesDir, "mlx-example.toml"):   ExampleEntry(BackendMLX),
	})

	tree, err := Load(root)
	if err != nil {
		t.Fatalf("the config.toml example failed to load: %v", err)
	}
	if len(tree.Broken) != 0 {
		for _, broken := range tree.Broken {
			t.Errorf("the example entry %s was refused: %v", broken.ID, broken.Err)
		}
		t.FailNow()
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("the examples loaded as %d entries, want 2", len(tree.Entries))
	}

	llama, mlx := tree.Entries[0], tree.Entries[1]
	if llama.Backend != BackendLlama || mlx.Backend != BackendMLX {
		t.Fatalf("entries loaded as backends %q and %q, want %q and %q", llama.Backend, mlx.Backend, BackendLlama, BackendMLX)
	}
	if llama.Quant == "" {
		t.Errorf("the llama example resolved without a quant; it is the key that backend alone takes")
	}
	if mlx.Quant != "" {
		t.Errorf("the mlx example resolved with quant %q, a key that backend does not take", mlx.Quant)
	}
	for _, entry := range tree.Entries {
		if entry.Repo == "" || entry.Port == 0 || entry.Host == "" || entry.Name == "" || len(entry.Args) == 0 {
			t.Errorf("the %s example resolved incompletely: %+v", entry.ID, entry)
		}
	}
}

// The page is read in a terminal and pasted into an agent's context; nothing on it
// runs off the edge.
func TestDocsFitsTheWidth(t *testing.T) {
	for i, line := range strings.Split(Docs(), "\n") {
		if width := len([]rune(line)); width > docsWidth {
			t.Errorf("line %d is %d columns wide, want at most %d:\n%s", i+1, width, docsWidth, line)
		}
	}
}

// definedKeyNames lists every key the schemas declare, sub-table keys under their
// dotted names.
func definedKeyNames() []string {
	var names []string
	var walk func(s schema, prefix string)
	walk = func(s schema, prefix string) {
		for _, k := range s {
			names = append(names, prefix+k.name)
			if k.kind.holdsKeys() {
				walk(k.keys, prefix+k.name+".")
			}
		}
	}
	walk(entrySchema, "")
	walk(treeSchema, "")
	return names
}
