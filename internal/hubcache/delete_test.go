package hubcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Every delete test proves the same three things against a real tree on disk:
// what the plan named is what left, nothing else moved, and the bytes reported
// back are the bytes the tree lost. A delete that was only checked against its
// own bookkeeping would prove nothing about the cache on the machine.

// Deleting a quant takes its snapshot entry and the blob behind it, and leaves
// everything else in the repo exactly where it was.
func TestPlanQuantRemovesItsLinksAndItsBlobs(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200},
			cachedFile{name: "mmproj-BF16.gguf", size: 400}).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL")

	if plan.Bytes != 3000 {
		t.Errorf("the plan reclaims %d bytes, want the 3000 the quant holds", plan.Bytes)
	}
	if len(plan.Removes) != 2 {
		t.Errorf("the plan removes %v, want the quant's link and its blob", removalPaths(plan))
	}
	if len(plan.Shared) != 0 {
		t.Errorf("the plan leaves %v behind; nothing else reaches those blobs", plan.Shared)
	}
	if len(plan.Dirs) != 0 {
		t.Errorf("the plan empties %v, but the revision still holds the other quants", plan.Dirs)
	}

	applyPlan(t, tree.Root, plan)

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")
	if got, want := itemLabels(repo), []string{"UD-Q2_K_XL", "mmproj-BF16.gguf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the repo now lists %v, want %v", got, want)
	}
	if remaining, _ := repo.Item("UD-Q2_K_XL"); remaining.Bytes != 1200 {
		t.Errorf("the quant beside it now holds %d bytes, want the 1200 it always held", remaining.Bytes)
	}
	if want := diskUsage(t, repo.Dir); repo.Bytes != want {
		t.Errorf("the repo reports %d bytes, want the %d its directory occupies", repo.Bytes, want)
	}
}

// The tag a config entry spells finds the item the same way everywhere: exactly,
// case aside (docs/specs/CACHE.md). A tag the repo does not publish is refused
// rather than resolved to something near it.
func TestPlanQuantFindsTheQuantTheWayAnEntryDoes(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "Qwen3.8-27B-Q4_K_M.gguf", size: 2000}).
		main("revision-one")
	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")

	plan, err := PlanQuant(repo, "ud-q4_k_xl", nil)
	if err != nil {
		t.Fatalf("planning the delete of ud-q4_k_xl: %v", err)
	}
	if plan.Target.Quant != "UD-Q4_K_XL" {
		t.Errorf("the plan targets %q, want the repo's own spelling UD-Q4_K_XL", plan.Target.Quant)
	}

	// The same tag without the prefix the file carries is a different tag.
	if _, err := PlanQuant(repo, "Q4_K_XL", nil); err == nil {
		t.Error("planning the delete of Q4_K_XL succeeded, want a refusal — the repo spells it UD-Q4_K_XL")
	}
}

// A blob two different files reach stays where it is: the delete removes its own
// name and nothing else, and the plan says which bytes it had to leave behind
// and what still keeps them (docs/specs/CACHE.md).
func TestPlanQuantLeavesABlobAnotherFileStillReaches(t *testing.T) {
	shared := cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-new", shared, cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
		alias("revision-old", "Qwen3.8-27B-UD-Q8_0.gguf", shared).
		main("revision-new")

	plan := planQuant(t, tree.Root, "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL")

	keeper := filepath.Join(tree.Root, "models--unsloth--Qwen3.8-27B-GGUF", "snapshots", "revision-old", "Qwen3.8-27B-UD-Q8_0.gguf")
	if len(plan.Shared) != 1 {
		t.Fatalf("the plan leaves %v behind, want the one blob the other name still reaches", plan.Shared)
	}
	if plan.Shared[0].Bytes != 3000 {
		t.Errorf("the blob left behind holds %d bytes, want 3000", plan.Shared[0].Bytes)
	}
	if got := plan.Shared[0].Links; !reflect.DeepEqual(got, []string{keeper}) {
		t.Errorf("the blob is kept by %v, want %v", got, []string{keeper})
	}
	if plan.Bytes != 0 {
		t.Errorf("the plan reclaims %d bytes, want 0 — the bytes are still needed", plan.Bytes)
	}
	if len(plan.Removes) != 1 {
		t.Errorf("the plan removes %v, want only the quant's own link", removalPaths(plan))
	}

	applyPlan(t, tree.Root, plan)

	if _, err := os.Stat(keeper); err != nil {
		t.Errorf("the other name no longer resolves after the delete: %v", err)
	}
}

