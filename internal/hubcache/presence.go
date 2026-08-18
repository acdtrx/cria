package hubcache

import (
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
