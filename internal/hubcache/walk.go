package hubcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// partialSuffixes are the names an unfinished download goes by. huggingface_hub
// writes ".incomplete"; llama-server, which fetches every GGUF cria serves,
// writes ".downloadInProgress". Both land in blobs/ with no snapshot entry
// pointing at them, which is also why a file reachable from a snapshot is
// finished by construction (docs/specs/CACHE.md).
var partialSuffixes = []string{".incomplete", ".downloadInProgress"}

// maxConfigBytes caps the config.json read. The file is a few kilobytes of model
// metadata and the walk asks it exactly one question, so it must not pull an
// arbitrarily large file into memory to answer it.
const maxConfigBytes = 1 << 20

// Read walks the hub cache at root. A root that does not exist is an empty cache
// rather than an error: nothing has been downloaded yet, which is a normal state
// for a fresh host.
func Read(root string) (*Cache, error) {
	cache := &Cache{Root: root}

	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the Hugging Face hub cache at %s: %w", root, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoType, id, ok := parseRepoDir(entry.Name())
		if !ok {
			continue
		}
		repo, err := readRepo(filepath.Join(root, entry.Name()), repoType, id)
		if err != nil {
			return nil, err
		}
		cache.Repos = append(cache.Repos, repo)
		cache.Bytes += repo.Bytes
		cache.PartialBytes += repo.PartialBytes
	}

	sort.Slice(cache.Repos, func(i, j int) bool { return cache.Repos[i].ID < cache.Repos[j].ID })
	return cache, nil
}

// readRepo reads one repository directory: what it occupies, what its snapshots
// name, and what that makes it.
func readRepo(dir string, repoType RepoType, id string) (Repo, error) {
	repo := Repo{ID: id, Type: repoType, Dir: dir}

	bytes, err := diskBytes(dir)
	if err != nil {
		return Repo{}, err
	}
	repo.Bytes = bytes

	partials, err := readPartials(filepath.Join(dir, "blobs"))
	if err != nil {
		return Repo{}, err
	}
	repo.Partials = partials
	for _, partial := range partials {
		repo.PartialBytes += partial.Bytes
		if partial.Modified.After(repo.Modified) {
			repo.Modified = partial.Modified
		}
	}

	repo.Revision = readRef(filepath.Join(dir, "refs", "main"))

	repo.Files, err = readFiles(filepath.Join(dir, "snapshots"), repo.Revision)
	if err != nil {
		return Repo{}, err
	}
	for _, file := range repo.Files {
		if file.Modified.After(repo.Modified) {
			repo.Modified = file.Modified
		}
	}

	repo.Kind = repoKind(repo.Files)
	if repo.Kind == KindGGUF {
		repo.Items = ggufItems(repo.Files)
	}
	repo.Complete = len(repo.Files) > 0 && len(repo.Partials) == 0 && seriesComplete(names(repo.Files))
	return repo, nil
}

// diskBytes is what a repository directory occupies: every regular file under it.
// Symlinks hold no bytes of their own and the blob behind them is counted where
// it lives, so a blob several snapshots share is counted once — the true number,
// the one du reports (docs/specs/CACHE.md).
func diskBytes(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
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
		return 0, fmt.Errorf("cannot measure the cached repository at %s: %w", dir, err)
	}
	return total, nil
}

// readPartials lists the unfinished downloads in a repo's blobs directory. A repo
// with no blobs directory yet has none.
func readPartials(dir string) ([]Partial, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the cached blobs at %s: %w", dir, err)
	}

	var partials []Partial
	for _, entry := range entries {
		if entry.IsDir() || !isPartial(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("cannot measure %s: %w", filepath.Join(dir, entry.Name()), err)
		}
		partials = append(partials, Partial{
			Path:     filepath.Join(dir, entry.Name()),
			Bytes:    info.Size(),
			Modified: info.ModTime(),
		})
	}
	sort.Slice(partials, func(i, j int) bool { return partials[i].Path < partials[j].Path })
	return partials, nil
}