// One file linked from several snapshots is one blob and several names: all of
// them go, and the blob goes with the last of them.
func TestPlanQuantRemovesEveryLinkOfAFileSeveralSnapshotsShare(t *testing.T) {
	quant := cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one", quant, cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
		snapshot("revision-two", quant).
		snapshot("revision-three", quant).
		main("revision-three")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")
	item, _ := repo.Item("UD-Q4_K_XL")
	if len(item.Files) != 1 || len(item.Files[0].Links) != 3 {
		t.Fatalf("the walk sees %d files reached by %d entries, want one blob reached by 3",
			len(item.Files), len(item.Files[0].Links))
	}

	plan := planQuant(t, tree.Root, "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL")

	if len(plan.Removes) != 4 {
		t.Errorf("the plan removes %v, want the 3 links and the one blob", removalPaths(plan))
	}
	if plan.Bytes != 3000 {
		t.Errorf("the plan reclaims %d bytes, want the 3000 of the one blob", plan.Bytes)
	}
	// The two revisions that held nothing else are left empty and go too.
	snapshots := filepath.Join(repo.Dir, "snapshots")
	want := []string{filepath.Join(snapshots, "revision-three"), filepath.Join(snapshots, "revision-two")}
	if got := sorted(plan.Dirs); !reflect.DeepEqual(got, sorted(want)) {
		t.Errorf("the plan empties %v, want %v", got, sorted(want))
	}

	applyPlan(t, tree.Root, plan)

	after := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")
	if got, want := itemLabels(after), []string{"UD-Q2_K_XL"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the repo now lists %v, want %v", got, want)
	}
}

// A quant split across shards is one unit: every shard's link and every shard's
// blob go together (docs/cria.md, principle 2).
func TestPlanQuantRemovesEveryShardOfASplitQuant(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3-235B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00001-of-00003.gguf", size: 1000},
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00002-of-00003.gguf", size: 1000},
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00003-of-00003.gguf", size: 500},
			cachedFile{name: "Qwen3-235B-UD-Q2_K_XL.gguf", size: 700}).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Qwen3-235B-GGUF", "UD-Q4_K_XL")

	if len(plan.Removes) != 6 {
		t.Errorf("the plan removes %v, want the 3 shards' links and their 3 blobs", removalPaths(plan))
	}
	if plan.Bytes != 2500 {
		t.Errorf("the plan reclaims %d bytes, want the 2500 the shards sum to", plan.Bytes)
	}

	applyPlan(t, tree.Root, plan)

	after := repoOf(t, read(t, tree.Root), "unsloth/Qwen3-235B-GGUF")
	if got, want := itemLabels(after), []string{"UD-Q2_K_XL"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the repo now lists %v, want %v", got, want)
	}
}

// A repository whose last quant is deleted is disk with no model in it: the
// skeleton goes too — snapshots, refs, blobs and the bookkeeping beside them
// (docs/specs/CACHE.md).
func TestPlanQuantTakesTheSkeletonOfTheLastQuant(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--LiquidAI--LFM2.5-2.6B-GGUF").
		snapshot("revision-one", cachedFile{name: "LFM2.5-2.6B-Q8_0.gguf", size: 700}).
		file(filepath.Join(".no_exist", "revision-one", "generation_config.json"), "").
		file(filepath.Join("trees", "revision-one"), "tree").
		main("revision-one")
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one", cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}).
		main("revision-one")

	dir := repoOf(t, read(t, tree.Root), "LiquidAI/LFM2.5-2.6B-GGUF").Dir
	plan := planQuant(t, tree.Root, "LiquidAI/LFM2.5-2.6B-GGUF", "Q8_0")

	if want := diskUsage(t, dir); plan.Bytes != want {
		t.Errorf("the plan reclaims %d bytes, want the %d the repository occupies", plan.Bytes, want)
	}
	if got := plan.Dirs[len(plan.Dirs)-1]; got != dir {
		t.Errorf("the plan's last directory is %s, want the repository directory %s", got, dir)
	}

	applyPlan(t, tree.Root, plan)

	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the repository directory is still there after its last quant went: %v", err)
	}
	cache := read(t, tree.Root)
	if got, want := repoIDs(cache), []string{"unsloth/Qwen3.8-27B-GGUF"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the cache now holds %v, want %v", got, want)
	}
}

