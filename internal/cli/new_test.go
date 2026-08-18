package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/format"
)

// `cria new` is the one subcommand that writes, so its tests drive a real config
// tree in a temp directory rather than the fixed fake the rest of the package
// uses: the file it creates is the file the next load reads, and that round trip
// is what the subcommand is for.
func scaffolded(t *testing.T) (*app, string, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "cria")
	if err := config.Scaffold(root); err != nil {
		t.Fatalf("cannot scaffold a config tree: %v", err)
	}
	app, out, errOut := newTestApp(nil, &fakeServers{})
	app.tree = func() (*config.Tree, error) { return config.Load(root) }
	return app, root, out, errOut
}

// entryPath is where an id's file belongs in a tree.
func entryPath(root, id string) string {
	return filepath.Join(root, entriesDirName, id+".toml")
}

// read is a file the subcommand was supposed to have written.
func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(content)
}

// writingEditor is a stand-in editor that saves content over the file it is
// handed — what a person does between `cria new` writing the template and the
// verdict being read.
func writingEditor(t *testing.T, content string) string {
	t.Helper()
	program := filepath.Join(t.TempDir(), "editor")
	body := "#!/bin/sh\ncat > \"$1\" <<'ENTRY'\n" + content + "ENTRY\n"
	if err := os.WriteFile(program, []byte(body), 0o755); err != nil {
		t.Fatalf("cannot write the stand-in editor: %v", err)
	}
	return program
}

// The file `cria new` writes is the example for its backend, whole: the same
// commented template `cria docs` prints, keys, rules and all.
func TestNewWritesTheBackendsExample(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		backend config.Backend
	}{
		{name: "llama is what a bare invocation takes", args: []string{"qwen"}, backend: config.BackendLlama},
		{name: "--llama says the same thing out loud", args: []string{"qwen", llamaFlag}, backend: config.BackendLlama},
		{name: "--mlx takes the other backend", args: []string{"qwen", mlxFlag}, backend: config.BackendMLX},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			noEditor(t)
			app, root, out, errOut := scaffolded(t)

			if code := app.newEntry(test.args); code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
			}

			path := entryPath(root, "qwen")
			if !strings.Contains(out.String(), "created "+path) {
				t.Errorf("cria printed %q, want the file it created", out)
			}
			if content := read(t, path); content != config.ExampleEntry(test.backend) {
				t.Errorf("the created file is not the %q example:\n%s", test.backend, content)
			}

			// quant is the key one backend alone takes, so it is the visible
			// difference between the two templates.
			hasQuant := strings.Contains(read(t, path), "quant = ")
			if hasQuant != (test.backend == config.BackendLlama) {
				t.Errorf("the %q file %s a quant key", test.backend, map[bool]string{true: "holds", false: "lacks"}[hasQuant])
			}
		})
	}
}

