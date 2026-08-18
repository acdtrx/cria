package hubcache

import (
	"testing"

	"cria/internal/config"
)

// A llama entry serves one quantization out of its repo, so that quant — not the
// repo — is what has to be there before the server can start without downloading.
func TestPresenceOfALlamaEntry(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--unsloth--Qwen3.8-27B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3.8-27B-UD-Q4_K_XL.gguf", size: 3000},
			cachedFile{name: "mmproj-BF16.gguf", size: 400}).
		main("revision-one")
	tree.repo("models--unsloth--Qwen3-235B-GGUF").
		snapshot("revision-one",
			cachedFile{name: "Qwen3-235B-UD-Q4_K_XL-00001-of-00002.gguf", size: 1000}).
		partial("bbbb.downloadInProgress", 250).
		main("revision-one")
	tree.repo("models--unsloth--Qwen3.6-35B-A3B-GGUF").
		partial("cccc.downloadInProgress", 1500).
		main("revision-one")

	cache := read(t, tree.Root)

	tests := []struct {
		name  string
		entry config.Entry
		want  Presence
	}{
		{
			// The tag as unsloth documents it and as the file spells it.
			name:  "the quant is on disk",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3.8-27B-GGUF", Quant: "UD-Q4_K_XL"},
			want:  Presence{Cached: true, Bytes: 3000},
		},
		{
			// The same tag written without the prefix: one item answers to it.
			name:  "the quant is on disk, spelled without the UD prefix",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3.8-27B-GGUF", Quant: "Q4_K_XL"},
			want:  Presence{Cached: true, Bytes: 3000},
		},
		{
			// llama-server's -hf repo:TAG matching ignores case, so the answer must too.
			name:  "the quant is on disk, spelled in another case",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3.8-27B-GGUF", Quant: "ud-q4_k_xl"},
			want:  Presence{Cached: true, Bytes: 3000},
		},
		{
			name:  "the repo is there but not this quant",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3.8-27B-GGUF", Quant: "Q8_0"},
			want:  Presence{},
		},
		{
			name:  "no quant named: any whole quant answers for the repo",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3.8-27B-GGUF"},
			want:  Presence{Cached: true, Bytes: repoOf(t, cache, "unsloth/Qwen3.8-27B-GGUF").Bytes},
		},
		{
			// One shard landed, one is still coming: not servable, and both the
			// finished shard and the partial are progress.
			name:  "the quant is mid-download, shard by shard",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3-235B-GGUF", Quant: "Q4_K_XL"},
			want:  Presence{Bytes: 1250},
		},
		{
			name:  "nothing has reached a snapshot yet",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3.6-35B-A3B-GGUF", Quant: "Q4_K_XL"},
			want:  Presence{Bytes: 1500},
		},
		{
			name:  "the repo is not in the cache at all",
			entry: config.Entry{Backend: config.BackendLlama, Repo: "unsloth/never-downloaded-GGUF", Quant: "Q4_K_M"},
			want:  Presence{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := cache.Presence(test.entry); got != test.want {
				t.Errorf("presence is %+v, want %+v", got, test.want)
			}
		})
	}
}

// An MLX quantization is its own repo, so the whole repo is the unit: it is there
// when every file it declares is there and nothing is still downloading.
func TestPresenceOfAnMLXEntry(t *testing.T) {
	tree := newCacheTree(t)
	tree.repo("models--mlx-community--Qwen3.8-27B-4bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00002.safetensors", size: 2000},
			cachedFile{name: "model-00002-of-00002.safetensors", size: 2000},
			cachedFile{name: "tokenizer.json", size: 100}).
		main("revision-one")
	tree.repo("models--mlx-community--Qwen3.8-27B-8bit").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: mlxConfig},
			cachedFile{name: "model-00001-of-00003.safetensors", size: 2000}).
		partial("dddd.incomplete", 700).
		main("revision-one")
	// mlx_lm.server serves unquantized safetensors too, so an entry pointing at
	// one is cached on the same terms even though the cache tags it "other".
	tree.repo("models--Qwen--Qwen3.6-35B-A3B").
		snapshot("revision-one",
			cachedFile{name: "config.json", text: upstreamConfig},
			cachedFile{name: "model-00001-of-00001.safetensors", size: 5000}).
		main("revision-one")

	cache := read(t, tree.Root)

	tests := []struct {
		name       string
		repo       string
		wantCached bool
	}{
		{name: "the whole repo is on disk", repo: "mlx-community/Qwen3.8-27B-4bit", wantCached: true},
		{name: "two of three shards, and one still downloading", repo: "mlx-community/Qwen3.8-27B-8bit"},
		{name: "an unquantized repo mlx_lm.server can serve", repo: "Qwen/Qwen3.6-35B-A3B", wantCached: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presence := cache.Presence(config.Entry{Backend: config.BackendMLX, Repo: test.repo})
			if presence.Cached != test.wantCached {
				t.Errorf("cached is %v, want %v", presence.Cached, test.wantCached)
			}
			// The whole repo is the entry's model, downloaded or not, so its
			// bytes are the progress numerator either way.
			if want := repoOf(t, cache, test.repo).Bytes; presence.Bytes != want {
				t.Errorf("presence reports %d bytes, want the repo's %d", presence.Bytes, want)
			}
		})
	}
}

// An entry naming a repo nothing ever downloaded has nothing on disk.
func TestPresenceOfAnUncachedMLXEntry(t *testing.T) {
	cache := read(t, newCacheTree(t).Root)

	presence := cache.Presence(config.Entry{Backend: config.BackendMLX, Repo: "mlx-community/never-downloaded"})

	if (presence != Presence{}) {
		t.Errorf("presence is %+v, want nothing on disk", presence)
	}
}
