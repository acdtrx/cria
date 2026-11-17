package hubcache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A delete through huggingface_hub's shared store is judged by hf's own rule: a
// stored payload is left with no link only when every valid link its .refs
// manifest lists goes with this delete, and a missing or unreadable manifest
// frees nothing (docs/specs/CACHE.md, Deletion). cria still unlinks only the
// repository's own entries, so what the rule frees stays in the store.

// payloadOf is where the shared store holds a file's bytes.
func payloadOf(root string, file cachedFile) string {
	hash := blobName(file.content())
	return filepath.Join(root, "blobs", hash[:2], hash)
}

// repoBlobOf is the repository's own blob linking a stored file into the store.
func repoBlobOf(repo *repoTree, file cachedFile) string {
	return filepath.Join(repo.dir, "blobs", blobName(append([]byte("etag|"), file.content()...)))
}

// A quant no other repository links: its stored bytes stay in the store, with
// nothing linking them, and only what the repository itself holds comes back.
func TestPlanOfAStoredQuantLeavesItsStoredBytesInTheStore(t *testing.T) {
	stored := cachedFile{name: "Q8_0/Flash-Q8_0-00001-of-00002.gguf", size: 2000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200},
			cachedFile{name: "Q8_0/Flash-Q8_0-00002-of-00002.gguf", size: 3000}).
		stored("revision-one", stored).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	if plan.Bytes != 3000 || plan.Stored != 2000 {
		t.Errorf("the plan reclaims %d bytes and leaves %d in the store, want the 3000 the repo holds and the 2000 stored",
			plan.Bytes, plan.Stored)
	}
	if len(plan.Shared) != 0 {
		t.Errorf("the plan keeps %v, want nothing — no other repository links the stored shard", plan.Shared)
	}

	applyPlan(t, tree.Root, plan)

	if _, err := os.Stat(payloadOf(tree.Root, stored)); err != nil {
		t.Errorf("the stored shard is not in the store after the delete: %v", err)
	}
}

// A stored file another repository links is kept, and the plan names that
// repository's link.
func TestPlanKeepsAStoredFileAnotherRepoLinks(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", stored).
		main("revision-one")
	other := tree.repo("models--mirror--Flash-GGUF").
		stored("revision-one", stored).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	want := []Shared{{
		Blob:   payloadOf(tree.Root, stored),
		Bytes:  2000,
		Links:  []string{repoBlobOf(other, stored)},
		Stored: true,
		Repos:  []string{"mirror/Flash-GGUF"},
	}}
	if !reflect.DeepEqual(plan.Shared, want) {
		t.Errorf("the plan keeps %+v, want %+v", plan.Shared, want)
	}
	if plan.Bytes != 0 || plan.Stored != 0 {
		t.Errorf("the plan reclaims %d bytes and leaves %d in the store, want neither: the other repository still links them",
			plan.Bytes, plan.Stored)
	}

	applyPlan(t, tree.Root, plan)

	if _, err := os.Stat(filepath.Join(other.dir, "snapshots", "revision-one", stored.name)); err != nil {
		t.Errorf("the other repository's file no longer resolves after the delete: %v", err)
	}
}

// A manifest is a hint: a line naming a link that is gone, or that leads to
// other bytes, keeps nothing.
func TestPlanIgnoresAManifestLineWhoseLinkIsNotOneToIt(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	elsewhere := cachedFile{name: "Other-Q4_0.gguf", size: 900}
	cases := map[string]func(t *testing.T, root, link string){
		"gone": func(t *testing.T, root, link string) {
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
		},
		"leads elsewhere": func(t *testing.T, root, link string) {
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			target, err := filepath.Rel(filepath.Dir(link), payloadOf(root, elsewhere))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
		},
	}

	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			tree := newCacheTree(t)
			tree.repo("models--unsloth--Flash-GGUF").
				snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
				stored("revision-one", stored).
				main("revision-one")
			other := tree.repo("models--mirror--Flash-GGUF").
				stored("revision-one", stored, elsewhere).
				main("revision-one")
			spoil(t, tree.Root, repoBlobOf(other, stored))

			plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			if plan.Stored != 2000 || len(plan.Shared) != 0 {
				t.Errorf("the plan leaves %d in the store and keeps %+v, want the 2000 left with no link and nothing kept",
					plan.Stored, plan.Shared)
			}
		})
	}
}

