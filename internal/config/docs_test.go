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
// appear in that backend's example at its example value.
func TestDocsExamplesShowEveryKeyOfTheirBackend(t *testing.T) {
	for _, backend := range []Backend{BackendLlama, BackendMLX} {
		t.Run(string(backend), func(t *testing.T) {
			example := exampleEntry(backend)
			for _, k := range entrySchema {
				assignment := k.name + " = " + k.exampleFor(backend)
				if k.onlyBackend != "" && k.onlyBackend != backend {
					if strings.Contains(example, k.name+" =") {
						t.Errorf("the %q example sets %q, a key only the %q backend takes", backend, k.name, k.onlyBackend)
					}
					continue
				}
				if !strings.Contains(example, assignment) {
					t.Errorf("the %q example does not hold %q", backend, assignment)
				}
			}
			if !strings.Contains(Docs(), example) {
				t.Errorf("the %q example is not part of the `cria docs` page", backend)
			}
		})
	}
}

func TestDocsSettingsExampleShowsEveryTreeKey(t *testing.T) {
	example := exampleSettings()
	for _, k := range treeSchema {
		if k.kind == kindTable {
			if !strings.Contains(example, "["+k.name+"]") {
				t.Errorf("the config.toml example has no [%s] section", k.name)
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
		path.Join(entriesDir, "llama-example.toml"): exampleEntry(BackendLlama),
		path.Join(entriesDir, "mlx-example.toml"):   exampleEntry(BackendMLX),
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
			if k.kind == kindTable {
				walk(k.keys, prefix+k.name+".")
			}
		}
	}
	walk(entrySchema, "")
	walk(treeSchema, "")
	return names
}