// Last means the repository is left with nothing. Anything else it still holds —
// a file that is not an item, a download still in flight — keeps the skeleton,
// because the repository still holds it.
func TestPlanQuantKeepsTheSkeletonWhenTheRepoStillHoldsSomething(t *testing.T) {
	tests := []struct {
		name  string
		build func(*repoTree)
	}{
		{
			name:  "a file that is not an item",
			build: func(r *repoTree) { r.snapshot("revision-one", cachedFile{name: "README.md", size: 120}) },
		},
		{
			name:  "an unfinished download",
			build: func(r *repoTree) { r.partial("aaaa.downloadInProgress", 640) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := newCacheTree(t)
			repo := tree.repo("models--LiquidAI--LFM2.5-2.6B-GGUF").
				snapshot("revision-one", cachedFile{name: "LFM2.5-2.6B-Q8_0.gguf", size: 700}).
				main("revision-one")
			test.build(repo)

			dir := repoOf(t, read(t, tree.Root), "LiquidAI/LFM2.5-2.6B-GGUF").Dir
			plan := planQuant(t, tree.Root, "LiquidAI/LFM2.5-2.6B-GGUF", "Q8_0")

			if plan.Bytes != 700 {
				t.Errorf("the plan reclaims %d bytes, want the 700 of the quant alone", plan.Bytes)
			}
			for _, emptied := range plan.Dirs {
				if emptied == dir {
					t.Errorf("the plan removes the repository directory %s, but the repo still holds something", dir)
				}
			}

			applyPlan(t, tree.Root, plan)

			after := repoOf(t, read(t, tree.Root), "LiquidAI/LFM2.5-2.6B-GGUF")
			if len(after.Items) != 0 {
				t.Errorf("the repo still lists %v, want no quants", itemLabels(after))
			}
			if _, err := os.Stat(filepath.Join(dir, "refs", "main")); err != nil {
				t.Errorf("the repo's ref went with the quant: %v", err)
			}
		})
	}
}

// An MLX model is its repo and so is everything else the cache happens to hold:
// those are deleted whole (docs/specs/CACHE.md).
func TestPlanRepoRemovesARepositoryWhole(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00002.safetensors", size: 2500},
			cachedFile{name: "model-00002-of-00002.safetensors", size: 2500}).
		partial("bbbb.incomplete", 300).
		main("revision-one")
	tree.repo("datasets--allenai--c4").
		snapshot("revision-one", cachedFile{name: "train.parquet", size: 900}).
		main("revision-one")
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one", cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}).
		main("revision-one")

	for _, id := range []string{"mlx-community/Qwen3.8-27B-4bit", "allenai/c4"} {
		t.Run(id, func(t *testing.T) {
			repo := repoOf(t, read(t, tree.Root), id)
			plan, err := PlanRepo(repo, nil)
			if err != nil {
				t.Fatalf("planning the delete of %s: %v", id, err)
			}
			if plan.Bytes != repo.Bytes {
				t.Errorf("the plan reclaims %d bytes, want the %d the repo reports", plan.Bytes, repo.Bytes)
			}
			// The unfinished download is part of the repository, so it goes with it.
			for _, partial := range repo.Partials {
				if !pathsOf(plan.Removes)[partial.Path] {
					t.Errorf("the plan leaves %s behind, want the repo's unfinished downloads removed with it", partial.Path)
				}
			}

			applyPlan(t, tree.Root, plan)

			if _, err := os.Stat(repo.Dir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the repository directory is still there: %v", err)
			}
		})
	}

	// The repo nobody asked about is untouched, bytes and all.
	survivor := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")
	if quant, _ := survivor.Item("UD-Q4_K_XL"); quant.Bytes != 3000 {
		t.Errorf("the untouched repo's quant holds %d bytes, want 3000", quant.Bytes)
	}
	if want := diskUsage(t, survivor.Dir); survivor.Bytes != want {
		t.Errorf("the untouched repo reports %d bytes, want the %d its directory occupies", survivor.Bytes, want)
	}
}

