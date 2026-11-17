package hubcache

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A delete that unlinks files whose bytes live in hf's shared store runs
// through `hf cache rm` when hf is found (docs/specs/CACHE.md, Deletion). hf
// is never run here: fakeHF stands in for it, doing to the tree what hf 2.4's
// DeleteCacheStrategy does — snapshot entries out of every revision, repository
// blobs nothing else in the repository reaches, the store payloads left with no
// valid link — and nothing it does not: the snapshot directories it empties
// stay.
type fakeHF struct {
	t       *testing.T
	root    string
	program string     // the program execute ran
	args    [][]string // the arguments of each run
	stderr  string     // what a failing run says
	fail    error      // the run's error; nil for a clean exit
	idle    bool       // exit cleanly having removed nothing
}

func (f *fakeHF) run(_ context.Context, program string, args []string) ([]byte, error) {
	f.program = program
	f.args = append(f.args, args)
	if f.idle {
		return nil, nil
	}
	targets := args[5:]
	var swept []string
	for i, target := range targets {
		if f.fail != nil && i > 0 {
			break // a failing run stops after its first target
		}
		swept = append(swept, f.remove(target)...)
	}
	for _, payload := range swept {
		f.sweep(payload)
	}
	return []byte(f.stderr), f.fail
}

// remove takes one hf:// target out of the tree and names the store payloads
// whose links it unlinked.
func (f *fakeHF) remove(target string) []string {
	f.t.Helper()
	rest, ok := strings.CutPrefix(target, "hf://models/")
	if !ok {
		f.t.Fatalf("hf was handed %q, want an hf://models/ URI", target)
	}
	parts := strings.SplitN(rest, "/", 3)
	repo := filepath.Join(f.root, "models--"+parts[0]+"--"+parts[1])

	if len(parts) == 2 {
		var payloads []string
		entries, _ := os.ReadDir(filepath.Join(repo, "blobs"))
		for _, entry := range entries {
			if payload, managed, _ := storeTarget(f.root, filepath.Join(repo, "blobs", entry.Name())); managed {
				payloads = append(payloads, payload)
			}
		}
		if err := os.RemoveAll(repo); err != nil {
			f.t.Fatal(err)
		}
		return payloads
	}

	var unlinked []string
	revisions, _ := os.ReadDir(filepath.Join(repo, "snapshots"))
	for _, revision := range revisions {
		entry := filepath.Join(repo, "snapshots", revision.Name(), filepath.FromSlash(parts[2]))
		target, err := os.Readlink(entry)
		if err != nil {
			continue
		}
		unlinked = append(unlinked, filepath.Join(filepath.Dir(entry), target))
		if err := os.Remove(entry); err != nil {
			f.t.Fatal(err)
		}
	}
	reached := map[string]bool{}
	filepath.WalkDir(filepath.Join(repo, "snapshots"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type()&fs.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			reached[filepath.Clean(filepath.Join(filepath.Dir(path), target))] = true
		}
		return nil
	})
	var payloads []string
	for _, blob := range unlinked {
		blob = filepath.Clean(blob)
		if reached[blob] {
			continue
		}
		if payload, managed, _ := storeTarget(f.root, blob); managed {
			payloads = append(payloads, payload)
		}
		os.Remove(blob)
	}
	return payloads
}

// sweep collects a payload no valid link reaches any more, as hf does.
func (f *fakeHF) sweep(payload string) {
	if links, readable := manifestLinks(f.root, payload); readable && len(links) == 0 {
		os.Remove(payload)
		os.Remove(payload + ".refs")
	}
}

