package hubcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures build a real hub cache on disk — blobs holding real bytes,
// snapshots of symlinks pointing at them, refs naming the current revision. The
// walker's whole job is reading that layout, so nothing here is mocked: a test
// that passed against a stand-in tree would prove nothing about the cache on the
// machine.

// cacheTree is a hub cache under construction in a temp directory.
type cacheTree struct {
	t    *testing.T
	Root string
}

// repoTree is one repository directory inside a cacheTree.
type repoTree struct {
	t   *testing.T
	dir string
}

// cachedFile is one file a snapshot holds: filler bytes of the given size, or the
// exact text when the test cares what is inside (config.json). Two snapshots
// naming the same file get the same blob, the way the cache deduplicates.
type cachedFile struct {
	name string
	size int
	text string
}

// newCacheTree starts an empty hub cache.
func newCacheTree(t *testing.T) *cacheTree {
	t.Helper()
	return &cacheTree{t: t, Root: t.TempDir()}
}

// repo adds a repository directory, named the way huggingface_hub names them
// (models--org--name).
func (c *cacheTree) repo(dir string) *repoTree {
	c.t.Helper()
	repo := &repoTree{t: c.t, dir: filepath.Join(c.Root, dir)}
	mkdir(c.t, repo.dir)
	return repo
}

// write drops a plain file into the cache root, for the entries that are not
// repositories (CACHEDIR.TAG and the like).
func (c *cacheTree) write(name, content string) *cacheTree {
	c.t.Helper()
	path := filepath.Join(c.Root, name)
	mkdir(c.t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		c.t.Fatalf("cannot write %s: %v", path, err)
	}
	return c
}

// snapshot adds one revision holding the given files, each backed by a blob.
func (r *repoTree) snapshot(revision string, files ...cachedFile) *repoTree {
	r.t.Helper()
	dir := filepath.Join(r.dir, "snapshots", revision)
	mkdir(r.t, dir)
	for _, file := range files {
		content := file.content()
		blob := filepath.Join(r.dir, "blobs", blobName(content))
		mkdir(r.t, filepath.Dir(blob))
		if _, err := os.Stat(blob); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(blob, content, 0o644); err != nil {
				r.t.Fatalf("cannot write %s: %v", blob, err)
			}
		}
		r.link(dir, file.name, blob)
	}
	return r
}

// main writes the ref that names the current revision.
func (r *repoTree) main(revision string) *repoTree {
	r.t.Helper()
	dir := filepath.Join(r.dir, "refs")
	mkdir(r.t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main"), []byte(revision), 0o644); err != nil {
		r.t.Fatalf("cannot write the main ref of %s: %v", r.dir, err)
	}
	return r
}

// partial adds an unfinished download: a blob under a name no snapshot points at.
func (r *repoTree) partial(name string, size int) *repoTree {
	r.t.Helper()
	dir := filepath.Join(r.dir, "blobs")
	mkdir(r.t, dir)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, fill(name, size), 0o644); err != nil {
		r.t.Fatalf("cannot write %s: %v", path, err)
	}
	return r
}

// dangling adds a snapshot entry whose blob was already removed — what a cache
// looks like after a blob is deleted out from under a snapshot.
func (r *repoTree) dangling(revision, name string) *repoTree {
	r.t.Helper()
	dir := filepath.Join(r.dir, "snapshots", revision)
	mkdir(r.t, dir)
	r.link(dir, name, filepath.Join(r.dir, "blobs", "gone"))
	return r
}

// link points a snapshot entry at a blob the way the cache does, by a relative
// symlink.
func (r *repoTree) link(dir, name, blob string) {
	r.t.Helper()
	path := filepath.Join(dir, name)
	mkdir(r.t, filepath.Dir(path))
	target, err := filepath.Rel(filepath.Dir(path), blob)
	if err != nil {
		r.t.Fatalf("cannot point %s at %s: %v", path, blob, err)
	}
	if err := os.Symlink(target, path); err != nil {
		r.t.Fatalf("cannot link %s: %v", path, err)
	}
}

// content is the bytes this file holds.
func (f cachedFile) content() []byte {
	if f.text != "" {
		return []byte(f.text)
	}
	return fill(f.name, f.size)
}

// fill produces size bytes that depend on name, so two different files never
// collide on one blob and the same file always lands on the same one.
func fill(name string, size int) []byte {
	if size == 0 {
		return nil
	}
	seed := []byte(name + "|")
	return bytes.Repeat(seed, size/len(seed)+1)[:size]
}

// blobName names a blob after its content, the way the cache names blobs after
// the hashes the Hub publishes.
func blobName(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", dir, err)
	}
}

// read walks a fixture and fails the test if the walk itself errors.
func read(t *testing.T, root string) *Cache {
	t.Helper()
	cache, err := Read(root)
	if err != nil {
		t.Fatalf("reading the cache at %s: %v", root, err)
	}
	return cache
}

// repoOf finds one repo in a walk result, failing the test when it is missing.
func repoOf(t *testing.T, cache *Cache, id string) *Repo {
	t.Helper()
	repo, ok := cache.Repo(id)
	if !ok {
		t.Fatalf("the walk found no repo %q; it found %v", id, repoIDs(cache))
	}
	return repo
}

// repoIDs lists what a walk found, for failure messages.
func repoIDs(cache *Cache) []string {
	ids := make([]string, len(cache.Repos))
	for i, repo := range cache.Repos {
		ids[i] = repo.ID
	}
	return ids
}

// itemLabels lists a repo's quants, for failure messages and for the assertions
// that only care about which quants were found.
func itemLabels(repo *Repo) []string {
	labels := make([]string, len(repo.Items))
	for i, item := range repo.Items {
		labels[i] = item.Label
	}
	return labels
}

// diskUsage sums what a tree occupies the way du does: every regular file once,
// symlinks contributing nothing of their own because the blob they point at is
// already counted where it lives.
func diskUsage(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("cannot measure %s: %v", root, err)
	}
	return total
}
