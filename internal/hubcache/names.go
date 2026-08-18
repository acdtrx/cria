package hubcache

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// repoDirTypes maps the prefix a cache directory carries to the Hub namespace it
// names. huggingface_hub writes exactly these three; a directory shaped any other
// way is not a repository, which is how .locks and the cache's own bookkeeping
// stay out of the walk.
var repoDirTypes = map[string]RepoType{
	"models":   RepoModel,
	"datasets": RepoDataset,
	"spaces":   RepoSpace,
}

// shardSuffix matches the -NNNNN-of-NNNNN a file carries when a model is split
// across several files. GGUF splits and safetensors shards spell it the same way,
// so one pattern covers both backends.
var shardSuffix = regexp.MustCompile(`-(\d{5})-of-(\d{5})$`)

// quantToken matches the quantization names GGUF file names carry: ggml's own
// type names as repos spell them, in any case — llama-server's `-hf repo:TAG`
// matching ignores case too, and older repos write theirs in lower case.
var quantToken = regexp.MustCompile(`(?i)^(?:[IT]?Q\d[0-9A-Z_]*|B?F(?:16|32)|MXFP4[0-9A-Z_]*)$`)

// udPrefix is the token unsloth puts in front of its dynamic quantizations. It
// is part of the tag the repo publishes — llama.cpp's `-hf repo:TAG` resolves
// the full UD-Q4_K_XL spelling and unsloth's documentation tells people to ask
// for it — so the label keeps it, like every other character of a name a
// provider chose (docs/specs/CACHE.md).
const udPrefix = "UD"

// parseRepoDir reads a cache directory name — models--org--name — into the Hub
// namespace and the repo id it stands for. The separator is the same "--" the Hub
// folder convention substitutes for "/", so a repo id that itself contains "--"
// is ambiguous; that is the convention's limit, not a choice cria makes.
func parseRepoDir(name string) (RepoType, string, bool) {
	prefix, rest, ok := strings.Cut(name, "--")
	if !ok || rest == "" {
		return "", "", false
	}
	repoType, known := repoDirTypes[prefix]
	if !known {
		return "", "", false
	}
	return repoType, strings.ReplaceAll(rest, "--", "/"), true
}

// splitShard separates a file name from the shard it declares. The suffix sits
// before the extension, so the base keeps the extension and two shards of one
// model share it exactly.
func splitShard(name string) (base string, index, count int, sharded bool) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	match := shardSuffix.FindStringSubmatch(stem)
	if match == nil {
		return name, 0, 0, false
	}
	index, _ = strconv.Atoi(match[1])
	count, _ = strconv.Atoi(match[2])
	return strings.TrimSuffix(stem, match[0]) + ext, index, count, true
}

// quantLabel names the quantization a GGUF file holds, read off its name: the
// last token that spells a ggml type, carrying unsloth's UD- prefix with it
// when the name has one. Repos put the tag last by convention
// (Qwen3-30B-A3B-UD-Q4_K_XL.gguf), so taking the last match keeps a model name
// that happens to contain something tag-shaped from winning over the real tag.
//
// That one prefix is the only token absorbed, and only directly before the
// type: every other token ahead of it is model name (gemma-4-26B-it-qat-Q4_K_XL
// is a Q4_K_XL, not a qat-Q4_K_XL).
func quantLabel(name string) (string, bool) {
	base, _, _, _ := splitShard(name)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	tokens := strings.Split(stem, "-")
	for i := len(tokens) - 1; i >= 0; i-- {
		if !quantToken.MatchString(tokens[i]) {
			continue
		}
		if i > 0 && strings.EqualFold(tokens[i-1], udPrefix) {
			return tokens[i-1] + "-" + tokens[i], true
		}
		return tokens[i], true
	}
	return "", false
}

// MatchQuant picks which of a repo's item labels the quant a config entry names
// refers to, and reports its position. The labels are the cache's items or the
// ones the Hub's file listing spells.
//
// The match is exact, on the tag as the repo's files spell it: cria does not
// normalize provider naming, so an entry naming Q2_K_XL where the repo publishes
// UD-Q2_K_XL finds nothing, and says so (docs/specs/CACHE.md). Case is the one
// difference tolerated, because llama.cpp's own `-hf repo:TAG` resolution is
// case-insensitive.
func MatchQuant(labels []string, quant string) (int, bool) {
	for i, label := range labels {
		if strings.EqualFold(label, quant) {
			return i, true
		}
	}
	return 0, false
}

// itemLabel is the name a GGUF file's item carries: its quantization when the
// name spells one, the file's own name otherwise — a file cria cannot read a
// tag off is still a thing on disk worth showing and deleting, and it shows
// under the name the Hub gives it (docs/specs/CACHE.md). A label is an item's
// identity, not a file name; the files keep their own names in Item.Files.
// Shards fold into one item either way, and a fallback series is named after
// the file its shards are parts of.
//
// A projector is the exception that has to come first: it carries a precision
// token that reads exactly like a quantization (mmproj-BF16.gguf) but pairs with
// any quant of its repo, so grouping it under that token would fold two unrelated
// things into one row — and one delete.
func itemLabel(name string) string {
	base, _, _, _ := splitShard(name)
	if isProjector(base) {
		return base
	}
	if label, ok := quantLabel(name); ok {
		return label
	}
	return base
}

// GGUFItem names the item a file belongs to and reports whether it is a GGUF
// file at all — the whole rule for which files make up one quantization, in one
// place. internal/hubapi asks it the same question of the Hub's file listing
// that the walk asks of the cache, so the bytes on disk and the bytes still to
// come are counted over the same set of files (docs/specs/SERVE.md).
func GGUFItem(name string) (string, bool) {
	if !hasExt(name, ".gguf") {
		return "", false
	}
	return itemLabel(name), true
}

// isProjector reports whether a file is a multimodal projector, which both
// llama.cpp and the repos publishing them mark by naming it mmproj-*.
func isProjector(name string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(name)), "mmproj")
}

// seriesComplete reports whether every shard the given file names declare is
// present. A shard names its series' size (-00002-of-00005), so an interrupted
// multi-file download is visible from the file names alone, with nothing asked of
// the Hub. Names that declare no shards impose no requirement.
func seriesComplete(names []string) bool {
	type series struct {
		count int
		seen  map[int]bool
	}
	found := map[string]*series{}
	for _, name := range names {
		base, index, count, sharded := splitShard(name)
		if !sharded {
			continue
		}
		current := found[base]
		if current == nil {
			current = &series{seen: map[int]bool{}}
			found[base] = current
		}
		if count > current.count {
			current.count = count
		}
		current.seen[index] = true
	}
	for _, current := range found {
		for i := 1; i <= current.count; i++ {
			if !current.seen[i] {
				return false
			}
		}
	}
	return true
}

// hasExt reports whether name carries the given extension, ignoring case: the
// cache stores whatever the repo published, and .GGUF is a legal spelling.
func hasExt(name, ext string) bool {
	return strings.EqualFold(filepath.Ext(name), ext)
}
