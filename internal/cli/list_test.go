package cli

import (
	"strings"
	"testing"

	"cria/internal/config"
)

// listedTree is a tree whose ids and models are of different lengths, so the
// columns have something to line up.
func listedTree() *config.Tree {
	tree := testTree()
	tree.Entries = append(tree.Entries, config.Entry{
		ID:      "gemma",
		Path:    "/home/u/.config/cria/models/gemma.toml",
		Backend: config.BackendMLX,
		Repo:    "mlx-community/gemma-3-27b-it-4bit",
		Host:    "0.0.0.0",
		Port:    8081,
		Name:    "Gemma 3 27B",
	})
	return tree
}

// `cria list` is one line per entry, in columns: the id, the backend, the model
// reference and the port. The columns are padded to their widest cell, so a
// listing is read down a column rather than parsed.
func TestListPrintsOneAlignedLinePerEntry(t *testing.T) {
	app, out, errOut := newTestApp(listedTree(), &fakeServers{})

	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	want := []string{
		"qwen   llama  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL  8080",
		"gemma  mlx    mlx-community/gemma-3-27b-it-4bit      8081",
	}
	if len(lines) != len(want) {
		t.Fatalf("cria printed %d lines:\n%s\nwant %d", len(lines), out, len(want))
	}
	for i, line := range lines {
		if line != want[i] {
			t.Errorf("line %d is %q, want %q", i+1, line, want[i])
		}
	}
}

// --paths adds the file each entry was read from, as the last column: the answer
// to "which file do I edit" without opening the tree.
func TestListWithPathsNamesEveryFile(t *testing.T) {
	app, out, errOut := newTestApp(listedTree(), &fakeServers{})

	if code := app.list([]string{pathsFlag}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	for _, want := range []string{
		"qwen   llama  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL  8080  /home/u/.config/cria/models/qwen.toml",
		"gemma  mlx    mlx-community/gemma-3-27b-it-4bit      8081  /home/u/.config/cria/models/gemma.toml",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed\n%s\nwant a line %q", out, want)
		}
	}
}

// A refused entry is listed too, after the ones that loaded and with the key
// that failed: it disables only itself, and the author needs to see it named
// (docs/specs/CONFIG.md). The listing still succeeds — the tree was read.
func TestListReportsRefusedEntries(t *testing.T) {
	tree := listedTree()
	tree.Broken = []config.BrokenEntry{{
		ID:   "mistral",
		Path: "/home/u/.config/cria/models/mistral.toml",
		Err:  &config.KeyError{Key: "port", Reason: "required: this entry sets no port and config.toml sets no default_port"},
	}}
	app, out, errOut := newTestApp(tree, &fakeServers{})

	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "mistral  refused: ") || !strings.Contains(last, `key "port"`) {
		t.Errorf("the refused entry reads %q, want it named with its offending key, last", last)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("cria coloured a plain listing: %q", out)
	}

	app, out, _ = newTestApp(tree, &fakeServers{})
	if code := app.list([]string{pathsFlag}); code != exitOK {
		t.Fatalf("exit code %d with %s, want %d", code, pathsFlag, exitOK)
	}
	if !strings.Contains(out.String(), "models/mistral.toml") {
		t.Errorf("cria printed\n%s\nwant the refused entry's file with %s", out, pathsFlag)
	}
}

// An empty tree is a true answer to "what is declared", so it exits zero — and
// says where entries go and what writes one.
func TestListOnAnEmptyTree(t *testing.T) {
	tree := &config.Tree{Root: "/home/u/.config/cria"}
	app, out, errOut := newTestApp(tree, &fakeServers{})

	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	for _, want := range []string{"/home/u/.config/cria/models", "cria docs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed %q, want it to name %q", out, want)
		}
	}
}

// The listing routes like every other subcommand: it takes one flag and no
// arguments.
func TestListRefusesWhatItCannotRoute(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "an unknown flag", args: []string{"--all"}, contains: "unknown flag --all"},
		{name: "an argument", args: []string{"qwen"}, contains: "takes no arguments (got qwen)"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app, _, errOut := newTestApp(listedTree(), &fakeServers{})
			if code := app.list(test.args); code != exitUsage {
				t.Errorf("exit code %d, want %d", code, exitUsage)
			}
			if !strings.Contains(errOut.String(), test.contains) {
				t.Errorf("cria printed %q, want it to contain %q", errOut, test.contains)
			}
		})
	}
}
