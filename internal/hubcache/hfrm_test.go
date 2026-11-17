package hubcache

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
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
	hang    bool       // run until killed, as a hung hf does
}

func (f *fakeHF) run(ctx context.Context, program string, args []string) ([]byte, error) {
	f.program = program
	f.args = append(f.args, args)
	if f.hang {
		<-ctx.Done()
		return []byte(f.stderr), errors.New("signal: killed")
	}
	if f.idle {
		return []byte(f.stderr), nil
	}
	targets := args[5:]
	stderr := f.stderr
	var swept []string
	for i, target := range targets {
		if f.fail != nil && i > 0 {
			break // a failing run stops after its first target
		}
		payloads, found := f.remove(target)
		if !found {
			stderr += "Could not find in cache:\n  - " + target + "\n"
		}
		swept = append(swept, payloads...)
	}
	for _, payload := range swept {
		f.sweep(payload)
	}
	return []byte(stderr), f.fail
}

// damaged reports what hf's scan reports as a corrupted repository, which it
// then leaves out of the cache it sees: a ref naming a snapshot that is not
// there.
func (f *fakeHF) damaged(repo string) bool {
	damaged := false
	filepath.WalkDir(filepath.Join(repo, "refs"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		revision, _ := os.ReadFile(path)
		if _, err := os.Stat(filepath.Join(repo, "snapshots", string(revision))); err != nil {
			damaged = true
		}
		return nil
	})
	return damaged
}

// remove takes one hf:// target out of the tree and names the store payloads
// whose links it unlinked; a target in a repository hf skips is not found.
func (f *fakeHF) remove(target string) ([]string, bool) {
	f.t.Helper()
	kind, rest, _ := strings.Cut(strings.TrimPrefix(target, "hf://"), "/")
	if !strings.HasPrefix(target, "hf://") || !slices.Contains([]string{"models", "datasets", "spaces"}, kind) {
		f.t.Fatalf("hf was handed %q, want an hf://models/, hf://datasets/ or hf://spaces/ URI", target)
	}
	parts := strings.SplitN(rest, "/", 3)
	repo := filepath.Join(f.root, kind+"--"+parts[0]+"--"+parts[1])
	if f.damaged(repo) {
		return nil, false
	}

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
		return payloads, true
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
	return payloads, true
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
	idle := &fakeHF{t: t, root: tree.Root, idle: true,
		stderr: "\x1b[33mCould not find in cache:\x1b[0m\n  - hf://models/unsloth/Flash-GGUF/Flash-Q8_0.gguf\nNothing to delete.\n"}
	reclaimed, err := executeThrough(t, tree.Root, plan, idle)
	want := "hf cache rm left 2 of the 2 paths of unsloth/Flash-GGUF:Q8_0 in place, "
	said := "; it said: Could not find in cache: · - hf://models/unsloth/Flash-GGUF/Flash-Q8_0.gguf · Nothing to delete."
	if err == nil || !strings.HasPrefix(err.Error(), want) || !strings.HasSuffix(err.Error(), said) {
		t.Errorf("the execute answered %v, want it to say hf left the delete in place, and what hf said", err)
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

// A repository of another namespace goes to hf under that namespace's URI.
func TestExecuteNamesEachNamespaceToHF(t *testing.T) {
	for dir, want := range map[string]string{
		"datasets--unsloth--Flash-Data": "hf://datasets/unsloth/Flash-Data",
		"spaces--unsloth--Flash-Demo":   "hf://spaces/unsloth/Flash-Demo",
	} {
		t.Run(dir, func(t *testing.T) {
			tree := newCacheTree(t)
			tree.repo(dir).
				stored("revision-one", cachedFile{name: "data.bin", size: 2000}).
				main("revision-one")
			repoType, id, _ := parseRepoDir(dir)
			repo := repoOf(t, read(t, tree.Root), id)
			if repo.Type != repoType {
				t.Fatalf("the walk read %s as a %s, want a %s", dir, repo.Type, repoType)
			}
			plan, err := PlanRepo(repo, nil)
			if err != nil {
				t.Fatal(err)
			}

			hf := &fakeHF{t: t, root: tree.Root}
			if _, err := executeThrough(t, tree.Root, plan, hf); err != nil {
				t.Fatalf("executing the plan: %v", err)
			}
			if got := hf.args[0][5:]; !reflect.DeepEqual(got, []string{want}) {
				t.Errorf("hf was handed %q, want %q", got, want)
			}
		})
	}
}

// hf takes a path out of every revision that holds it, so a quant two
// revisions name goes to hf as one URI per path, not one per snapshot entry.
func TestExecuteNamesAPathTwoRevisionsHoldOnce(t *testing.T) {
	quant := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	tree := newCacheTree(t)
	repo := tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", quant).
		main("revision-one")
	second := filepath.Join(repo.dir, "snapshots", "revision-two")
	mkdir(t, second)
	repo.link(second, quant.name, repoBlobOf(repo, quant))

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	hf := &fakeHF{t: t, root: tree.Root}
	if _, err := executeThrough(t, tree.Root, plan, hf); err != nil {
		t.Fatalf("executing the plan: %v", err)
	}
	if got, want := hf.args[0][5:], []string{"hf://models/unsloth/Flash-GGUF/Flash-Q8_0.gguf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hf was handed %q, want %q", got, want)
	}
	if _, err := os.Lstat(second); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the revision the delete emptied is still there (%v)", err)
	}
}

