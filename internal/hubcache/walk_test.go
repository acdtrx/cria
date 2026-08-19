package hubcache

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// mlxConfig is what mlx-lm writes into a quantized repo's config.json: the
// quantization block is the marker, and it is a block rather than a flag.
const mlxConfig = `{"model_type":"qwen3_moe","quantization":{"group_size":64,"bits":4,"mode":"affine"},"quantization_config":{"group_size":64,"bits":4}}`

// upstreamConfig is the same model before quantization — safetensors weights with
// no quantization block, which mlx_lm.server serves happily and cria must not
// mistake for an MLX quantization.
const upstreamConfig = `{"model_type":"qwen3_moe","transformers_version":"4.57.0"}`

// A GGUF repo lists one item per quantization. The projector and the file whose
// name carries no recognizable tag are items of their own — everything on disk is
// shown and deletable (docs/specs/CACHE.md).
func TestReadListsAGGUFRepoQuantByQuant(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--gemma-4-26B-A4B-it-qat-GGUF").
		snapshot("revision-one",
			cachedFile{name: "gemma-4-26B-A4B-it-qat-UD-Q4_K_XL.gguf", size: 4000},
			cachedFile{name: "gemma-4-26B-A4B-it-qat-UD-IQ2_M.gguf", size: 2000},
			cachedFile{name: "mmproj-BF16.gguf", size: 500},
			cachedFile{name: "mtp-gemma-4-26B-A4B-it.gguf", size: 300}).
		main("revision-one")

	repo := repoOf(t, read(t, tree.Root), "unsloth/gemma-4-26B-A4B-it-qat-GGUF")

	if repo.Kind != KindGGUF {
		t.Errorf("the repo is %s, want gguf", repo.Kind)
	}
	if repo.Type != RepoModel {
		t.Errorf("the repo type is %q, want model", repo.Type)
	}
	if repo.Revision != "revision-one" {
		t.Errorf("the current revision is %q, want revision-one", repo.Revision)
	}
	want := []string{"UD-IQ2_M", "UD-Q4_K_XL", "mmproj-BF16.gguf", "mtp-gemma-4-26B-A4B-it.gguf"}
	if got := itemLabels(repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("the repo lists %v, want %v", got, want)
	}
	for _, test := range []struct {
		label string
		bytes int64
	}{{"UD-Q4_K_XL", 4000}, {"UD-IQ2_M", 2000}, {"mmproj-BF16.gguf", 500}, {"mtp-gemma-4-26B-A4B-it.gguf", 300}} {
		item, ok := repo.Item(test.label)
		if !ok {
			t.Fatalf("the repo has no item %q", test.label)
		}
		if item.Bytes != test.bytes {
			t.Errorf("%s holds %d bytes, want %d", test.label, item.Bytes, test.bytes)
		}
		if !item.Complete {
			t.Errorf("%s is reported incomplete, but every file it names is on disk", test.label)
		}
		if len(item.Files) != 1 {
			t.Errorf("%s has %d files, want 1", test.label, len(item.Files))
		}
	}
	if repo.Bytes != diskUsage(t, repo.Dir) {
		t.Errorf("the repo reports %d bytes, want the %d its directory occupies", repo.Bytes, diskUsage(t, repo.Dir))
	}
	if !repo.Complete {
		t.Error("the repo is reported incomplete, but nothing is missing or downloading")
	}
}