// An unfinished download is reclaimable on its own, and every unfinished
// download of a repo can go at once (docs/specs/CACHE.md).
func TestPlanPartialsReclaimUnfinishedDownloads(t *testing.T) {
	build := func(t *testing.T) *cacheTree {
		tree := newCacheTree(t)
		tree.repo("models--unsloth--Qwen3.6-35B-A3B-GGUF").
			snapshot("revision-one", cachedFile{name: "Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf", size: 2000}).
			partial("aaaa.downloadInProgress", 1500).
			partial("bbbb.incomplete", 800).
			main("revision-one")
		return tree
	}

	t.Run("one of them", func(t *testing.T) {
		tree := build(t)
		repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.6-35B-A3B-GGUF")
		one := filepath.Join(repo.Dir, "blobs", "aaaa.downloadInProgress")

		plan, err := PlanPartial(repo, one, nil)
		if err != nil {
			t.Fatalf("planning the delete of %s: %v", one, err)
		}
		if plan.Bytes != 1500 || len(plan.Removes) != 1 {
			t.Errorf("the plan removes %v worth %d bytes, want just the one partial worth 1500",
				removalPaths(plan), plan.Bytes)
		}

		applyPlan(t, tree.Root, plan)

		after := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.6-35B-A3B-GGUF")
		if after.PartialBytes != 800 {
			t.Errorf("%d reclaimable bytes are left, want the 800 of the other partial", after.PartialBytes)
		}
	})

	t.Run("all of them", func(t *testing.T) {
		tree := build(t)
		repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.6-35B-A3B-GGUF")

		plan, err := PlanPartials(repo, nil)
		if err != nil {
			t.Fatalf("planning the delete of the unfinished downloads: %v", err)
		}
		if plan.Bytes != 2300 || len(plan.Removes) != 2 {
			t.Errorf("the plan removes %v worth %d bytes, want both partials worth 2300",
				removalPaths(plan), plan.Bytes)
		}

		applyPlan(t, tree.Root, plan)

		after := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.6-35B-A3B-GGUF")
		if after.PartialBytes != 0 || !after.Complete {
			t.Errorf("the repo reports %d reclaimable bytes (complete=%v), want none and complete",
				after.PartialBytes, after.Complete)
		}
		if quant, _ := after.Item("UD-Q4_K_XL"); quant.Bytes != 2000 {
			t.Errorf("the quant that was there holds %d bytes, want the 2000 it always held", quant.Bytes)
		}
	})

	t.Run("none to reclaim", func(t *testing.T) {
		tree := newCacheTree(t)
		tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
			snapshot("revision-one", cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}).
			main("revision-one")
		repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")

		if _, err := PlanPartials(repo, nil); err == nil {
			t.Error("planning the unfinished downloads of a whole repo succeeded, want a refusal — there are none")
		}
	})
}

