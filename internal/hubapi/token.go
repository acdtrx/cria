package hubapi

import (
	"os"
	"path/filepath"
	"strings"
)

// Token resolves the Hugging Face credential this host holds: HF_TOKEN when the
// environment sets one, else the token file `hf auth login` writes. No token is
// a normal state, not an error — public repos need none, and cria never
// installs or runs `hf` to make one appear (docs/specs/TOOLS.md).
//
// The result is a secret with exactly two destinations: the Authorization
// header this package sends, and the environment of the servers cria launches
// (docs/specs/SERVE.md). It is never printed, never put in a URL and never
// passed on a command line (CODING-RULES §9).
func Token() string {
	if token := strings.TrimSpace(os.Getenv("HF_TOKEN")); token != "" {
		return token
	}
	path := tokenPath()
	if path == "" {
		return ""
	}
	// An unreadable or absent token file means no token. There is nothing to
	// report: a host that never logged in is the common case, and gated repos
	// fail loudly on their own when the server tries to fetch them.
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// tokenPath locates the file `hf auth login` writes, resolved the way
// huggingface_hub resolves it — HF_HOME when it names the Hugging Face home,
// the XDG cache location otherwise. It is the same resolution hubcache.Root
// applies to the cache directory, because it is the same tree: the token sits
// next to the hub/ directory the models land in.
func tokenPath() string {
	if home := os.Getenv("HF_HOME"); home != "" {
		return filepath.Join(home, "token")
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "huggingface", "token")
}