// A quant split across shards is one item: the parts sum, and the -of-NNNNN the
// names carry says whether they are all there.
func TestReadFoldsShardsIntoOneItem(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3-235B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00001-of-00003.gguf", size: 1000},
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00002-of-00003.gguf", size: 1000},
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00003-of-00003.gguf", size: 500},
			cachedFile{name: "Qwen3-235B-UD-Q2_K_XL-00001-of-00002.gguf", size: 700}).
		main("revision-one")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3-235B-GGUF")

	if got, want := itemLabels(repo), []string{"UD-Q2_K_XL", "UD-Q4_K_XL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the repo lists %v, want %v", got, want)
	}

	whole, _ := repo.Item("UD-Q4_K_XL")
	if len(whole.Files) != 3 {
		t.Errorf("UD-Q4_K_XL has %d files, want its 3 shards", len(whole.Files))
	}
	if whole.Bytes != 2500 {
		t.Errorf("UD-Q4_K_XL holds %d bytes, want the 2500 its shards sum to", whole.Bytes)
	}
	if !whole.Complete {
		t.Error("UD-Q4_K_XL is reported incomplete, but all 3 of its shards are on disk")
	}

	// The second quant names two shards and only one arrived: the item exists,
	// and it is not servable.
	half, _ := repo.Item("UD-Q2_K_XL")
	if half.Complete {
		t.Error("UD-Q2_K_XL is reported complete, but shard 2 of 2 is missing")
	}
	if repo.Complete {
		t.Error("the repo is reported complete, but one of its shard series is short")
	}
}

// A repo that publishes a dynamic quantization beside a plain one holds two
// items: unsloth's prefix is part of the tag, so the two keep their own rows —
// and their own deletes. An entry finds the one whose tag it spells, and only
// that one.
func TestReadKeepsUDQuantsApartFromPlainOnes(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3-30B-A3B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3-30B-A3B-UD-Q4_K_XL.gguf", size: 4000},
			cachedFile{name: "Qwen3-30B-A3B-Q4_K_M.gguf", size: 3000},
			cachedFile{name: "Qwen3-30B-A3B-UD-Q2_K_XL-00001-of-00002.gguf", size: 900},
			cachedFile{name: "Qwen3-30B-A3B-UD-Q2_K_XL-00002-of-00002.gguf", size: 600}).
		main("revision-one")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3-30B-A3B-GGUF")

	want := []string{"Q4_K_M", "UD-Q2_K_XL", "UD-Q4_K_XL"}
	if got := itemLabels(repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("the repo lists %v, want %v", got, want)
	}
	for _, test := range []struct {
		quant string
		label string
		files int
		bytes int64
	}{
		{quant: "UD-Q4_K_XL", label: "UD-Q4_K_XL", files: 1, bytes: 4000},
		{quant: "Q4_K_M", label: "Q4_K_M", files: 1, bytes: 3000},
		{quant: "UD-Q2_K_XL", label: "UD-Q2_K_XL", files: 2, bytes: 1500},
	} {
		t.Run(test.quant, func(t *testing.T) {
			item, ok := repo.Item(test.quant)
			if !ok {
				t.Fatalf("the repo has no item for %q; it lists %v", test.quant, itemLabels(repo))
			}
			if item.Label != test.label {
				t.Fatalf("%q finds item %q, want %q", test.quant, item.Label, test.label)
			}
			if len(item.Files) != test.files || item.Bytes != test.bytes {
				t.Errorf("%s holds %d files worth %d bytes, want %d worth %d",
					item.Label, len(item.Files), item.Bytes, test.files, test.bytes)
			}
		})
	}

	// A tag spelled differently from the file's own is a different tag: cria
	// does not strip or add a provider's prefix to make it fit.
	for _, quant := range []string{"Q2_K_XL", "UD-Q4_K_M"} {
		t.Run("no item for "+quant, func(t *testing.T) {
			if item, ok := repo.Item(quant); ok {
				t.Errorf("%q finds item %q, want no item — the repo spells its tags otherwise", quant, item.Label)
			}
		})
	}
}