// The model a server has open cannot be deleted; the refusal names the server to
// stop (docs/specs/CACHE.md). Matching is the same everywhere: exact, case
// aside — llama.cpp's own -hf resolution ignores case and nothing else about a
// provider's spelling is reinterpreted.
func TestPlanRefusesWhatAServerHasOpen(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
		partial("aaaa.downloadInProgress", 640).
		main("revision-one")
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00001.safetensors", size: 2500}).
		main("revision-one")

	cache := read(t, tree.Root)
	gguf := repoOf(t, cache, "unsloth/Qwen3.8-27B-GGUF")
	mlx := repoOf(t, cache, "mlx-community/Qwen3.8-27B-4bit")
	partial := gguf.Partials[0].Path

	serving := func(entry, repo, quant string) []Served {
		return []Served{{Entry: "unrelated", Repo: "Qwen/Qwen3-Embedding-0.6B-GGUF", Quant: "Q8_0"}, {Entry: entry, Repo: repo, Quant: quant}}
	}

	tests := []struct {
		name    string
		plan    func([]Served) (*Plan, error)
		served  []Served
		refused string // the entry the refusal must name; empty when the delete is allowed
	}{
		{
			name:    "the quant a llama server is serving",
			plan:    func(s []Served) (*Plan, error) { return PlanQuant(gguf, "UD-Q4_K_XL", s) },
			served:  serving("chat", "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL"),
			refused: "chat",
		},
		{
			name:    "the same quant spelled in another case",
			plan:    func(s []Served) (*Plan, error) { return PlanQuant(gguf, "UD-Q4_K_XL", s) },
			served:  serving("chat", "UNSLOTH/qwen3.8-27b-gguf", "ud-q4_k_xl"),
			refused: "chat",
		},
		{
			// Different files: the running server maps neither of the other's bytes.
			name:   "another quant of the repo a llama server is serving",
			plan:   func(s []Served) (*Plan, error) { return PlanQuant(gguf, "UD-Q2_K_XL", s) },
			served: serving("chat", "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL"),
		},
		{
			// The server let llama-server pick the file; cria cannot know which.
			name:    "any quant of a repo served without a quant named",
			plan:    func(s []Served) (*Plan, error) { return PlanQuant(gguf, "UD-Q2_K_XL", s) },
			served:  serving("chat", "unsloth/Qwen3.8-27B-GGUF", ""),
			refused: "chat",
		},
		{
			name:    "the whole repo of a served quant",
			plan:    func(s []Served) (*Plan, error) { return PlanRepo(gguf, s) },
			served:  serving("chat", "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL"),
			refused: "chat",
		},
		{
			name:    "an MLX repo a server is serving",
			plan:    func(s []Served) (*Plan, error) { return PlanRepo(mlx, s) },
			served:  serving("mlx", "mlx-community/Qwen3.8-27B-4bit", ""),
			refused: "mlx",
		},
		{
			name:   "an MLX repo nobody is serving",
			plan:   func(s []Served) (*Plan, error) { return PlanRepo(mlx, s) },
			served: serving("chat", "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL"),
		},
		{
			// cria cannot tell a stale partial from the one the running server is
			// filling right now, so the repo it belongs to is what is guarded.
			name:    "an unfinished download of a repo being served",
			plan:    func(s []Served) (*Plan, error) { return PlanPartial(gguf, partial, s) },
			served:  serving("chat", "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL"),
			refused: "chat",
		},
		{
			name:   "an unfinished download of a repo nobody is serving",
			plan:   func(s []Served) (*Plan, error) { return PlanPartials(gguf, s) },
			served: serving("mlx", "mlx-community/Qwen3.8-27B-4bit", ""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := test.plan(test.served)
			if test.refused == "" {
				if err != nil {
					t.Fatalf("the delete was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("the delete of %s was planned, want a refusal naming %s", plan.Target, test.refused)
			}
			var served *ServedError
			if !errors.As(err, &served) {
				t.Fatalf("the refusal is %v, want a *ServedError", err)
			}
			if served.Served.Entry != test.refused {
				t.Errorf("the refusal names %q, want the running %q", served.Served.Entry, test.refused)
			}
		})
	}

	// Nothing was planned, so nothing may have moved.
	if after := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF"); after.Bytes != gguf.Bytes {
		t.Errorf("the repo holds %d bytes after the refusals, want the %d it started with", after.Bytes, gguf.Bytes)
	}
}

// A plan names exact paths and exact bytes. If the cache moves on between the
// confirmation and the delete, acting on it would remove something nobody was
// shown — so the execute refuses and asks for a new plan.
func TestExecuteRefusesAPlanTheCacheHasMovedOn(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, repo *Repo)
	}{
		{
			name: "another name now reaches the blob",
			change: func(t *testing.T, repo *Repo) {
				dir := filepath.Join(repo.Dir, "snapshots", "revision-two")
				mkdir(t, dir)
				target, err := filepath.Rel(dir, targetFile(t, repo).Blob)
				if err != nil {
					t.Fatalf("cannot point at the blob: %v", err)
				}
				if err := os.Symlink(target, filepath.Join(dir, "Qwen3.8-27B-UD-Q8_0.gguf")); err != nil {
					t.Fatalf("cannot link: %v", err)
				}
			},
		},
		{
			name: "the link is already gone",
			change: func(t *testing.T, repo *Repo) {
				if err := os.Remove(targetFile(t, repo).Links[0]); err != nil {
					t.Fatalf("cannot remove the link: %v", err)
				}
			},
		},
		{
			name: "the blob is not the size the plan counted",
			change: func(t *testing.T, repo *Repo) {
				file, err := os.OpenFile(targetFile(t, repo).Blob, os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					t.Fatalf("cannot grow the blob: %v", err)
				}
				defer file.Close()
				if _, err := file.WriteString("more bytes than the plan counted"); err != nil {
					t.Fatalf("cannot grow the blob: %v", err)
				}
			},
		},
		{
			name: "the repository is gone",
			change: func(t *testing.T, repo *Repo) {
				if err := os.RemoveAll(repo.Dir); err != nil {
					t.Fatalf("cannot remove the repository: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := newCacheTree(t)
			tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
				snapshot("revision-one",
					cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
					cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
				main("revision-one")

			repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")
			plan := planQuant(t, tree.Root, "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL")
			test.change(t, repo)

			err := refuseExecute(t, tree.Root, plan, nil)
			var drift *DriftError
			if !errors.As(err, &drift) {
				t.Fatalf("the refusal is %v, want a *DriftError", err)
			}
			if drift.Change == "" {
				t.Error("the refusal says nothing about what changed")
			}
		})
	}
}

// A plan is spent once it has run: the second attempt finds a cache that no
// longer holds what it described.
func TestExecuteRefusesAPlanThatAlreadyRan(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Qwen3.8-27B-GGUF", "UD-Q4_K_XL")
	applyPlan(t, tree.Root, plan)

	var drift *DriftError
	if err := refuseExecute(t, tree.Root, plan, nil); !errors.As(err, &drift) {
		t.Fatalf("running the plan again failed with %v, want a *DriftError", err)
	}
}

// A plan is made before a confirmation and carried out after it, so the serving
// guard is asked again at the moment of the delete: a server that started while
// the confirmation was on screen still stops it, and the bytes it has open stay
// where they are.
func TestExecuteRefusesWhatAServerStartedSincePlanning(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
		main("revision-one")
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00001.safetensors", size: 2500}).
		main("revision-one")

	tests := []struct {
		name   string
		plan   func(*Cache) (*Plan, error)
		served []Served
	}{
		{
			name: "a llama server on the quant",
			plan: func(cache *Cache) (*Plan, error) {
				return PlanQuant(repoOf(t, cache, "unsloth/Qwen3.8-27B-GGUF"), "UD-Q4_K_XL", nil)
			},
			served: []Served{{Entry: "chat", Repo: "unsloth/Qwen3.8-27B-GGUF", Quant: "UD-Q4_K_XL"}},
		},
		{
			name: "an MLX server on the repo",
			plan: func(cache *Cache) (*Plan, error) {
				return PlanRepo(repoOf(t, cache, "mlx-community/Qwen3.8-27B-4bit"), nil)
			},
			served: []Served{{Entry: "mlx", Repo: "mlx-community/Qwen3.8-27B-4bit"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Nothing was running when the plan was made.
			plan, err := test.plan(read(t, tree.Root))
			if err != nil {
				t.Fatalf("planning the delete: %v", err)
			}

			err = refuseExecute(t, tree.Root, plan, test.served)
			var served *ServedError
			if !errors.As(err, &served) {
				t.Fatalf("the refusal is %v, want a *ServedError", err)
			}
			if served.Served.Entry != test.served[0].Entry {
				t.Errorf("the refusal names %q, want the running %q", served.Served.Entry, test.served[0].Entry)
			}
		})
	}
}

// targetFile is the one file behind the quant every drift case plans to delete.
func targetFile(t *testing.T, repo *Repo) File {
	t.Helper()
	item, found := repo.Item("UD-Q4_K_XL")
	if !found || len(item.Files) != 1 {
		t.Fatalf("the fixture holds %v, want one UD-Q4_K_XL of one file", itemLabels(repo))
	}
	return item.Files[0]
}

// planQuant plans one quant deletion off a fresh walk, failing the test when the
// plan itself is refused.
func planQuant(t *testing.T, root, id, quant string) *Plan {
	t.Helper()
	plan, err := PlanQuant(repoOf(t, read(t, root), id), quant, nil)
	if err != nil {
		t.Fatalf("planning the delete of %s:%s: %v", id, quant, err)
	}
	return plan
}

// applyPlan runs a plan and holds it to everything a delete promises: exactly
// the planned paths left the tree, nothing else moved, and the bytes it reports
// are the bytes the tree lost.
func applyPlan(t *testing.T, root string, plan *Plan) int64 {
	t.Helper()
	before := treeEntries(t, root)
	beforeBytes := diskUsage(t, root)

	// Nothing is serving in the fixtures these deletes run against; the guard at
	// execute time has its own tests.
	reclaimed, err := Execute(plan, nil)
	if err != nil {
		t.Fatalf("executing the plan for %s: %v", plan.Target, err)
	}

	after := treeEntries(t, root)
	var removed, added []string
	for path, was := range before {
		now, still := after[path]
		switch {
		case !still:
			removed = append(removed, path)
		case now != was:
			t.Errorf("%s is now %q, was %q — the delete changed what it did not plan to remove", path, now, was)
		}
	}
	for path := range after {
		if _, existed := before[path]; !existed {
			added = append(added, path)
		}
	}
	if len(added) > 0 {
		t.Errorf("the delete left %v behind, want nothing new", sorted(added))
	}
	if want := plannedPaths(plan); !reflect.DeepEqual(sorted(removed), want) {
		t.Errorf("the delete removed\n\t%v\nwant exactly what it planned\n\t%v", sorted(removed), want)
	}
	if lost := beforeBytes - diskUsage(t, root); lost != reclaimed {
		t.Errorf("the delete reports %d bytes reclaimed, want the %d the tree lost", reclaimed, lost)
	}
	if reclaimed != plan.Bytes {
		t.Errorf("the delete reports %d bytes reclaimed, want the %d the plan promised", reclaimed, plan.Bytes)
	}
	return reclaimed
}

// refuseExecute runs a plan that must be refused, and proves the tree is exactly
// as it was.
func refuseExecute(t *testing.T, root string, plan *Plan, served []Served) error {
	t.Helper()
	before := treeEntries(t, root)

	reclaimed, err := Execute(plan, served)
	if err == nil {
		t.Fatalf("the plan for %s executed, want a refusal", plan.Target)
	}
	if reclaimed != 0 {
		t.Errorf("the refusal reports %d bytes reclaimed, want 0", reclaimed)
	}
	if after := treeEntries(t, root); !reflect.DeepEqual(after, before) {
		t.Errorf("the refused delete changed the tree:\nbefore %v\nafter  %v", before, after)
	}
	return err
}

// treeEntries describes every entry under a tree — what it is, how big it is and
// where a link points — so a test can name exactly what a delete removed and
// prove everything else is untouched.
func treeEntries(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			entries[path] = "directory"
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries[path] = "link to " + target
		default:
			info, err := entry.Info()
			if err != nil {
				return err
			}
			entries[path] = fmt.Sprintf("%d bytes", info.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read %s: %v", root, err)
	}
	return entries
}

// plannedPaths is everything a plan says will be gone afterwards, ordered.
func plannedPaths(plan *Plan) []string {
	paths := append(removalPaths(plan), plan.Dirs...)
	return sorted(paths)
}

// removalPaths is the files and links a plan removes, for the assertions and the
// failure messages that name them.
func removalPaths(plan *Plan) []string {
	paths := make([]string, 0, len(plan.Removes))
	for _, removal := range plan.Removes {
		paths = append(paths, removal.Path)
	}
	return paths
}

func sorted(paths []string) []string {
	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)
	return ordered
}