// A store marked for a layout other than the one hf writes is one hf frees
// nothing from, so a delete leaves nothing there for it to free.
func TestPlanCountsNothingInAStoreOfAnotherLayout(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", cachedFile{name: "Flash-Q8_0.gguf", size: 2000}).
		main("revision-one")
	marker := filepath.Join(tree.Root, "blobs", ".huggingface-shared-blobs")
	if err := os.Chmod(marker, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	if plan.Stored != 0 || plan.Shared != nil {
		t.Errorf("the plan leaves %d in the store and keeps %+v, want neither", plan.Stored, plan.Shared)
	}
}

// Without a manifest hf can read, hf never frees the payload — so neither does
// the plan count it as left for hf to free. It is kept, by no link anyone can
// name.
func TestPlanNeverCountsAStoredFileFreedWithoutAReadableManifest(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	cases := map[string]func(t *testing.T, manifest string){
		"missing": func(t *testing.T, manifest string) {
			if err := os.Remove(manifest); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable": func(t *testing.T, manifest string) {
			if os.Geteuid() == 0 {
				t.Skip("root reads a file whatever its mode")
			}
			if err := os.Chmod(manifest, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(manifest, 0o644) })
		},
		"not text": func(t *testing.T, manifest string) {
			if err := os.WriteFile(manifest, []byte{0xff, 0xfe, '\n'}, 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}

	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			tree := newCacheTree(t)
			tree.repo("models--unsloth--Flash-GGUF").
				snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
				stored("revision-one", stored).
				main("revision-one")
			payload := payloadOf(tree.Root, stored)
			spoil(t, payload+".refs")

			plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			want := []Shared{{Blob: payload, Bytes: 2000, Stored: true}}
			if plan.Stored != 0 || !reflect.DeepEqual(plan.Shared, want) {
				t.Errorf("the plan leaves %d in the store and keeps %+v, want nothing left and %+v kept",
					plan.Stored, plan.Shared, want)
			}
		})
	}
}

// A whole repository is judged by the same rule: what only it links is left in
// the store, and what another repository links is kept.
func TestPlanRepoJudgesItsStoredFilesByTheSameRule(t *testing.T) {
	own := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	common := cachedFile{name: "mmproj-BF16.gguf", size: 400}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "README.md", size: 100}).
		stored("revision-one", own, common).
		main("revision-one")
	other := tree.repo("models--unsloth--Small-GGUF").
		stored("revision-one", common).
		main("revision-one")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Flash-GGUF")
	plan, err := PlanRepo(repo, nil)
	if err != nil {
		t.Fatalf("planning the delete of %s: %v", repo.ID, err)
	}
	if plan.Stored != 2000 {
		t.Errorf("the plan leaves %d in the store, want the 2000 only this repository links", plan.Stored)
	}
	want := []Shared{{
		Blob:   payloadOf(tree.Root, common),
		Bytes:  400,
		Links:  []string{repoBlobOf(other, common)},
		Stored: true,
		Repos:  []string{"unsloth/Small-GGUF"},
	}}
	if !reflect.DeepEqual(plan.Shared, want) {
		t.Errorf("the plan keeps %+v, want %+v", plan.Shared, want)
	}

	applyPlan(t, tree.Root, plan)
}

// A delete that touches no stored file is planned exactly as before, though
// the cache has a shared store.
func TestPlanOfAnUnstoredQuantIgnoresTheStore(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "Flash-Q8_0.gguf", size: 1200}).
		main("revision-one")
	tree.repo("models--unsloth--Small-GGUF").
		stored("revision-one", cachedFile{name: "Small-Q8_0.gguf", size: 700}).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "UD-Q4_K_XL")
	if plan.Bytes != 3000 || plan.Stored != 0 || plan.Shared != nil || len(plan.Removes) != 2 {
		t.Errorf("the plan reclaims %d, leaves %d in the store, keeps %+v and removes %v; want 3000, nothing, nothing, the link and its blob",
			plan.Bytes, plan.Stored, plan.Shared, removalPaths(plan))
	}

	applyPlan(t, tree.Root, plan)
}

// flashWithQ8 is the fixture most store tests start from: a repository holding
// one quant of its own and Q8_0 in the shared store.
func flashWithQ8(t *testing.T, stored cachedFile) *cacheTree {
	t.Helper()
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		snapshot("revision-one", cachedFile{name: "Flash-UD-Q4_K_XL.gguf", size: 1200}).
		stored("revision-one", stored).
		main("revision-one")
	return tree
}