// A plan that would no longer go to hf as it was confirmed is refused: the
// store it counted on is not one hf frees from any more.
func TestExecuteRefusesAPlanWhoseHFTargetsChanged(t *testing.T) {
	tree := flashWithQ8(t, cachedFile{name: "Flash-Q8_0.gguf", size: 2000})
	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	if err := os.Remove(filepath.Join(tree.Root, "blobs", ".huggingface-shared-blobs")); err != nil {
		t.Fatal(err)
	}

	_, err := execute(context.Background(), plan, nil, "/opt/homebrew/bin/hf", (&fakeHF{t: t, root: tree.Root}).run)
	var drift *DriftError
	if !errors.As(err, &drift) || drift.Change != "what the delete would hand to hf is not what it planned to" {
		t.Errorf("the execute answered %v, want a drift refusal naming what goes to hf", err)
	}
}

// A run that outlives its deadline is killed, and the failure says it timed
// out.
func TestExecuteSaysAnHFRunTimedOut(t *testing.T) {
	tree := flashWithQ8(t, cachedFile{name: "Flash-Q8_0.gguf", size: 2000})
	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := execute(ctx, plan, nil, "/opt/homebrew/bin/hf", (&fakeHF{t: t, root: tree.Root, hang: true}).run)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("the execute answered %v, want it to say hf timed out", err)
	}
}

// hf runs with colour and its update checks off.
func TestRunHFTurnsOffColourAndUpdateChecks(t *testing.T) {
	stderr, err := runHF(context.Background(), "/bin/sh", []string{"-c", `echo "$NO_COLOR $HF_HUB_DISABLE_UPDATE_CHECK" >&2`})
	if err != nil || string(stderr) != "1 1\n" {
		t.Errorf("hf ran with NO_COLOR and HF_HUB_DISABLE_UPDATE_CHECK reading %q (%v), want both 1", stderr, err)
	}
}

// What hf writes to a terminal is not what a failure carries: colour and
// links come out.
func TestLastWordsStripsTerminalEscapes(t *testing.T) {
	got := lastWords([]byte("\x1b[1m\x1b[31mError:\x1b[0m Could not find in cache\n\x1b]8;;https://hf.co\x07docs\x1b]8;;\x07\n\x1b7done\n"))
	if want := "Error: Could not find in cache · docs · done"; got != want {
		t.Errorf("the failure carries %q, want %q", got, want)
	}
}

// refRepo is the repository hf's damaged-repository rule is about: r1 holds
// Q4_K_M; refs/main names r2 and refs/pr/1 names r3, each holding only Q8_0;
// r4, which no ref names, holds only Q8_0 too. Every file is in the store.
func refRepo(t *testing.T) (*cacheTree, *repoTree) {
	t.Helper()
	q8 := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	tree := newCacheTree(t)
	repo := tree.repo("models--unsloth--Flash-GGUF").
		stored("r1", cachedFile{name: "Flash-Q4_K_M.gguf", size: 1200}).
		stored("r2", q8).
		main("r2").
		file("refs/pr/1", "r3")
	for _, revision := range []string{"r3", "r4"} {
		dir := filepath.Join(repo.dir, "snapshots", revision)
		mkdir(t, dir)
		repo.link(dir, q8.name, repoBlobOf(repo, q8))
	}
	return tree, repo
}

