// Package hubcache reads the Hugging Face hub cache — the single source of truth
// for model bytes (docs/cria.md, principle 2). It answers two questions: what the
// cache holds, with the true blob-deduped sizes docs/specs/CACHE.md promises, and
// whether a config entry's model is already fully present.
//
// Everything here is filesystem observation: no Hub API call, no `hf` exec, no
// server output parsed. The result is data — naming repos, their quants, their
// files and their bytes belongs here; deciding how to show any of it belongs to
// the TUI and the CLI.
//
// This is the read side. Deletion is a separate, deliberate operation.
package hubcache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Kind is what cria can do with a cached repo: serve GGUF quants out of it, serve
// it whole the way MLX models are served, or nothing at all.
type Kind int

const (
	KindGGUF  Kind = iota // holds .gguf files; its quants are the selectable units
	KindMLX               // safetensors weights whose config.json carries mlx-lm's quantization block
	KindOther             // everything else the cache happens to hold
)

// String names the kind the way the cache view spells it.
func (k Kind) String() string {
	switch k {
	case KindGGUF:
		return "gguf"
	case KindMLX:
		return "mlx"
	case KindOther:
		return "other"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// RepoType is the Hub namespace a cached repo belongs to, read off the directory
// prefix. Models are what cria serves; the cache holds the others too, and the
// view shows them rather than pretending the disk they use does not exist.
type RepoType string

const (
	RepoModel   RepoType = "model"
	RepoDataset RepoType = "dataset"
	RepoSpace   RepoType = "space"
)

// Cache is one read of the hub cache.
type Cache struct {
	Root         string // the hub directory this read walked
	Repos        []Repo // ordered by id
	Bytes        int64  // what the cache occupies on disk
	PartialBytes int64  // the reclaimable subset: unfinished downloads
}

// Repo is one repository directory — models--org--name and its dataset and space
// siblings.
type Repo struct {
	ID           string    // org/name, the way the Hub spells it
	Type         RepoType  // which Hub namespace it came from
	Kind         Kind      // what cria can do with it
	Dir          string    // the repository directory
	Revision     string    // the snapshot refs/main names; empty when the repo has no main ref
	Items        []Item    // the quants of a KindGGUF repo, ordered by label; empty otherwise, where the repo itself is the unit (docs/specs/CACHE.md)
	Files        []File    // every distinct blob the snapshots reach, ordered by name
	Partials     []Partial // unfinished downloads, ordered by path
	Bytes        int64     // what the repository directory occupies on disk, partials included
	PartialBytes int64     // the reclaimable subset
	Complete     bool      // the repo holds files, every shard series they declare is whole, and nothing is still downloading
	Modified     time.Time // the newest blob in the repo — when its bytes landed
}

// Item is a selectable unit inside a GGUF repo: one quantization, its shards
// folded into a single thing (docs/specs/CACHE.md).
type Item struct {
	Label    string    // the quantization as the file names spell it (Q4_K_M), or the file name when no quant token is recognizable
	Files    []File    // ordered by name; more than one only for a sharded quant
	Bytes    int64     // its blobs, each counted once
	Complete bool      // every shard the file names declare is present
	Modified time.Time // the newest of its blobs
}

// File is one distinct blob a repo's snapshots reach. Snapshots hold names,
// blobs hold bytes; several snapshots pointing at the same blob are one file
// here, because that is what the disk holds.
type File struct {
	Name     string    // the name the snapshots give it
	Blob     string    // the file holding the bytes: a blob, or the snapshot entry itself where the cache stores copies instead of symlinks
	Links    []string  // the snapshot entries pointing at Blob, ordered; a delete has to remove every one of them (docs/specs/CACHE.md)
	Bytes    int64     // the blob's size
	Modified time.Time // when the blob landed
}

// Partial is an unfinished download: bytes sitting in blobs/ that no snapshot can
// reach yet. It is reclaimable on its own (docs/specs/CACHE.md), which is why it
// is reported apart from the files rather than mixed into them.
type Partial struct {
	Path     string
	Bytes    int64
	Modified time.Time
}

// Presence is what the cache holds for one config entry: whether starting it
// would download anything, and how many of its bytes are on disk right now — the
// numerator of the downloading progress (docs/specs/SERVE.md).
type Presence struct {
	Cached bool  // the entry's model is fully present; starting it fetches nothing
	Bytes  int64 // what it occupies so far, unfinished downloads included
}

// Root resolves the hub cache directory the way huggingface_hub does, so cria
// reads exactly the tree hf and the servers write into: HF_HUB_CACHE names it
// outright, else HF_HOME holds it under hub/, else it sits in the XDG cache
// location — ~/.cache when the variable is unset.
func Root() (string, error) {
	if dir := os.Getenv("HF_HUB_CACHE"); dir != "" {
		return dir, nil
	}
	if home := os.Getenv("HF_HOME"); home != "" {
		return filepath.Join(home, "hub"), nil
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot locate the home directory that holds the Hugging Face cache: %w", err)
		}
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "huggingface", "hub"), nil
}

// Repo finds a repository by its Hub id. The match is exact: the cache directory
// is named after the id the download asked for, so an entry spelled with
// different capitalisation genuinely names a different directory — and the server
// would fetch it again into that one.
func (c *Cache) Repo(id string) (*Repo, bool) {
	for i := range c.Repos {
		if c.Repos[i].ID == id {
			return &c.Repos[i], true
		}
	}
	return nil, false
}

// Item finds the quantization a llama entry names, by the rule MatchQuant
// holds: the spellings llama-server would accept for the same tag all find the
// same item.
func (r *Repo) Item(quant string) (*Item, bool) {
	labels := make([]string, len(r.Items))
	for i := range r.Items {
		labels[i] = r.Items[i].Label
	}
	match, found := MatchQuant(labels, quant)
	if !found {
		return nil, false
	}
	return &r.Items[match], true
}