// A projector shares its precision token with a real quantization, and the two
// are different things: the projector pairs with any quant, so it keeps its own
// row and its own delete rather than merging into the quant that happens to spell
// the same precision.
func TestReadKeepsProjectorsOutOfQuantItems(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3-30B-A3B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3-30B-A3B-BF16.gguf", size: 6000},
			cachedFile{name: "mmproj-BF16.gguf", size: 500}).
		main("revision-one")
	tree.repo("models--unsloth--Qwen3-235B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00001-of-00002.gguf", size: 1000},
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00002-of-00002.gguf", size: 800},
			cachedFile{name: "mmproj-F16.gguf", size: 400}).
		main("revision-one")

	cache := read(t, tree.Root)

	// The full-precision weights and the projector both spell BF16 and stay apart.
	paired := repoOf(t, cache, "unsloth/Qwen3-30B-A3B-GGUF")
	if got, want := itemLabels(paired), []string{"BF16", "mmproj-BF16.gguf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the repo lists %v, want %v", got, want)
	}
	for _, test := range []struct {
		label string
		bytes int64
	}{{"BF16", 6000}, {"mmproj-BF16.gguf", 500}} {
		item, ok := paired.Item(test.label)
		if !ok {
			t.Fatalf("the repo has no item %q", test.label)
		}
		if len(item.Files) != 1 || item.Bytes != test.bytes {
			t.Errorf("%s holds %d files worth %d bytes, want 1 worth %d",
				test.label, len(item.Files), item.Bytes, test.bytes)
		}
	}

	// Shards still fold, and the projector beside them is still its own item.
	sharded := repoOf(t, cache, "unsloth/Qwen3-235B-GGUF")
	if got, want := itemLabels(sharded), []string{"UD-Q4_K_XL", "mmproj-F16.gguf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the repo lists %v, want %v", got, want)
	}
	quant, _ := sharded.Item("UD-Q4_K_XL")
	if len(quant.Files) != 2 || quant.Bytes != 1800 || !quant.Complete {
		t.Errorf("UD-Q4_K_XL holds %d files worth %d bytes (complete=%v), want its 2 shards worth 1800, complete",
			len(quant.Files), quant.Bytes, quant.Complete)
	}
	projector, _ := sharded.Item("mmproj-F16.gguf")
	if len(projector.Files) != 1 || projector.Bytes != 400 {
		t.Errorf("mmproj-F16.gguf holds %d files worth %d bytes, want 1 worth 400", len(projector.Files), projector.Bytes)
	}
}

// A blob two snapshots point at is one file and one set of bytes — the number
// that makes the cache total trustworthy (docs/specs/CACHE.md).
func TestReadCountsABlobSharedBySnapshotsOnce(t *testing.T) {
	shared := cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-old", shared, cachedFile{name: "mmproj-BF16.gguf", size: 400}).
		snapshot("revision-new", shared).
		main("revision-new")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")

	if len(repo.Files) != 2 {
		t.Fatalf("the repo lists %d files, want 2 distinct blobs: %v", len(repo.Files), names(repo.Files))
	}
	item, ok := repo.Item("UD-Q4_K_XL")
	if !ok {
		t.Fatal("the repo has no UD-Q4_K_XL item")
	}
	if item.Bytes != 3000 {
		t.Errorf("UD-Q4_K_XL holds %d bytes, want the 3000 of its one blob", item.Bytes)
	}
	if len(item.Files[0].Links) != 2 {
		t.Errorf("the shared blob is reached by %d snapshot entries, want 2 — a delete has to remove both",
			len(item.Files[0].Links))
	}
	// Both revisions name it the same, and the file keeps that name once.
	if item.Files[0].Name != shared.name {
		t.Errorf("the shared blob is named %q, want %q", item.Files[0].Name, shared.name)
	}
}