// manifestBytes is what the .refs manifests of stored files hold.
func manifestBytes(t *testing.T, root string, files ...cachedFile) int64 {
	t.Helper()
	var total int64
	for _, file := range files {
		info, err := os.Stat(payloadOf(root, file) + ".refs")
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	return total
}

// executeThrough runs a plan through the fake hf and holds it to what a delete
// promises: the bytes it reports are the bytes the tree lost.
func executeThrough(t *testing.T, root string, plan *Plan, hf *fakeHF) (int64, error) {
	t.Helper()
	before := diskUsage(t, root)
	reclaimed, err := execute(context.Background(), plan, nil, "/opt/homebrew/bin/hf", hf.run)
	if lost := before - diskUsage(t, root); reclaimed != lost {
		t.Errorf("the delete reports %d bytes reclaimed, but the tree lost %d", reclaimed, lost)
	}
	return reclaimed, err
}

// A quant hf can take goes to hf as one file URI per path it holds; hf frees
// the store's bytes, and cria removes the snapshot directories hf left empty.
func TestExecuteRunsAStoredQuantThroughHF(t *testing.T) {
	first := cachedFile{name: "Q8_0/Flash-Q8_0-00001-of-00002.gguf", size: 2000}
	second := cachedFile{name: "Q8_0/Flash-Q8_0-00002-of-00002.gguf", size: 3000}
	tree := newCacheTree(t)
	repo := tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", first, second).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	manifests := manifestBytes(t, tree.Root, first, second)
	hf := &fakeHF{t: t, root: tree.Root}
	reclaimed, err := executeThrough(t, tree.Root, plan, hf)
	if err != nil {
		t.Fatalf("executing the plan: %v", err)
	}

	want := [][]string{{"cache", "rm", "--cache-dir", tree.Root, "--yes",
		"hf://models/unsloth/Flash-GGUF/Q8_0/Flash-Q8_0-00001-of-00002.gguf",
		"hf://models/unsloth/Flash-GGUF/Q8_0/Flash-Q8_0-00002-of-00002.gguf"}}
	if hf.program != "/opt/homebrew/bin/hf" || !reflect.DeepEqual(hf.args, want) {
		t.Errorf("ran %s %q, want /opt/homebrew/bin/hf %q", hf.program, hf.args, want)
	}
	if want := 5000 + manifests; reclaimed != want {
		t.Errorf("the delete reclaimed %d bytes, want the %d of both stored shards and their manifests", reclaimed, want)
	}
	snapshots := filepath.Join(repo.dir, "snapshots")
	var held []string
	for path := range treeEntries(t, snapshots) {
		rel, _ := filepath.Rel(snapshots, path)
		held = append(held, filepath.ToSlash(rel))
	}
	if want := []string{".", "revision-one", "revision-one/Flash-UD-Q4_K_XL.gguf"}; !reflect.DeepEqual(sorted(held), want) {
		t.Errorf("the snapshots hold %v, want %v", sorted(held), want)
	}
}

// A whole repository goes to hf as its repository URI, and so does a quant
// that is the last thing its repository holds.
func TestExecuteRunsAStoredRepoThroughHFByItsURI(t *testing.T) {
	plans := map[string]func(t *testing.T, root string) *Plan{
		"repo": func(t *testing.T, root string) *Plan {
			plan, err := PlanRepo(repoOf(t, read(t, root), "unsloth/Flash-GGUF"), nil)
			if err != nil {
				t.Fatal(err)
			}
			return plan
		},
		"last quant": func(t *testing.T, root string) *Plan {
			return planQuant(t, root, "unsloth/Flash-GGUF", "Q8_0")
		},
	}
	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			tree := newCacheTree(t)
			tree.repo("models--unsloth--Flash-GGUF").
				stored("revision-one", cachedFile{name: "Flash-Q8_0.gguf", size: 2000}).
				main("revision-one")

			hf := &fakeHF{t: t, root: tree.Root}
			reclaimed, err := executeThrough(t, tree.Root, plan(t, tree.Root), hf)
			if err != nil {
				t.Fatalf("executing the plan: %v", err)
			}
			want := [][]string{{"cache", "rm", "--cache-dir", tree.Root, "--yes", "hf://models/unsloth/Flash-GGUF"}}
			if !reflect.DeepEqual(hf.args, want) {
				t.Errorf("ran hf %q, want %q", hf.args, want)
			}
			if reclaimed < 2000 {
				t.Errorf("the delete reclaimed %d bytes, want at least the 2000 stored", reclaimed)
			}
		})
	}
}

