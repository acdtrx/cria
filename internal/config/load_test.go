package config

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree materialises a config tree in a temp directory and returns its root.
// Keys are paths relative to the root, slash-separated ("models/demo.toml").
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", filepath.Dir(file), err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", file, err)
		}
	}
	return root
}

// loadOne loads a tree holding a single entry named demo and returns either the
// entry or the error that disabled it. An empty settings body means the tree has
// no config.toml at all.
func loadOne(t *testing.T, settings, entry string) (Entry, error) {
	t.Helper()
	files := map[string]string{path.Join(entriesDir, "demo"+tomlExt): entry}
	if settings != "" {
		files[settingsFile] = settings
	}
	tree, err := Load(writeTree(t, files))
	if err != nil {
		t.Fatalf("Load failed at tree level, want a per-entry outcome: %v", err)
	}
	switch {
	case len(tree.Entries) == 1 && len(tree.Broken) == 0:
		return tree.Entries[0], nil
	case len(tree.Entries) == 0 && len(tree.Broken) == 1:
		return Entry{}, tree.Broken[0].Err
	}
	t.Fatalf("want one valid or one broken entry, got %d valid and %d broken", len(tree.Entries), len(tree.Broken))
	return Entry{}, nil
}

// A broken entry disables itself and nothing else (docs/specs/CONFIG.md) — the
// isolation rule, tested with valid entries on both sides of a broken one.
func TestLoadIsolatesBrokenEntries(t *testing.T) {
	root := writeTree(t, map[string]string{
		settingsFile:         "default_port = 8080\n",
		"models/aaa.toml":    "backend = \"llama\"\nrepo = \"org/aaa\"\n",
		"models/broken.toml": "backend = \"llama\"\nrepo = \"org/broken\"\nctx = 16384\n",
		"models/zzz.toml":    "backend = \"mlx\"\nrepo = \"org/zzz\"\n",
	})

	tree, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var ids []string
	for _, entry := range tree.Entries {
		ids = append(ids, entry.ID)
	}
	if strings.Join(ids, ",") != "aaa,zzz" {
		t.Errorf("valid entries are %v, want [aaa zzz]", ids)
	}

	if len(tree.Broken) != 1 {
		t.Fatalf("broken entries are %+v, want exactly one", tree.Broken)
	}
	broken := tree.Broken[0]
	if broken.ID != "broken" {
		t.Errorf("broken entry id is %q, want %q", broken.ID, "broken")
	}
	if want := filepath.Join(root, "models", "broken.toml"); broken.Path != want {
		t.Errorf("broken entry path is %q, want %q", broken.Path, want)
	}
	var keyErr *KeyError
	if !errors.As(broken.Err, &keyErr) {
		t.Fatalf("broken entry error is %T (%v), want a *KeyError", broken.Err, broken.Err)
	}
	if keyErr.Key != "ctx" {
		t.Errorf("broken entry names key %q, want %q", keyErr.Key, "ctx")
	}
}

// A choice is validated per entry like everything else: an axis that fights the
// entry it lives in disables that file and names the key, while the entries
// around it load.
func TestLoadIsolatesABrokenChoice(t *testing.T) {
	root := writeTree(t, map[string]string{
		settingsFile:      "default_port = 8080\n",
		"models/aaa.toml": "backend = \"llama\"\nrepo = \"org/aaa\"\n",
		"models/broken.toml": "backend = \"llama\"\nrepo = \"org/broken\"\nargs = [\"--ctx-size\", \"16384\"]\n" +
			"[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n",
		"models/zzz.toml": "backend = \"llama\"\nrepo = \"org/zzz\"\n" +
			"[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n",
	})

	tree, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var ids []string
	for _, entry := range tree.Entries {
		ids = append(ids, entry.ID)
	}
	if strings.Join(ids, ",") != "aaa,zzz" {
		t.Errorf("valid entries are %v, want [aaa zzz]", ids)
	}
	if len(tree.Broken) != 1 || tree.Broken[0].ID != "broken" {
		t.Fatalf("broken entries are %+v, want only broken", tree.Broken)
	}
	var keyErr *KeyError
	if !errors.As(tree.Broken[0].Err, &keyErr) {
		t.Fatalf("broken entry error is %T (%v), want a *KeyError", tree.Broken[0].Err, tree.Broken[0].Err)
	}
	if keyErr.Key != "choice.option.args" {
		t.Errorf("broken entry names key %q, want %q", keyErr.Key, "choice.option.args")
	}
}

// An id is a filename, so its charset is enforced at discovery: a file cria
// cannot name is a broken entry, not a tree failure.
func TestLoadRejectsIdsOutsideTheCharset(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantOK   bool
	}{
		{name: "plain id", filename: "qwen3.toml", wantOK: true},
		{name: "dashes, underscores and dots", filename: "qwen3-30b_a3b.q4.toml", wantOK: true},
		{name: "digits only", filename: "30.toml", wantOK: true},
		{name: "a space", filename: "qwen 3.toml", wantOK: false},
		{name: "a colon", filename: "qwen3:q4.toml", wantOK: false},
		{name: "a non-ascii letter", filename: "café.toml", wantOK: false},
		{name: "an empty id", filename: ".toml", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				path.Join(entriesDir, test.filename): "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
			})
			tree, err := Load(root)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if test.wantOK {
				if len(tree.Entries) != 1 || len(tree.Broken) != 0 {
					t.Fatalf("%s loaded as %d valid and %d broken, want one valid", test.filename, len(tree.Entries), len(tree.Broken))
				}
				if want := strings.TrimSuffix(test.filename, tomlExt); tree.Entries[0].ID != want {
					t.Errorf("id is %q, want %q", tree.Entries[0].ID, want)
				}
				return
			}
			if len(tree.Entries) != 0 || len(tree.Broken) != 1 {
				t.Fatalf("%s loaded as %d valid and %d broken, want one broken", test.filename, len(tree.Entries), len(tree.Broken))
			}
			if !strings.Contains(tree.Broken[0].Err.Error(), "invalid entry id") {
				t.Errorf("broken entry error is %q, want it to name the invalid id", tree.Broken[0].Err)
			}
		})
	}
}