// A provider who requantizes a repo leaves two copies of one file name on disk,
// under different blobs and different snapshots. The item is what the repo holds
// under that tag today; the copy the current revision no longer names is
// superseded — its own bytes, reclaimable on their own — and a file both
// revisions share is not superseded at all (docs/specs/CACHE.md).
func TestReadHoldsSupersededCopiesApartFromCurrentOnes(t *testing.T) {
	projector := cachedFile{name: "mmproj-BF16.gguf", size: 400}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-old",
			cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 3000},
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 5000},
			projector).
		snapshot("revision-new",
			cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 2800},
			projector).
		main("revision-new")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")

	// The requantized tag: one item, holding the copy the repo names now, with
	// the old one hanging off it.
	quant, ok := repo.Item("UD-Q2_K_XL")
	if !ok {
		t.Fatalf("the repo lists %v, want the requantized tag among them", itemLabels(repo))
	}
	if quant.Bytes != 2800 || len(quant.Files) != 1 {
		t.Errorf("UD-Q2_K_XL holds %d files worth %d bytes, want the current copy alone: 1 worth 2800",
			len(quant.Files), quant.Bytes)
	}
	if len(quant.Superseded) != 1 || quant.SupersededBytes != 3000 {
		t.Fatalf("UD-Q2_K_XL carries %d superseded copies worth %d bytes, want 1 worth 3000",
			len(quant.Superseded), quant.SupersededBytes)
	}
	if got := quant.Superseded[0].Name; got != "Qwen3.8-27B-UD-Q2_K_XL.gguf" {
		t.Errorf("the superseded copy is named %q, want the file name it still carries", got)
	}
	if quant.Superseded[0].Blob == quant.Files[0].Blob {
		t.Error("the superseded copy and the current one are the same blob")
	}

	// A tag only the older revision ever named is not superseded: it is the only
	// copy of that file on disk, and the repo still holds it.
	untouched, ok := repo.Item("UD-Q4_K_XL")
	if !ok {
		t.Fatalf("the repo lists %v, want the quant only the old revision names", itemLabels(repo))
	}
	if untouched.Bytes != 5000 || len(untouched.Superseded) != 0 {
		t.Errorf("UD-Q4_K_XL holds %d bytes with %d superseded copies, want 5000 and none",
			untouched.Bytes, len(untouched.Superseded))
	}

	// The file both revisions share is one blob two names reach, not a copy that
	// was replaced.
	shared, ok := repo.Item("mmproj-BF16.gguf")
	if !ok {
		t.Fatalf("the repo lists %v, want the projector among them", itemLabels(repo))
	}
	if shared.Bytes != 400 || len(shared.Superseded) != 0 {
		t.Errorf("the projector holds %d bytes with %d superseded copies, want 400 and none",
			shared.Bytes, len(shared.Superseded))
	}

	if repo.SupersededBytes != 3000 || len(repo.Superseded) != 1 {
		t.Errorf("the repo reports %d superseded files worth %d bytes, want 1 worth 3000",
			len(repo.Superseded), repo.SupersededBytes)
	}
	// The old copy is on disk, so the repo total still counts it: the header
	// number is the one du reports (docs/specs/CACHE.md).
	if want := diskUsage(t, repo.Dir); repo.Bytes != want {
		t.Errorf("the repo reports %d bytes, want the %d its directory occupies", repo.Bytes, want)
	}
}

// Which copy is current is the ref's answer first. Without one — a repo whose
// refs/main was never written, or names a revision the cache no longer holds —
// it is the newest snapshot naming that file (docs/specs/CACHE.md).
func TestTheCurrentCopyIsTheRefsAndThenTheNewestSnapshots(t *testing.T) {
	old := cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 3000}
	recent := cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 2800}

	tests := []struct {
		name    string
		ref     string
		current int64
	}{
		{name: "refs/main names the older revision", ref: "revision-old", current: 3000},
		{name: "refs/main names the newer one", ref: "revision-new", current: 2800},
		{name: "no ref at all", current: 2800},
		{name: "the ref names a revision the cache does not hold", ref: "revision-gone", current: 2800},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := newCacheTree(t)
			repo := tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
				snapshot("revision-old", old).
				snapshot("revision-new", recent).
				aged("revision-old", time.Date(2026, 8, 14, 18, 22, 0, 0, time.UTC)).
				aged("revision-new", time.Date(2026, 8, 19, 16, 52, 0, 0, time.UTC))
			if test.ref != "" {
				repo.main(test.ref)
			}

			quant, ok := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF").Item("UD-Q2_K_XL")
			if !ok {
				t.Fatal("the repo has no UD-Q2_K_XL item")
			}
			if quant.Bytes != test.current {
				t.Errorf("the current copy holds %d bytes, want %d", quant.Bytes, test.current)
			}
			if len(quant.Superseded) != 1 || quant.Superseded[0].Bytes == test.current {
				t.Errorf("the superseded set is %v, want the one copy that is not current", quant.Superseded)
			}
		})
	}
}