// The one-source rule, proved from outside the config package: what `cria new`
// hands someone is a passage of what `cria docs` teaches, not a second template.
func TestNewWritesWhatDocsTeaches(t *testing.T) {
	noEditor(t)
	app, root, _, errOut := scaffolded(t)

	if code := app.newEntry([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if content := read(t, entryPath(root, "qwen")); !strings.Contains(config.Docs(), content) {
		t.Errorf("`cria docs` does not hold the file `cria new` wrote:\n%s", content)
	}
}

// A fresh entry file is one an editor opens: with none set the creation still
// stands, and the note names what would have opened it.
func TestNewWithoutAnEditorKeepsTheFile(t *testing.T) {
	noEditor(t)
	app, root, out, errOut := scaffolded(t)

	if code := app.newEntry([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if content := read(t, entryPath(root, "qwen")); content != config.ExampleEntry(config.BackendLlama) {
		t.Errorf("the created file is not the llama example:\n%s", content)
	}
	if !strings.Contains(out.String(), "created ") {
		t.Errorf("cria printed %q, want the file it created", out)
	}
	if !strings.Contains(errOut.String(), "set $EDITOR (or $VISUAL)") {
		t.Errorf("cria printed %q, want the variable to set", errOut)
	}
}

// With an editor set, the new file is what it is handed — and when it closes,
// the tree is read back: the entry it now declares, and the command that proves
// it serves.
func TestNewOpensTheEditorAndReportsTheEntry(t *testing.T) {
	noEditor(t)
	program, arguments := editorScript(t, 0)
	t.Setenv(editorEnv, program)
	app, root, out, errOut := scaffolded(t)

	if code := app.newEntry([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	path := entryPath(root, "qwen")
	if got := opened(t, arguments); len(got) != 1 || got[0] != path {
		t.Errorf("the editor was handed %v, want the file cria created", got)
	}

	// The verdict is read out of the tree rather than spelled here: the example's
	// repo is the schema's business, not this test's.
	tree, err := config.Load(root)
	if err != nil {
		t.Fatalf("cannot load the tree cria wrote into: %v", err)
	}
	if len(tree.Entries) != 1 {
		t.Fatalf("the tree holds %d entries, want the one cria created", len(tree.Entries))
	}
	entry := tree.Entries[0]
	want := "qwen: llama " + format.HubReference(entry.Repo, entry.Quant) + " — start it: cria start qwen --wait"
	if !strings.Contains(out.String(), want) {
		t.Errorf("cria printed %q, want the verdict %q", out, want)
	}
}

// An entry the editor left broken is reported with the key that disables it: the
// file is on disk, and it does not serve.
func TestNewReportsAnEntryTheEditorBroke(t *testing.T) {
	noEditor(t)
	t.Setenv(editorEnv, writingEditor(t, "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nnonsense = true\n"))
	app, root, _, errOut := scaffolded(t)

	if code := app.newEntry([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{`key "nonsense"`, "fix it: cria edit qwen"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
	if content := read(t, entryPath(root, "qwen")); !strings.Contains(content, "nonsense") {
		t.Errorf("the file the editor saved is not the one on disk:\n%s", content)
	}
}

// An editor that exits non-zero is reported and the file is kept: cria did not
// write what is in it and cannot say what state it is in.
func TestNewKeepsTheFileWhenTheEditorFails(t *testing.T) {
	noEditor(t)
	program, _ := editorScript(t, 3)
	t.Setenv(editorEnv, program)
	app, root, _, errOut := scaffolded(t)

	if code := app.newEntry([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "exited 3") {
		t.Errorf("cria printed %q, want the editor's exit code", errOut)
	}
	if content := read(t, entryPath(root, "qwen")); content != config.ExampleEntry(config.BackendLlama) {
		t.Errorf("the created file was not kept as written:\n%s", content)
	}
}

// An id whose file exists is refused, and the file is not touched: cria creates
// entries, it never rewrites them. A refused entry file counts — it is a file
// someone wrote, and editing it is the point.
func TestNewRefusesAnIdThatAlreadyExists(t *testing.T) {
	cases := map[string]string{
		"an entry that loads":   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
		"an entry cria refuses": "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nctx = 16384\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			noEditor(t)
			app, root, _, errOut := scaffolded(t)
			path := entryPath(root, "qwen")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("cannot write the entry that is already there: %v", err)
			}

			if code := app.newEntry([]string{"qwen"}); code != exitFailure {
				t.Fatalf("exit code %d, want %d", code, exitFailure)
			}
			for _, want := range []string{path, "already exists", "cria edit qwen"} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if content := read(t, path); content != body {
				t.Errorf("the file that was already there changed to:\n%s", content)
			}
		})
	}
}

// An id the loader could not read back is refused before anything is written: a
// file cria creates must be one it can find again.
func TestNewRefusesAnIdItCouldNotReadBack(t *testing.T) {
	noEditor(t)
	for _, id := range []string{"qwen/3", "qwen 3", "qwen:q4", ""} {
		app, root, _, errOut := scaffolded(t)
		if code := app.newEntry([]string{id}); code != exitFailure {
			t.Errorf("`cria new %q` exited %d, want %d", id, code, exitFailure)
		}
		if !strings.Contains(errOut.String(), "cannot name an entry") {
			t.Errorf("cria printed %q, want the id rule", errOut)
		}
		if names := entryNames(t, root); len(names) != 0 {
			t.Errorf("`cria new %q` created %v, want nothing written", id, names)
		}
	}
}

// The entries directory is the scaffold's to create on every run, so a missing
// one is reported by name rather than papered over.
func TestNewReportsAMissingEntriesDirectory(t *testing.T) {
	noEditor(t)
	app, root, _, errOut := scaffolded(t)
	entries := filepath.Join(root, entriesDirName)
	if err := os.RemoveAll(entries); err != nil {
		t.Fatalf("cannot remove %s: %v", entries, err)
	}

	if code := app.newEntry([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), entries) {
		t.Errorf("cria printed %q, want the directory that is missing", errOut)
	}
}

// The subcommand takes one id and at most one backend flag; the two flags name
// different backends, so together they are not an invocation cria can route.
func TestNewRefusesWhatItCannotRoute(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "no id", args: nil, contains: "no entry named"},
		{name: "only a flag", args: []string{mlxFlag}, contains: "no entry named"},
		{name: "two ids", args: []string{"qwen", "gemma"}, contains: "one entry at a time (got qwen, gemma)"},
		{name: "an unknown flag", args: []string{"qwen", "--backend"}, contains: "unknown flag --backend"},
		{name: "both backends", args: []string{"qwen", llamaFlag, mlxFlag}, contains: "name different backends"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			noEditor(t)
			app, root, _, errOut := scaffolded(t)

			if code := app.newEntry(test.args); code != exitUsage {
				t.Errorf("exit code %d, want %d", code, exitUsage)
			}
			for _, want := range []string{test.contains, "usage: cria new <id> [--llama|--mlx]"} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if names := entryNames(t, root); len(names) != 0 {
				t.Errorf("an unroutable command line created %v, want nothing written", names)
			}
		})
	}
}

// entryNames is what the tree's models/ directory holds.
func entryNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, entriesDirName))
	if err != nil {
		t.Fatalf("cannot read the entries directory: %v", err)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
