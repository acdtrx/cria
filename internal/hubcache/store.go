package hubcache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// What a delete does to huggingface_hub's shared blob store is decided by hf's
// own rule, read here the way huggingface_hub/utils/_shared_blobs.py reads it:
// a stored payload is freed only when every valid link its .refs manifest lists
// is one this delete removes, and a manifest that is missing or unreadable frees
// nothing. cria never writes into the store; this only says what a delete
// leaves there (docs/specs/CACHE.md, Deletion).

// storeLayout is the marker content of the one store layout huggingface_hub
// writes and reads; a store marked any other way is not one hf will free from.
const storeLayout = "1\n"

// xetHash is how the store names a payload.
var xetHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// hfRepoDirs are the repository directory prefixes huggingface_hub writes
// (REPO_TYPES_MAPPING), each with the type it puts before the id in an hf://
// URI. The walk reads fewer of them; hf's rule counts a link from any of them.
var hfRepoDirs = map[string]string{
	"models":   "",
	"datasets": "datasets/",
	"spaces":   "spaces/",
	"kernels":  "kernels/",
}

// storeFate is what a set of removals leaves in the cache's shared store: the
// bytes of every payload whose links all go with them, which no repository
// reaches any more, and the payloads something still keeps — another link the
// manifest lists, or a manifest that cannot say. payloads is every payload the
// removals link to, the one and the other.
func storeFate(root string, removed map[string]bool) (unreferenced int64, kept []Shared, payloads []string) {
	if !hfStore(root) {
		return 0, nil, nil
	}

	linked := map[string]bool{}
	for path := range removed {
		// A removal hf cannot read as one of its links is not one: it leads
		// nowhere in the store, so it leaves nothing there.
		if payload, managed, _ := storeTarget(root, path); managed {
			linked[payload] = true
		}
	}
	for payload := range linked {
		payloads = append(payloads, payload)
	}
	sort.Strings(payloads)

	for _, payload := range payloads {
		info, err := os.Lstat(payload)
		if err != nil || !info.Mode().IsRegular() {
			// A link to nothing holds nothing.
			continue
		}
		links, readable := manifestLinks(root, payload)
		var keepers []string
		for _, link := range links {
			if !removed[link] {
				keepers = append(keepers, link)
			}
		}
		if readable && len(keepers) == 0 {
			unreferenced += info.Size()
			continue
		}
		kept = append(kept, Shared{
			Blob:   payload,
			Bytes:  info.Size(),
			Links:  keepers,
			Stored: true,
			Repos:  linkRepos(root, keepers),
		})
	}
	return unreferenced, kept, payloads
}

// hfStore reports whether the cache holds a shared store hf would free from:
// a real directory carrying the marker of the layout hf writes.
func hfStore(root string) bool {
	dir := filepath.Join(root, "blobs")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	marker := filepath.Join(dir, storeMarker)
	if info, err := os.Lstat(marker); err != nil || !info.Mode().IsRegular() {
		return false
	}
	content, err := os.ReadFile(marker)
	return err == nil && universalNewlines(string(content)) == storeLayout
}

// universalNewlines reads text the way Python's read_text does: \r\n and a
// lone \r both become \n.
func universalNewlines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// hfLines splits text into lines the way Python's str.splitlines does, which is
// how hf reads a manifest: at every line boundary Unicode knows, not only \n.
// Empty lines go — hf finds no link in one either.
func hfLines(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '\n', '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
			return true
		}
		return false
	})
}

// storeTarget is the payload a repository's blob links to, when it is a link
// hf manages: <repo>/blobs/<name>, a symlink to blobs/<xx>/<hash> at the cache
// root. The comparison is lexical, as hf's is. err is any failure to read the
// link other than its absence, which a manifest read treats as unreadable.
func storeTarget(root, link string) (payload string, managed bool, err error) {
	if !repoBlobPath(root, link) {
		return "", false, nil
	}
	info, err := os.Lstat(link)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return "", false, nil
	}
	target, err := os.Readlink(link)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}

	store := filepath.Join(root, "blobs")
	rel, err := filepath.Rel(store, filepath.Clean(target))
	if err != nil {
		// No relative path from the store to the target: it lies outside.
		return "", false, nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || !xetHash.MatchString(parts[1]) || parts[0] != parts[1][:2] {
		return "", false, nil
	}
	return filepath.Join(store, parts[0], parts[1]), true, nil
}

// repoBlobPath reports whether path is shaped like a repository's blob:
// <root>/<repo directory>/blobs/<name>, the only kind of path a manifest may
// name.
func repoBlobPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.ContainsAny(rel, "\n\r") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[1] != "blobs" || parts[2] == "" {
		return false
	}
	_, isRepo := hfRepoDir(parts[0])
	return isRepo
}

// hfRepoDir reads a repository directory name the way hf does, naming the
// repository as an hf:// URI does: the id, after its type where that is not a
// model.
func hfRepoDir(name string) (string, bool) {
	prefix, rest, ok := strings.Cut(name, "--")
	kind, known := hfRepoDirs[prefix]
	if !ok || !known || rest == "" {
		return "", false
	}
	return kind + strings.ReplaceAll(rest, "--", "/"), true
}

// manifestLinks lists the links a payload's .refs manifest names that really
// lead to it. The manifest is a hint: a line whose link is gone, or leads
// elsewhere, keeps nothing. readable is false where the manifest is missing or
// cannot be read, which hf answers by never freeing the payload.
func manifestLinks(root, payload string) (links []string, readable bool) {
	manifest := payload + ".refs"
	info, err := os.Lstat(manifest)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	content, err := os.ReadFile(manifest)
	if err != nil || !utf8.Valid(content) {
		return nil, false
	}

	seen := map[string]bool{}
	for _, line := range hfLines(string(content)) {
		if filepath.IsAbs(line) || slices.Contains(strings.Split(line, "/"), "..") {
			continue
		}
		candidate := filepath.Join(root, filepath.FromSlash(line))
		target, managed, err := storeTarget(root, candidate)
		if err != nil {
			return nil, false
		}
		if managed && target == payload && !seen[candidate] {
			seen[candidate] = true
			links = append(links, candidate)
		}
	}
	sort.Strings(links)
	return links, true
}

// linkRepos names the repositories a set of repository blobs belong to, as
// hfRepoDir names them, each once, in the links' order.
func linkRepos(root string, links []string) []string {
	var repos []string
	for _, link := range links {
		rel, err := filepath.Rel(root, link)
		if err != nil {
			// Not reached: every link here passed repoBlobPath, which took the
			// same relative path.
			continue
		}
		dir, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		if id, ok := hfRepoDir(dir); ok && !slices.Contains(repos, id) {
			repos = append(repos, id)
		}
	}
	return repos
}