// Only <id>.toml files are entries; the tree may hold anything else without cria
// inventing entries from it.
func TestLoadReadsOnlyTomlFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"models/demo.toml":       "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
		"models/notes.md":        "# scratch notes, not an entry\n",
		"models/demo.toml.bak":   "backend = \"nonsense\"\n",
		"models/old/demo.toml":   "backend = \"nonsense\"\n",
		"AGENTS.md":              "# points agents at cria docs\n",
		"models/.demo.toml.swp":  "binary junk",
		"models/README":          "not an entry either\n",
		"models/.hidden.notoml":  "",
		"models/subdir/keep.txt": "",
	})

	tree, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(tree.Broken) != 0 {
		t.Errorf("broken entries are %+v, want none", tree.Broken)
	}
	if len(tree.Entries) != 1 || tree.Entries[0].ID != "demo" {
		t.Fatalf("entries are %+v, want only demo", tree.Entries)
	}
}

// A tree that does not exist yet is empty, not broken: creating it is the
// first-run scaffold's job, and Load only reads.
func TestLoadAcceptsAnAbsentTree(t *testing.T) {
	t.Run("no root at all", func(t *testing.T) {
		tree, err := Load(filepath.Join(t.TempDir(), "never-created"))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(tree.Entries) != 0 || len(tree.Broken) != 0 || tree.Settings != (Settings{}) {
			t.Errorf("tree is %+v, want an empty one", tree)
		}
	})

	t.Run("root without models directory", func(t *testing.T) {
		root := writeTree(t, map[string]string{settingsFile: "default_port = 8080\n"})
		tree, err := Load(root)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(tree.Entries) != 0 {
			t.Errorf("entries are %+v, want none", tree.Entries)
		}
		if tree.Settings.DefaultPort != 8080 {
			t.Errorf("default_port is %d, want 8080", tree.Settings.DefaultPort)
		}
	})
}

func TestLoadOrdersEntriesById(t *testing.T) {
	root := writeTree(t, map[string]string{
		settingsFile:        "default_port = 8080\n",
		"models/zeta.toml":  "backend = \"llama\"\nrepo = \"org/zeta\"\n",
		"models/alpha.toml": "backend = \"llama\"\nrepo = \"org/alpha\"\n",
		"models/mid.toml":   "backend = \"mlx\"\nrepo = \"org/mid\"\n",
	})

	tree, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var ids []string
	for _, entry := range tree.Entries {
		ids = append(ids, entry.ID)
	}
	if strings.Join(ids, ",") != "alpha,mid,zeta" {
		t.Errorf("entries are ordered %v, want [alpha mid zeta]", ids)
	}
}

func TestLoadRecordsEntryPath(t *testing.T) {
	root := writeTree(t, map[string]string{
		"models/demo.toml": "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
	})
	tree, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(tree.Entries) != 1 {
		t.Fatalf("entries are %+v, want one", tree.Entries)
	}
	if want := filepath.Join(root, "models", "demo.toml"); tree.Entries[0].Path != want {
		t.Errorf("entry path is %q, want %q", tree.Entries[0].Path, want)
	}
	if tree.Root != root {
		t.Errorf("tree root is %q, want %q", tree.Root, root)
	}
}

// A file the parser cannot read at all is still an entry-level failure — it just
// is not a *KeyError.
func TestLoadReportsUnparsableEntry(t *testing.T) {
	_, err := loadOne(t, "", "backend = \nrepo = \"org/name\"\n")
	if err == nil {
		t.Fatal("a syntactically invalid entry was accepted")
	}
	var keyErr *KeyError
	if errors.As(err, &keyErr) {
		t.Fatalf("error is a *KeyError (%v), want the parser's own failure", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error is %q, want it to point at the offending line", err)
	}
}

// A config.toml the parser cannot read fails the whole tree: every entry resolves
// against it, so there is nothing to isolate.
func TestLoadReportsUnparsableSettings(t *testing.T) {
	root := writeTree(t, map[string]string{
		settingsFile:       "default_port = \n",
		"models/demo.toml": "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
	})
	tree, err := Load(root)
	if err == nil {
		t.Fatalf("tree loaded as %+v, want the broken config.toml to fail the load", tree)
	}
	if tree != nil {
		t.Errorf("Load returned a tree alongside its error; a tree-level failure yields none")
	}
	if !strings.Contains(err.Error(), settingsFile) {
		t.Errorf("error is %q, want it to name %s", err, settingsFile)
	}
}

func TestRootIsUnderTheHomeConfigDirectory(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this host: %v", err)
	}
	if want := filepath.Join(home, ".config", "cria"); root != want {
		t.Errorf("Root is %q, want %q", root, want)
	}
}