// A whole repository can be republished, and an MLX model is one unit: its
// superseded copies belong to the repo rather than to any quant of it.
func TestASupersededCopyInARepoWithNoItems(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-old",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00001.safetensors", size: 4000}).
		snapshot("revision-new",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00001.safetensors", size: 4200}).
		main("revision-new")

	repo := repoOf(t, read(t, tree.Root), "mlx-community/Qwen3.8-27B-4bit")

	if repo.Kind != KindMLX {
		t.Errorf("the repo is %s, want mlx", repo.Kind)
	}
	if len(repo.Superseded) != 1 || repo.SupersededBytes != 4000 {
		t.Fatalf("the repo reports %d superseded files worth %d bytes, want 1 worth 4000",
			len(repo.Superseded), repo.SupersededBytes)
	}
	// config.json did not change between the two revisions: one blob, two links,
	// nothing superseded.
	if got := repo.Superseded[0].Name; got != "model-00001-of-00001.safetensors" {
		t.Errorf("the superseded copy is %q, want the weights that changed", got)
	}
	if len(repo.Files) != 2 {
		t.Errorf("the repo holds %v, want the current config and the current weights", names(repo.Files))
	}
}

// The MLX marker is mlx-lm's quantization block in config.json — verified against
// the real cache, where mlx-community and unsloth MLX repos both carry it and the
// unquantized upstream does not.
func TestReadTagsRepositoriesByKind(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00002.safetensors", size: 2000},
			cachedFile{name: "model-00002-of-00002.safetensors", size: 2000},
			cachedFile{name: "tokenizer.json", size: 100}).
		main("revision-one")
	tree.repo("models--Qwen--Qwen3.6-35B-A3B").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: upstreamConfig},
			cachedFile{name: "model-00001-of-00001.safetensors", size: 5000}).
		main("revision-one")
	tree.repo("models--Qwen--Qwen3.6-35B-A3B-tokenizer-only").
		snapshot("revision-one", cachedFile{name: "tokenizer.json", size: 120}).
		main("revision-one")
	tree.repo("datasets--allenai--c4").
		snapshot("revision-one", cachedFile{name: "train.parquet", size: 900}).
		main("revision-one")

	cache := read(t, tree.Root)

	for _, test := range []struct {
		id       string
		kind     Kind
		repoType RepoType
	}{
		{"mlx-community/Qwen3.8-27B-4bit", KindMLX, RepoModel},
		{"Qwen/Qwen3.6-35B-A3B", KindOther, RepoModel},
		{"Qwen/Qwen3.6-35B-A3B-tokenizer-only", KindOther, RepoModel},
		{"allenai/c4", KindOther, RepoDataset},
	} {
		repo := repoOf(t, cache, test.id)
		if repo.Kind != test.kind {
			t.Errorf("%s is tagged %s, want %s", test.id, repo.Kind, test.kind)
		}
		if repo.Type != test.repoType {
			t.Errorf("%s is a %q, want a %q", test.id, repo.Type, test.repoType)
		}
		if len(repo.Items) != 0 {
			t.Errorf("%s lists quants %v; only a GGUF repo has any — it is selected whole (docs/specs/CACHE.md)",
				test.id, itemLabels(repo))
		}
	}
}