// writeManifest replaces a payload's .refs manifest with the given text.
func writeManifest(t *testing.T, root string, file cachedFile, text string) {
	t.Helper()
	if err := os.WriteFile(payloadOf(root, file)+".refs", []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// relToRoot is a path as a manifest line spells it.
func relToRoot(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

// hf counts a link from every repository directory it writes — kernels too,
// which the walk does not list — and names a repository the way an hf:// URI
// does.
func TestPlanKeepsAStoredFileAKernelRepoLinks(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	tree := flashWithQ8(t, stored)
	kernel := tree.repo("kernels--kernels-community--flash-attn").
		stored("revision-one", stored).
		main("revision-one")

	plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
	want := []Shared{{
		Blob:   payloadOf(tree.Root, stored),
		Bytes:  2000,
		Links:  []string{repoBlobOf(kernel, stored)},
		Stored: true,
		Repos:  []string{"kernels/kernels-community/flash-attn"},
	}}
	if plan.Stored != 0 || !reflect.DeepEqual(plan.Shared, want) {
		t.Errorf("the plan leaves %d in the store and keeps %+v, want nothing left and %+v kept", plan.Stored, plan.Shared, want)
	}
}

// A manifest hf can read but that names no valid link keeps nothing: the
// payload is left with no link, whether the manifest is empty or every line
// in it is one hf rejects.
func TestPlanCountsAStoredFileWhoseManifestNamesNoValidLink(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	cases := map[string]string{
		"empty": "",
		"only invalid lines": "/abs/models--unsloth--Flash-GGUF/blobs/x\n" +
			"models--unsloth--Flash-GGUF/../models--unsloth--Flash-GGUF/blobs/x\n" +
			"models--unsloth--Flash-GGUF/snapshots/x\n" +
			"bogus--unsloth--Flash-GGUF/blobs/x\n" +
			"models--/blobs/x\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			tree := flashWithQ8(t, stored)
			writeManifest(t, tree.Root, stored, text)

			plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			if plan.Stored != 2000 || plan.Shared != nil {
				t.Errorf("the plan leaves %d in the store and keeps %+v, want the 2000 left with no link and nothing kept",
					plan.Stored, plan.Shared)
			}
		})
	}
}

// hf reads a manifest with Python's read_text and splitlines: a link after
// any line boundary those know is a link, not part of the line before it.
func TestPlanReadsManifestLinesAtEveryBoundaryHfDoes(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	for name, boundary := range map[string]string{
		"carriage return": "\r", "crlf": "\r\n", "vertical tab": "\v", "form feed": "\f",
		"file separator": "\x1c", "group separator": "\x1d", "record separator": "\x1e",
		"next line": "\u0085", "line separator": " ", "paragraph separator": " ",
	} {
		t.Run(name, func(t *testing.T) {
			tree := flashWithQ8(t, stored)
			own := repoBlobOf(&repoTree{dir: filepath.Join(tree.Root, "models--unsloth--Flash-GGUF")}, stored)
			other := tree.repo("models--mirror--Flash-GGUF").stored("revision-one", stored)
			writeManifest(t, tree.Root, stored,
				relToRoot(t, tree.Root, own)+boundary+relToRoot(t, tree.Root, repoBlobOf(other, stored)))

			plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			if plan.Stored != 0 || len(plan.Shared) != 1 || !reflect.DeepEqual(plan.Shared[0].Repos, []string{"mirror/Flash-GGUF"}) {
				t.Errorf("the plan leaves %d in the store and keeps %+v, want the file kept by mirror/Flash-GGUF",
					plan.Stored, plan.Shared)
			}
		})
	}
}

// hf reads its marker with read_text too, so a marker ending in \r\n or \r
// marks the same store.
func TestPlanReadsTheMarkerTheWayHfDoes(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	for name, text := range map[string]string{"crlf": "1\r\n", "carriage return": "1\r"} {
		t.Run(name, func(t *testing.T) {
			tree := flashWithQ8(t, stored)
			marker := filepath.Join(tree.Root, "blobs", ".huggingface-shared-blobs")
			if err := os.Chmod(marker, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(marker, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}

			if plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0"); plan.Stored != 2000 {
				t.Errorf("the plan leaves %d in the store, want the 2000 of a store hf recognizes", plan.Stored)
			}
		})
	}
}

// A manifest line hf cannot check — its path runs through a file, or through
// a directory it may not read — makes the whole manifest unreadable to hf,
// which then never frees the payload.
func TestPlanNeverCountsAStoredFileFreedWhenAManifestLinkCannotBeRead(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	cases := map[string]func(t *testing.T, tree *cacheTree) string{
		"through a file": func(t *testing.T, tree *cacheTree) string {
			tree.write("models--ghost--Flash-GGUF", "not a directory")
			return "models--ghost--Flash-GGUF/blobs/abc"
		},
		"through a closed directory": func(t *testing.T, tree *cacheTree) string {
			if os.Geteuid() == 0 {
				t.Skip("root reads a directory whatever its mode")
			}
			// A kernel repository: the walk does not read one, so only hf's
			// rule meets the closed directory.
			other := tree.repo("kernels--mirror--flash-attn").stored("revision-one", stored)
			blobs := filepath.Join(other.dir, "blobs")
			if err := os.Chmod(blobs, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(blobs, 0o755) })
			return relToRoot(t, tree.Root, repoBlobOf(other, stored))
		},
	}
	for name, unreadable := range cases {
		t.Run(name, func(t *testing.T) {
			tree := flashWithQ8(t, stored)
			own := repoBlobOf(&repoTree{dir: filepath.Join(tree.Root, "models--unsloth--Flash-GGUF")}, stored)
			line := unreadable(t, tree)
			writeManifest(t, tree.Root, stored, relToRoot(t, tree.Root, own)+"\n"+line+"\n")

			plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			want := []Shared{{Blob: payloadOf(tree.Root, stored), Bytes: 2000, Stored: true}}
			if plan.Stored != 0 || !reflect.DeepEqual(plan.Shared, want) {
				t.Errorf("the plan leaves %d in the store and keeps %+v, want nothing left and %+v kept",
					plan.Stored, plan.Shared, want)
			}
		})
	}
}

