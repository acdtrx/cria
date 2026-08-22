package cli

import (
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/picks"
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

// An entry with choices carries its axes under its row: one line per choice, its
// options in file order with the current pick marked. That is what an agent
// reads to learn what `cria start <id> choice=option` may name, and what a bare
// start would launch (docs/specs/CLI.md).
func TestListShowsEachEntrysChoices(t *testing.T) {
	tree := listedTree()
	tree.Entries = append(tree.Entries, pickyEntry())

	// Nothing picked yet: the config default — each axis's first option — is what
	// a start would compose with.
	app, out, errOut := newTestApp(tree, &fakeServers{})
	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	lines := printedLines(out.String())
	if len(lines) != 5 {
		t.Fatalf("cria printed %d lines:\n%s\nwant three entries and two axes", len(lines), out)
	}
	if !strings.HasPrefix(lines[2], "qwen-choices  llama  unsloth/Qwen3-30B-A3B-GGUF ") {
		t.Errorf("the entry's row is %q, want it aligned with the rest of the listing", lines[2])
	}
	for i, want := range []string{"  quant: q4* q6", "  layout: chat* coding"} {
		if lines[3+i] != want {
			t.Errorf("axis line %d is %q, want %q", i+1, lines[3+i], want)
		}
	}

	// A stored pick is what is marked — it is what a bare start composes with —
	// and the axes it does not name keep their defaults.
	app, out, errOut = newTestApp(tree, &fakeServers{})
	app.picksStore = func() (picks.Picks, error) {
		return picks.Picks{"qwen-choices": {"layout": "coding"}}, nil
	}
	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	lines = printedLines(out.String())
	for i, want := range []string{"  quant: q4* q6", "  layout: chat coding*"} {
		if lines[3+i] != want {
			t.Errorf("axis line %d is %q, want %q", i+1, lines[3+i], want)
		}
	}

	// A stored pick the entry no longer holds is stale, not an error: the config
	// default is marked, because that is what would launch.
	app, out, errOut = newTestApp(tree, &fakeServers{})
	app.picksStore = func() (picks.Picks, error) {
		return picks.Picks{"qwen-choices": {"quant": "q8"}}, nil
	}
	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if lines = printedLines(out.String()); lines[3] != "  quant: q4* q6" {
		t.Errorf("the axis reads %q, want the config default marked", lines[3])
	}
}

// A tree of flat entries is listed exactly as it always was, and without going
// near cria's state: there is no axis to mark a pick on.
func TestListOfFlatEntriesReadsNoPicks(t *testing.T) {
	app, out, errOut := newTestApp(listedTree(), &fakeServers{})
	read := 0
	app.picksStore = func() (picks.Picks, error) {
		read++
		return picks.Picks{}, nil
	}

	if code := app.list(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if read != 0 {
		t.Errorf("cria read the picks store %d time(s) for a tree with no choices", read)
	}
	if lines := printedLines(out.String()); len(lines) != 2 {
		t.Errorf("cria printed %d lines:\n%s\nwant one per entry", len(lines), out)
	}
}

// printedLines is a listing as the lines it is read down.
func printedLines(printed string) []string {
	return strings.Split(strings.TrimRight(printed, "\n"), "\n")
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