// Unfinished downloads are reported apart from the files, as reclaimable bytes.
// Both downloaders cria drives leave one behind, under their own names, and
// llama-server can leave a repo that is nothing but a partial.
func TestReadReportsPartialDownloads(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.6-35B-A3B-GGUF").
		partial("707a55a8a4397ecde44de0c499d3e68c1ad1d240d1da65826b4949d1043f4450.downloadInProgress", 1500).
		main("revision-one")
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00002.safetensors", size: 2000}).
		partial("31b8c91ef899f79efaaa69e3d2c096f6e2ebeb2ff20e29222abbd9ebc79e560a.incomplete", 800).
		main("revision-one")

	cache := read(t, tree.Root)

	llama := repoOf(t, cache, "unsloth/Qwen3.6-35B-A3B-GGUF")
	if len(llama.Partials) != 1 || llama.PartialBytes != 1500 {
		t.Errorf("the interrupted GGUF repo reports %d partials worth %d bytes, want 1 worth 1500",
			len(llama.Partials), llama.PartialBytes)
	}
	// A partial is named after the blob it is becoming, whichever downloader
	// left it: that hash is what says which file of which revision is landing.
	if want := "707a55a8a4397ecde44de0c499d3e68c1ad1d240d1da65826b4949d1043f4450"; llama.Partials[0].Blob != want {
		t.Errorf("the partial is becoming blob %q, want %q", llama.Partials[0].Blob, want)
	}
	if len(llama.Files) != 0 {
		t.Errorf("the interrupted GGUF repo lists files %v; nothing has reached a snapshot yet", names(llama.Files))
	}
	if llama.Complete {
		t.Error("the interrupted GGUF repo is reported complete, but it holds nothing but a partial download")
	}

	mlx := repoOf(t, cache, "mlx-community/Qwen3.8-27B-4bit")
	if len(mlx.Partials) != 1 || mlx.PartialBytes != 800 {
		t.Errorf("the interrupted MLX repo reports %d partials worth %d bytes, want 1 worth 800",
			len(mlx.Partials), mlx.PartialBytes)
	}
	if want := "31b8c91ef899f79efaaa69e3d2c096f6e2ebeb2ff20e29222abbd9ebc79e560a"; mlx.Partials[0].Blob != want {
		t.Errorf("the partial is becoming blob %q, want %q", mlx.Partials[0].Blob, want)
	}
	if mlx.Complete {
		t.Error("the interrupted MLX repo is reported complete, but a download is still in flight")
	}

	if cache.PartialBytes != 2300 {
		t.Errorf("the cache reports %d reclaimable bytes, want the 2300 both partials sum to", cache.PartialBytes)
	}
	// Partial bytes are on disk, so the cache total covers them too.
	if cache.Bytes != diskUsage(t, tree.Root) {
		t.Errorf("the cache reports %d bytes, want the %d the tree occupies", cache.Bytes, diskUsage(t, tree.Root))
	}
}

// The number in the header is the one a user checks against du, so the walk has
// to account for every byte in the tree — shared blobs, shards, partials and all.
func TestCacheTotalMatchesADiskWalk(t *testing.T) {
	shared := cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000}
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-old", shared, cachedFile{name: "mmproj-BF16.gguf", size: 400}).
		snapshot("revision-new", shared, cachedFile{name: "Qwen3.8-27B-UD-Q2_K_XL.gguf", size: 1200}).
		partial("aaaa.downloadInProgress", 640).
		main("revision-new")
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00002.safetensors", size: 2500},
			cachedFile{name: "model-00002-of-00002.safetensors", size: 2500}).
		main("revision-one")
	tree.repo("datasets--allenai--c4").
		snapshot("revision-one", cachedFile{name: "train.parquet", size: 900}).
		main("revision-one")

	cache := read(t, tree.Root)

	if want := diskUsage(t, tree.Root); cache.Bytes != want {
		t.Errorf("the cache reports %d bytes, want the %d the tree occupies", cache.Bytes, want)
	}
	var repoTotal int64
	for _, repo := range cache.Repos {
		repoTotal += repo.Bytes
	}
	if repoTotal != cache.Bytes {
		t.Errorf("the repos sum to %d bytes, want the cache total %d", repoTotal, cache.Bytes)
	}
}

// A fresh host has no cache directory at all, and a host that only ever ran `hf`
// may have an empty one. Neither is an error.
func TestReadOfAnEmptyCache(t *testing.T) {
	tree := newCacheTree(t)

	for name, root := range map[string]string{
		"an empty hub directory": tree.Root,
		"no hub directory yet":   filepath.Join(tree.Root, "never-downloaded"),
	} {
		t.Run(name, func(t *testing.T) {
			cache := read(t, root)
			if len(cache.Repos) != 0 {
				t.Errorf("the walk found repos %v, want none", repoIDs(cache))
			}
			if cache.Bytes != 0 || cache.PartialBytes != 0 {
				t.Errorf("the walk reports %d bytes (%d reclaimable), want 0", cache.Bytes, cache.PartialBytes)
			}
			if cache.Root != root {
				t.Errorf("the result names root %q, want %q", cache.Root, root)
			}
		})
	}
}