// A stored copy a re-upload superseded is judged by the same rule: its delete
// leaves the old payload in the store with no link.
func TestPlanSupersededOfAStoredCopyLeavesItInTheStore(t *testing.T) {
	old := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	current := cachedFile{name: "Flash-Q8_0.gguf", text: "the re-uploaded Q8_0"}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Flash-GGUF").
		stored("revision-old", old).
		stored("revision-new", current).
		main("revision-new")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Flash-GGUF")
	plan, err := PlanSuperseded(repo, "Q8_0", nil)
	if err != nil {
		t.Fatalf("planning the delete of the superseded Q8_0: %v", err)
	}
	if plan.Bytes != 0 || plan.Stored != 2000 || plan.Shared != nil {
		t.Errorf("the plan reclaims %d, leaves %d in the store and keeps %+v; want nothing, the 2000 of the old copy, nothing",
			plan.Bytes, plan.Stored, plan.Shared)
	}

	applyPlan(t, tree.Root, plan)
}

// An execute refuses a plan whose stored file something has come to keep since,
// and says what keeps it.
func TestExecuteNamesWhatNowKeepsAStoredFile(t *testing.T) {
	stored := cachedFile{name: "Flash-Q8_0.gguf", size: 2000}
	cases := map[string]struct {
		change func(t *testing.T, tree *cacheTree)
		want   func(root string) string
	}{
		"another repository links it": {
			change: func(t *testing.T, tree *cacheTree) {
				tree.repo("models--mirror--Flash-GGUF").stored("revision-one", stored)
			},
			want: func(root string) string {
				return payloadOf(root, stored) + " in hf's shared store is now kept by mirror/Flash-GGUF"
			},
		},
		"its manifest is gone": {
			change: func(t *testing.T, tree *cacheTree) {
				if err := os.Remove(payloadOf(tree.Root, stored) + ".refs"); err != nil {
					t.Fatal(err)
				}
			},
			want: func(root string) string {
				return payloadOf(root, stored) + " in hf's shared store now has no readable .refs manifest, so hf keeps it"
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tree := flashWithQ8(t, stored)
			plan := planQuant(t, tree.Root, "unsloth/Flash-GGUF", "Q8_0")
			tc.change(t, tree)

			_, err := Execute(context.Background(), plan, nil, "")
			var drift *DriftError
			if !errors.As(err, &drift) {
				t.Fatalf("the execute answered %v, want a *DriftError", err)
			}
			if want := tc.want(tree.Root); drift.Change != want {
				t.Errorf("the refusal says %q, want %q", drift.Change, want)
			}
		})
	}
}