// A run hf fails carries its stderr, and what it did free before it stopped
// is measured and reported with the failure.
func TestExecuteReportsAFailedHFRun(t *testing.T) {
	first := cachedFile{name: "Flash-Q8_0-00001-of-00002.gguf", size: 2000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", first, cachedFile{name: "Flash-Q8_0-00002-of-00002.gguf", size: 3000}).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	manifest := manifestBytes(t, tree.Root, first)
	hf := &fakeHF{t: t, root: tree.Root, fail: errors.New("exit status 1"),
		stderr: "Traceback (most recent call last):\n  ...\nPermissionError: [Errno 13] Permission denied\n"}
	reclaimed, err := executeThrough(t, tree.Root, plan, hf)

	var failed *HFError
	if !errors.As(err, &failed) {
		t.Fatalf("the execute answered %v, want an *HFError", err)
	}
	if want := "Traceback (most recent call last): · ... · PermissionError: [Errno 13] Permission denied"; failed.Stderr != want {
		t.Errorf("the failure carries %q, want hf's stderr %q", failed.Stderr, want)
	}
	if want := 2000 + manifest; reclaimed != want {
		t.Errorf("the failed delete reports %d bytes, want the %d hf freed before it stopped", reclaimed, want)
	}
}

// hf exits cleanly for targets it did not find — a repository it skips as
// damaged among them — so a run that left the delete in place is a failure
// the tree reveals.
func TestExecuteRefusesAnHFRunThatLeftTheDeleteInPlace(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", cachedFile{name: "Flash-Q8_0.gguf", size: 2000}).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	reclaimed, err := executeThrough(t, tree.Root, plan, &fakeHF{t: t, root: tree.Root, idle: true})
	if err == nil || !strings.Contains(err.Error(), "hf cache rm left 2 of the 2 paths of unsloth/Flash-GGUF:Q8_0 in place") {
		t.Errorf("the execute answered %v, want it to say hf left the delete in place", err)
	}
	if reclaimed != 0 {
		t.Errorf("the delete reports %d bytes reclaimed, want none", reclaimed)
	}
}

// What hf cannot or need not take stays cria's own delete, hf found or not:
// superseded copies, unfinished downloads, and anything with no link into the
// store.
func TestOnlyDeletesThroughTheStoreGoToHF(t *testing.T) {
	plans := map[string]func(t *testing.T, tree *cacheTree) *Plan{
		"superseded stored copies": func(t *testing.T, tree *cacheTree) *Plan {
			tree.repo("models--unsloth--Flash-GGUF").
				stored("revision-old", cachedFile{name: "Flash-Q8_0.gguf", size: 2000}).
				stored("revision-new", cachedFile{name: "Flash-Q8_0.gguf", text: "the re-uploaded Q8_0"}).
				main("revision-new")
			plan, err := PlanSuperseded(repoOf(t, read(t, tree.Root), "unsloth/Flash-GGUF"), "Q8_0", nil)
			if err != nil {
				t.Fatal(err)
			}
			return plan
		},
		"unfinished downloads": func(t *testing.T, tree *cacheTree) *Plan {
			tree.repo("models--unsloth--Flash-GGUF").
				stored("revision-one", cachedFile{name: "Flash-Q8_0.gguf", size: 2000}).
				partial("abc.incomplete", 700).
				main("revision-one")
			plan, err := PlanPartials(repoOf(t, read(t, tree.Root), "unsloth/Flash-GGUF"), nil)
			if err != nil {
				t.Fatal(err)
			}
			return plan
		},
		"a quant outside the store": func(t *testing.T, tree *cacheTree) *Plan {
			tree.repo("models--unsloth--Flash-GGUF").
				snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
				stored("revision-one", cachedFile{name: "Flash-Q8_0.gguf", size: 2000}).
				main("revision-one")
			return planQuant(t, tree.Root, "unsloth/Flash-GGUF", "UD-Q4_K_XL")
		},
	}
	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			tree := newCacheTree(t)
			made := plan(t, tree)
			if made.HFTargets != nil {
				t.Errorf("the plan hands hf %q, want nothing", made.HFTargets)
			}
			hf := &fakeHF{t: t, root: tree.Root}
			if _, err := executeThrough(t, tree.Root, made, hf); err != nil {
				t.Fatalf("executing the plan: %v", err)
			}
			if hf.args != nil {
				t.Errorf("the delete ran hf %q, want it cria's own", hf.args)
			}
		})
	}
}

// runHF hands back what the program wrote to stderr and how it exited.
func TestRunHFCarriesStderrAndTheExit(t *testing.T) {
	stderr, err := runHF(context.Background(), "/bin/sh", []string{"-c", "echo out; echo boom >&2; exit 3"})
	if string(stderr) != "boom\n" {
		t.Errorf("stderr reads %q, want %q", stderr, "boom\n")
	}
	if err == nil || err.Error() != "exit status 3" {
		t.Errorf("the run answered %v, want exit status 3", err)
	}
}

// A failure's stderr is its tail on one line, a token masked.
func TestLastWordsMasksATokenOnOneLine(t *testing.T) {
	got := lastWords([]byte("\nusing hf_abcdefghijklmnopqrstuvwxyz0123\n\nError: denied\n"))
	if want := "using hf_*** · Error: denied"; got != want {
		t.Errorf("the failure carries %q, want %q", got, want)
	}
}