// The hub directory holds bookkeeping of its own next to the repositories. Only
// what is shaped like a repository is a repository.
func TestReadSkipsWhatIsNotARepository(t *testing.T) {
	tree := newCacheTree(t)
	tree.write("CACHEDIR.TAG", "Signature: 8a477f597d28d172789f06886806bc55")
	tree.write(filepath.Join(".locks", "models--gone--Model", "abc.lock"), "")
	tree.write("version.txt", "1")
	tree.repo("models--LiquidAI--LFM2.5-2.6B-GGUF").
		snapshot("revision-one", cachedFile{name: "LFM2.5-2.6B-Q8_0.gguf", size: 700}).
		main("revision-one")

	cache := read(t, tree.Root)

	if got, want := repoIDs(cache), []string{"LiquidAI/LFM2.5-2.6B-GGUF"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the walk found %v, want %v", got, want)
	}
	// The skipped entries are not repositories, so their bytes are not cache bytes.
	if want := diskUsage(t, cache.Repos[0].Dir); cache.Bytes != want {
		t.Errorf("the cache reports %d bytes, want the %d its one repo occupies", cache.Bytes, want)
	}
}

// A snapshot entry whose blob is gone points at nothing: those bytes are not on
// disk, so the quant reads as absent rather than as an item of size zero.
func TestReadIgnoresADanglingSnapshotEntry(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one", cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 900}).
		dangling("revision-one", "Qwen3.8-27B-UD-Q8_0.gguf").
		main("revision-one")

	repo := repoOf(t, read(t, tree.Root), "unsloth/Qwen3.8-27B-GGUF")

	if got, want := itemLabels(repo), []string{"UD-Q4_K_XL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the repo lists %v, want %v", got, want)
	}
}

// A repo whose snapshot nests directories keeps the paths the repo gives its
// files.
func TestReadKeepsNestedSnapshotPaths(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--Qwen--Qwen3-Embedding-0.6B").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: upstreamConfig},
			cachedFile{name: filepath.Join("onnx", "model.onnx"), size: 400}).
		main("revision-one")

	repo := repoOf(t, read(t, tree.Root), "Qwen/Qwen3-Embedding-0.6B")

	found := false
	for _, file := range repo.Files {
		if file.Name == "onnx/model.onnx" {
			found = true
		}
	}
	if !found {
		t.Errorf("the repo lists %v, want the nested onnx/model.onnx among them", names(repo.Files))
	}
}

// Root follows huggingface_hub's own resolution, so cria reads the tree hf and
// the servers write into rather than a path of its own invention.
func TestRootFollowsHuggingFaceHubResolution(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot read the home directory: %v", err)
	}

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "HF_HUB_CACHE names the hub directory outright",
			env:  map[string]string{"HF_HUB_CACHE": "/models/hub", "HF_HOME": "/models/hf", "XDG_CACHE_HOME": "/cache"},
			want: "/models/hub",
		},
		{
			name: "HF_HOME holds it under hub",
			env:  map[string]string{"HF_HOME": "/models/hf", "XDG_CACHE_HOME": "/cache"},
			want: filepath.Join("/models/hf", "hub"),
		},
		{
			name: "the XDG cache location otherwise",
			env:  map[string]string{"XDG_CACHE_HOME": "/cache"},
			want: filepath.Join("/cache", "huggingface", "hub"),
		},
		{
			name: "and ~/.cache when nothing is set",
			want: filepath.Join(home, ".cache", "huggingface", "hub"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range []string{"HF_HUB_CACHE", "HF_HOME", "XDG_CACHE_HOME"} {
				t.Setenv(name, test.env[name])
			}
			root, err := Root()
			if err != nil {
				t.Fatalf("resolving the hub cache: %v", err)
			}
			if root != test.want {
				t.Errorf("the hub cache resolved to %q, want %q", root, test.want)
			}
		})
	}
}
