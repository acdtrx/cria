package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A first run makes the tree cria reads: the root, models/ and AGENTS.md, and
// nothing else — config.toml and the entries are written from `cria docs`.
func TestScaffoldCreatesTheTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cria")
	if err := Scaffold(root); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	names := dirNames(t, root)
	if strings.Join(names, ",") != agentsFile+","+entriesDir {
		t.Errorf("the tree holds %v, want only %s and %s/", names, agentsFile, entriesDir)
	}
	if entries := dirNames(t, filepath.Join(root, entriesDir)); len(entries) != 0 {
		t.Errorf("%s/ holds %v, want an empty directory", entriesDir, entries)
	}

	page, err := os.ReadFile(filepath.Join(root, agentsFile))
	if err != nil {
		t.Fatalf("cannot read the scaffolded %s: %v", agentsFile, err)
	}
	if string(page) != string(agentsPage) {
		t.Errorf("the scaffolded %s is not the page carried in the binary", agentsFile)
	}
}

// Scaffold runs on every invocation, so a run after the first must be a no-op:
// nothing created, nothing rewritten, no timestamp moved.
func TestScaffoldTouchesNothingOnASecondRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cria")
	if err := Scaffold(root); err != nil {
		t.Fatalf("first Scaffold: %v", err)
	}
	before := treeState(t, root)

	time.Sleep(10 * time.Millisecond) // any write now would land on a later timestamp
	if err := Scaffold(root); err != nil {
		t.Fatalf("second Scaffold: %v", err)
	}

	after := treeState(t, root)
	for path, state := range before {
		switch got, ok := after[path]; {
		case !ok:
			t.Errorf("%s disappeared on the second run", path)
		case got != state:
			t.Errorf("%s changed on the second run: %+v, was %+v", path, got, state)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("the second run created %s", path)
		}
	}
}

// The tree belongs to whoever writes it: an AGENTS.md the user has rewritten
// survives untouched, and so does everything else they put there.
func TestScaffoldKeepsFilesThatExist(t *testing.T) {
	const ownPage = "# my own notes\n\nnothing cria wrote\n"
	root := writeTree(t, map[string]string{
		agentsFile:           ownPage,
		settingsFile:         "default_port = 8080\n",
		"models/qwen3.toml":  "backend = \"llama\"\nrepo = \"org/name\"\n",
		"models/keep-me.txt": "not an entry\n",
	})
	before := treeState(t, root)

	time.Sleep(10 * time.Millisecond) // any write now would land on a later timestamp
	if err := Scaffold(root); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	page, err := os.ReadFile(filepath.Join(root, agentsFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", agentsFile, err)
	}
	if string(page) != ownPage {
		t.Errorf("%s was rewritten to:\n%s\nwant it left as:\n%s", agentsFile, page, ownPage)
	}
	for path, state := range before {
		if got := treeState(t, root)[path]; got != state {
			t.Errorf("%s changed: %+v, was %+v", path, got, state)
		}
	}
}

// A scaffolded tree is a valid empty tree: the first run leaves the loader with
// nothing to complain about.
func TestScaffoldedTreeLoadsEmpty(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cria")
	if err := Scaffold(root); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	tree, err := Load(root)
	if err != nil {
		t.Fatalf("Load of a scaffolded tree: %v", err)
	}
	if len(tree.Entries) != 0 || len(tree.Broken) != 0 {
		t.Errorf("the scaffolded tree loaded as %+v, want an empty one", tree)
	}
}

// AGENTS.md is the agent's entry point (docs/cria.md, principle 5): it must send
// them to the binary for the schema and to the lifecycle loop for validation.
func TestAgentsPagePointsAtTheBinary(t *testing.T) {
	page := string(agentsPage)
	for _, want := range []string{
		"cria docs",
		"cria start <id> --wait",
		"cria status --json",
		"cria stop <id>",
		entriesDir + "/<id>.toml",
		settingsFile,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("%s never mentions %q", agentsFile, want)
		}
	}
}

// fileState is what "untouched" means for one path: same content, same size, same
// modification time.
type fileState struct {
	dir      bool
	size     int64
	modified time.Time
	content  string
}

// treeState reads a whole tree into comparable state, keyed by path relative to
// the root.
func treeState(t *testing.T, root string) map[string]fileState {
	t.Helper()
	state := map[string]fileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entry := fileState{dir: info.IsDir(), size: info.Size(), modified: info.ModTime()}
		if !info.IsDir() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry.content = string(content)
		}
		state[relative] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read the tree at %s: %v", root, err)
	}
	return state
}

// dirNames lists the names a directory holds, in the order os.ReadDir yields
// them: sorted, so a caller can compare them as one string.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