// isPartial reports whether a blob name marks a download still in flight.
func isPartial(name string) bool {
	for _, suffix := range partialSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// readRef reads the revision a ref names. A missing or unreadable ref is no
// revision at all — the walk reads the snapshots either way, so nothing is lost
// beyond knowing which of them is current.
func readRef(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// readFiles collects the distinct blobs a repo's snapshots reach. Every revision
// is read, not just the current one: a quant that only an older snapshot names is
// still on disk, and the cache view exists to show exactly that.
func readFiles(snapshots, current string) ([]File, error) {
	revisions, err := snapshotRevisions(snapshots, current)
	if err != nil {
		return nil, err
	}

	byBlob := map[string]*File{}
	var order []string
	for _, revision := range revisions {
		dir := filepath.Join(snapshots, revision)
		entries, err := readSnapshot(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			file := byBlob[entry.blob]
			if file == nil {
				file = &File{Name: entry.name, Blob: entry.blob, Bytes: entry.bytes, Modified: entry.modified}
				byBlob[entry.blob] = file
				order = append(order, entry.blob)
			}
			file.Links = append(file.Links, entry.link)
		}
	}

	files := make([]File, 0, len(order))
	for _, blob := range order {
		files = append(files, *byBlob[blob])
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// snapshotRevisions lists the snapshots a repo holds, the one refs/main names
// first: a blob two revisions know by different names is reported under the
// current one's, which is the name the repo goes by today.
func snapshotRevisions(dir, current string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the cached snapshots at %s: %w", dir, err)
	}

	var revisions []string
	for _, entry := range entries {
		if entry.IsDir() {
			revisions = append(revisions, entry.Name())
		}
	}
	sort.Strings(revisions)
	for i, revision := range revisions {
		if revision == current {
			revisions[0], revisions[i] = revisions[i], revisions[0]
			break
		}
	}
	return revisions, nil
}

// snapshotEntry is one name a snapshot gives to some bytes, already resolved to
// the file that holds them.
type snapshotEntry struct {
	name     string // the path inside the snapshot
	link     string // the snapshot entry itself
	blob     string // the file the bytes live in
	bytes    int64
	modified time.Time
}

// readSnapshot lists one revision's entries. A snapshot can nest directories, so
// the whole subtree is read and each entry keeps the path the repo gives it.
func readSnapshot(dir string) ([]snapshotEntry, error) {
	var entries []snapshotEntry
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		blob, info, ok := resolve(path, entry)
		if !ok {
			return nil
		}
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		entries = append(entries, snapshotEntry{
			name:     filepath.ToSlash(name),
			link:     path,
			blob:     blob,
			bytes:    info.Size(),
			modified: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot read the cached snapshot at %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

// resolve finds the file behind a snapshot entry: the blob a symlink points at,
// or the entry itself where the cache holds a copy instead of a link. A link
// whose blob is gone resolves to nothing — those bytes are not on disk, so
// nothing counts them and the quant reads as absent, which is the truth.
func resolve(path string, entry fs.DirEntry) (string, os.FileInfo, bool) {
	if entry.Type()&fs.ModeSymlink == 0 {
		info, err := entry.Info()
		if err != nil {
			return "", nil, false
		}
		return path, info, true
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", nil, false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	target = filepath.Clean(target)
	info, err := os.Stat(target)
	if err != nil {
		return "", nil, false
	}
	return target, info, true
}

// repoKind judges what cria can do with a repo from what its snapshots hold: a
// .gguf anywhere makes it a GGUF repo, safetensors with mlx-lm's quantization
// block make it an MLX one, anything else is just disk.
//
// The GGUF test spans every snapshot rather than only the current one, because
// the items do: a quant an older revision still names is on disk and listed, so
// tagging the repo from the current revision alone could contradict its own rows.
func repoKind(files []File) Kind {
	safetensors := false
	config := ""
	for _, file := range files {
		switch {
		case hasExt(file.Name, ".gguf"):
			return KindGGUF
		case hasExt(file.Name, ".safetensors"):
			safetensors = true
		case file.Name == "config.json":
			config = file.Blob
		}
	}
	if safetensors && config != "" && mlxQuantized(config) {
		return KindMLX
	}
	return KindOther
}

// mlxQuantized reports whether a repo's config.json carries mlx-lm's
// quantization block — the marker that tells an MLX model apart from the plain
// safetensors repo it was quantized from. It is a block, not a flag: a repo
// spelling the key with a null or a string is not claiming what mlx-lm writes.
func mlxQuantized(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	var config struct {
		Quantization map[string]any `json:"quantization"`
	}
	if err := json.NewDecoder(io.LimitReader(file, maxConfigBytes)).Decode(&config); err != nil {
		return false
	}
	return config.Quantization != nil
}

// ggufItems groups a repo's GGUF files into the units the cache view selects and
// deletes: one per quantization, shards folded in (docs/specs/CACHE.md).
func ggufItems(files []File) []Item {
	byLabel := map[string]*Item{}
	var order []string
	for _, file := range files {
		label, isGGUF := GGUFItem(file.Name)
		if !isGGUF {
			continue
		}
		item := byLabel[label]
		if item == nil {
			item = &Item{Label: label}
			byLabel[label] = item
			order = append(order, label)
		}
		item.Files = append(item.Files, file)
		item.Bytes += file.Bytes
		if file.Modified.After(item.Modified) {
			item.Modified = file.Modified
		}
	}

	items := make([]Item, 0, len(order))
	for _, label := range order {
		item := *byLabel[label]
		item.Complete = seriesComplete(names(item.Files))
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items
}

// names lists the file names of a set of files, for the checks that read meaning
// out of naming.
func names(files []File) []string {
	list := make([]string, len(files))
	for i, file := range files {
		list[i] = file.Name
	}
	return list
}
