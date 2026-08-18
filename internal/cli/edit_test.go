package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cria/internal/config"
)

// editorScript writes a stand-in editor: it records the arguments it was handed
// and exits with the code the test asked for. The real exec is what these tests
// drive — an editor is a program cria hands the terminal to, and a seam in front
// of it would test everything about `cria edit` except that.
func editorScript(t *testing.T, exitCode int) (program, arguments string) {
	t.Helper()
	dir := t.TempDir()
	arguments = filepath.Join(dir, "arguments")
	program = filepath.Join(dir, "editor")
	// Single quotes: a temp directory named after the subtest can hold a '$', and
	// the shell would expand it inside double quotes.
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\nexit %d\n", arguments, exitCode)
	if err := os.WriteFile(program, []byte(body), 0o755); err != nil {
		t.Fatalf("cannot write the stand-in editor: %v", err)
	}
	return program, arguments
}

// opened is what the stand-in editor was handed, one argument per line.
func opened(t *testing.T, arguments string) []string {
	t.Helper()
	recorded, err := os.ReadFile(arguments)
	if err != nil {
		t.Fatalf("the editor recorded nothing: %v", err)
	}
	return strings.Split(strings.TrimRight(string(recorded), "\n"), "\n")
}

// noEditor clears both variables, so a test's own shell environment cannot open
// a real editor in the middle of the suite.
func noEditor(t *testing.T) {
	t.Helper()
	t.Setenv(visualEnv, "")
	t.Setenv(editorEnv, "")
}

// `cria edit <id>` opens that entry's file, in $VISUAL when it is set and in
// $EDITOR otherwise — the order every tool that opens an editor uses.
func TestEditOpensTheEntryFile(t *testing.T) {
	cases := []struct {
		name    string
		visual  bool
		wantArg string
	}{
		{name: "$VISUAL is preferred", visual: true},
		{name: "$EDITOR is the fallback"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			noEditor(t)
			program, arguments := editorScript(t, 0)
			if test.visual {
				t.Setenv(visualEnv, program)
				t.Setenv(editorEnv, "/nonexistent/editor")
			} else {
				t.Setenv(editorEnv, program)
			}
			app, _, errOut := newTestApp(testTree(), &fakeServers{})

			if code := app.edit([]string{"qwen"}); code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
			}
			if got := opened(t, arguments); len(got) != 1 || got[0] != "/home/u/.config/cria/models/qwen.toml" {
				t.Errorf("the editor was handed %v, want the entry's own file", got)
			}
		})
	}
}

// An editor named with arguments is run with them: `EDITOR="code -w"` is a whole
// invocation, not a program name.
func TestEditPassesTheEditorsOwnArguments(t *testing.T) {
	noEditor(t)
	program, arguments := editorScript(t, 0)
	t.Setenv(editorEnv, program+" -w")
	app, _, errOut := newTestApp(testTree(), &fakeServers{})

	if code := app.edit([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if got := opened(t, arguments); len(got) != 2 || got[0] != "-w" {
		t.Errorf("the editor was handed %v, want its own flag before the file", got)
	}
}

// A refused entry is editable, and that is the point: the file that fails to
// parse is the one someone needs to open.
func TestEditOpensARefusedEntry(t *testing.T) {
	noEditor(t)
	program, arguments := editorScript(t, 0)
	t.Setenv(editorEnv, program)

	tree := testTree()
	tree.Broken = []config.BrokenEntry{{
		ID:   "gemma",
		Path: "/home/u/.config/cria/models/gemma.toml",
		Err:  &config.KeyError{Key: "port", Reason: "required"},
	}}
	app, _, errOut := newTestApp(tree, &fakeServers{})

	if code := app.edit([]string{"gemma"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if got := opened(t, arguments); len(got) != 1 || got[0] != "/home/u/.config/cria/models/gemma.toml" {
		t.Errorf("the editor was handed %v, want the refused entry's file", got)
	}
}

// An editor that exits non-zero is reported, with the file to look at: cria did
// not write it and cannot say what state it is in.
func TestEditPropagatesTheEditorsExitCode(t *testing.T) {
	noEditor(t)
	program, _ := editorScript(t, 3)
	t.Setenv(editorEnv, program)
	app, _, errOut := newTestApp(testTree(), &fakeServers{})

	if code := app.edit([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"exited 3", "models/qwen.toml"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
}

// An editor that cannot be run at all is a different failure, and says so.
func TestEditReportsAnEditorItCannotRun(t *testing.T) {
	noEditor(t)
	t.Setenv(editorEnv, filepath.Join(t.TempDir(), "not-installed"))
	app, _, errOut := newTestApp(testTree(), &fakeServers{})

	if code := app.edit([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "cannot run") || !strings.Contains(errOut.String(), "not-installed") {
		t.Errorf("cria printed %q, want the editor it could not run", errOut)
	}
}

// With neither variable set there is nothing to open the file with, and the
// refusal names the one thing that clears it.
func TestEditRefusesWithNoEditorSet(t *testing.T) {
	noEditor(t)
	app, _, errOut := newTestApp(testTree(), &fakeServers{})

	if code := app.edit([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "set $EDITOR (or $VISUAL) to use cria edit") {
		t.Errorf("cria printed %q, want the variable to set", errOut)
	}
}

// An id that names no file lists the ids that exist, refused ones included:
// those are editable too.
func TestEditRefusesAnUnknownEntry(t *testing.T) {
	noEditor(t)
	program, _ := editorScript(t, 0)
	t.Setenv(editorEnv, program)

	tree := testTree()
	tree.Broken = []config.BrokenEntry{{ID: "gemma", Path: "/home/u/.config/cria/models/gemma.toml", Err: &config.KeyError{Key: "port", Reason: "required"}}}
	app, _, errOut := newTestApp(tree, &fakeServers{})

	if code := app.edit([]string{"qwn"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), `no entry named "qwn"`) || !strings.Contains(errOut.String(), "available entries: qwen, gemma") {
		t.Errorf("cria printed %q, want the unknown id and every id that exists", errOut)
	}
}

// Edit takes exactly one id.
func TestEditRefusesWhatItCannotRoute(t *testing.T) {
	noEditor(t)
	for _, args := range [][]string{nil, {"qwen", "gemma"}, {"--all"}} {
		app, _, errOut := newTestApp(testTree(), &fakeServers{})
		if code := app.edit(args); code != exitUsage {
			t.Errorf("`cria edit %v` exited %d, want %d", args, code, exitUsage)
		}
		if !strings.Contains(errOut.String(), "usage: cria edit <id>") {
			t.Errorf("cria printed %q, want the usage line", errOut)
		}
	}
}