// A snapshot directory a ref names stays when a delete empties it, through hf
// or not; any other emptied snapshot goes (docs/specs/CACHE.md, Deletion).
// Through hf, that is what lets the repository's next delete go through hf as
// well: hf skips a repository whose ref names a missing snapshot.
func TestADeleteKeepsTheSnapshotsRefsName(t *testing.T) {
	next := map[string]func(t *testing.T, root string) *Plan{
		"then its last quant": func(t *testing.T, root string) *Plan {
			return planQuant(t, root, "unsloth/Flash-GGUF", "Q4_K_M")
		},
		"then the repo": func(t *testing.T, root string) *Plan {
			plan, err := PlanRepo(repoOf(t, read(t, root), "unsloth/Flash-GGUF"), nil)
			if err != nil {
				t.Fatal(err)
			}
			return plan
		},
	}
	for name, plan := range next {
		t.Run(name, func(t *testing.T) {
			tree, repo := refRepo(t)
			snapshots := filepath.Join(repo.dir, "snapshots")

			first := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			if want := []string{filepath.Join(snapshots, "r4")}; !reflect.DeepEqual(first.Dirs, want) {
				t.Errorf("the plan empties %v, want only %v", first.Dirs, want)
			}
			if _, err := executeThrough(t, tree.Root, first, &fakeHF{t: t, root: tree.Root}); err != nil {
				t.Fatalf("deleting Q8_0 through hf: %v", err)
			}
			for _, kept := range []string{"r1", "r2", "r3"} {
				if _, err := os.Stat(filepath.Join(snapshots, kept)); err != nil {
					t.Errorf("snapshot %s is gone after the delete: %v", kept, err)
				}
			}

			second := plan(t, tree.Root)
			if want := []string{"hf://models/unsloth/Flash-GGUF"}; !reflect.DeepEqual(second.HFTargets, want) {
				t.Errorf("the next delete hands hf %q, want %q", second.HFTargets, want)
			}
			if _, err := executeThrough(t, tree.Root, second, &fakeHF{t: t, root: tree.Root}); err != nil {
				t.Fatalf("the next delete through hf: %v", err)
			}
			if _, err := os.Lstat(repo.dir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the repository is still there after its delete (%v)", err)
			}
		})
	}
}

// cria's own delete keeps a snapshot a ref names just the same.
func TestCriasOwnDeleteKeepsTheSnapshotsRefsName(t *testing.T) {
	tree, repo := refRepo(t)
	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	if _, err := Execute(context.Background(), plan, nil, ""); err != nil {
		t.Fatalf("executing the plan: %v", err)
	}

	snapshots := filepath.Join(repo.dir, "snapshots")
	var held []string
	entries, _ := os.ReadDir(snapshots)
	for _, entry := range entries {
		held = append(held, entry.Name())
	}
	if want := []string{"r1", "r2", "r3"}; !reflect.DeepEqual(held, want) {
		t.Errorf("the snapshots are %v, want %v", held, want)
	}
}

// A ref cria cannot read may name any snapshot, so a delete keeps them all, and
// still goes ahead: it needs no ref, only hf does.
func TestADeleteWithAnUnreadableRefKeepsEverySnapshot(t *testing.T) {
	tree, repo := refRepo(t)
	if err := os.Symlink("nowhere", filepath.Join(repo.dir, "refs", "pr", "9")); err != nil {
		t.Fatal(err)
	}
	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	if plan.HFTargets != nil {
		t.Errorf("the plan hands hf %q, want nothing", plan.HFTargets)
	}
	if _, err := Execute(context.Background(), plan, nil, ""); err != nil {
		t.Fatalf("executing the plan: %v", err)
	}

	var held []string
	entries, _ := os.ReadDir(filepath.Join(repo.dir, "snapshots"))
	for _, entry := range entries {
		held = append(held, entry.Name())
	}
	if want := []string{"r1", "r2", "r3", "r4"}; !reflect.DeepEqual(held, want) {
		t.Errorf("the snapshots are %v, want %v", held, want)
	}
}

// A repository whose ref already names a missing snapshot is one hf skips, so
// its delete is cria's own and never runs hf.
func TestADamagedRepoNeverGoesToHF(t *testing.T) {
	plans := map[string]func(t *testing.T, root string) *Plan{
		"quant": func(t *testing.T, root string) *Plan {
			return planQuant(t, root, "unsloth/Flash-GGUF", "Q8_0")
		},
		"repo": func(t *testing.T, root string) *Plan {
			plan, err := PlanRepo(repoOf(t, read(t, root), "unsloth/Flash-GGUF"), nil)
			if err != nil {
				t.Fatal(err)
			}
			return plan
		},
	}
	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			tree := flashWithQ8(t, cachedFile{name: "Flash-Q8_0.gguf", size: 2000})
			tree.repo("models--unsloth--Flash-GGUF").file("refs/pr/7", "a-revision-not-cached")

			made := plan(t, tree.Root)
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

// A failure's stderr is its tail on one line, a token masked.
func TestLastWordsMasksATokenOnOneLine(t *testing.T) {
	got := lastWords([]byte("\nusing hf_abcdefghijklmnopqrstuvwxyz0123\n\nError: denied\n"))
	if want := "using hf_*** · Error: denied"; got != want {
		t.Errorf("the failure carries %q, want %q", got, want)
	}
}
