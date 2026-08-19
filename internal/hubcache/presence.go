package hubcache

import (
	"path/filepath"

	"cria/internal/config"
)

// Presence answers, for one config entry, whether starting it would download
// anything and how much of its model is already on disk. The serve layer marks an
// entry `downloading` on the negative answer and renders the bytes as progress
// (docs/specs/SERVE.md); the TUI marks the entry list from the same answer
// (docs/specs/TUI.md).
//
// Both downloaders write a snapshot entry only once a file is fully in place, so
// "the file is reachable from a snapshot" already means "it finished". What
// unfinished bytes there are sit in blobs/ under a name no snapshot points at,
// counted here but never mistaken for a present file.
func (c *Cache) Presence(entry config.Entry) Presence {
	repo, cached := c.Repo(entry.Repo)
	if !cached {
		return Presence{}
	}
	if entry.Backend == config.BackendLlama {
		return llamaPresence(repo, entry.Quant)
	}
	// An MLX model is its whole repo — the quantization is the repo
	// (docs/cria.md, principle 2) — so the repo being whole is the answer, and
	// what it occupies is what the entry occupies.
	return Presence{Cached: repo.Complete, Bytes: repo.Bytes}
}

// llamaPresence answers for a llama entry, which serves one quantization out of a
// repo that may hold several.
func llamaPresence(repo *Repo, quant string) Presence {
	if quant == "" {
		// The server picks the repo's default quantization and cria cannot know
		// which file that will be without asking the Hub. Any complete quant is
		// the closest true answer, and the whole repo is what is on disk for it.
		return Presence{Cached: anyComplete(repo.Items), Bytes: repo.Bytes}
	}
	item, present := repo.Item(quant)
	if !present {
		// Nothing of this quant has landed yet; whatever is downloading in this
		// repo is the only progress there is to show.
		return Presence{Bytes: repo.PartialBytes}
	}
	return Presence{Cached: item.Complete, Bytes: item.Bytes + repo.PartialBytes}
}

// Fetching answers whether this repository is receiving one particular set of
// blobs right now, and how many of their bytes are already on disk. The blobs
// are a model's files as the Hub names them for the revision it publishes today
// (internal/hubapi), so the answer is about that model rather than about the
// repository: an unfinished download of another quant in the same repo is
// somebody else's.
//
// It is what tells a silent re-download from a slow start (docs/specs/SERVE.md).
// When a provider republishes a quant, the copy on disk is still whole and the
// entry still reads cached — the only sign that a server is fetching gigabytes
// before it loads anything is an unfinished download named after the file the
// Hub publishes now.
//
// The bytes are the model's progress rather than one file's: a file still
// landing counts what it holds so far, and one that already landed under its
// current hash counts whole — so a re-fetched shard series climbs instead of
// falling back to zero at every shard.
func (r *Repo) Fetching(blobs []string) (int64, bool) {
	landing := map[string]int64{}
	for _, partial := range r.Partials {
		landing[partial.Blob] = partial.Bytes
	}
	landed := map[string]int64{}
	for _, file := range r.Files {
		landed[filepath.Base(file.Blob)] = file.Bytes
	}

	var bytes int64
	fetching := false
	for _, blob := range blobs {
		if unfinished, still := landing[blob]; still {
			bytes += unfinished
			fetching = true
			continue
		}
		bytes += landed[blob]
	}
	if !fetching {
		return 0, false
	}
	return bytes, true
}

// anyComplete reports whether a repo holds at least one quantization that is
// whole.
func anyComplete(items []Item) bool {
	for _, item := range items {
		if item.Complete {
			return true
		}
	}
	return false
}
