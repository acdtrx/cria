// Package config reads the config tree at ~/.config/cria: tree-wide settings in
// config.toml and one launchable entry per models/<id>.toml file
// (docs/specs/CONFIG.md). The tree is cria's interface — humans and coding agents
// write it, cria reads and drives it.
//
// Every key cria understands is declared once, in schema.go. Those definitions
// drive decoding, validation and the `cria docs` output alike, so a new key or a
// changed rule is a single edit and the documentation cannot drift from the
// parser.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Backend names the server program an entry runs. The two values are the whole
// set cria knows how to launch (docs/specs/TOOLS.md).
type Backend string

const (
	BackendLlama Backend = "llama"
	BackendMLX   Backend = "mlx"
)

// Entry is one launchable thing: a models/<id>.toml file whose keys have been
// validated and whose port and host are already resolved against the tree
// settings. Everything the lifecycle needs to compose a command line is here.
type Entry struct {
	ID      string   // the filename minus .toml; the id in the TUI and on the CLI
	Path    string   // the file this entry was read from
	Backend Backend  // which server program serves it
	Repo    string   // Hugging Face repo id, org/name
	Quant   string   // llama only; empty means the server picks the repo's default
	Port    int      // resolved: the entry's own port, else default_port
	Host    string   // resolved: the entry's own host, else default_host, else 0.0.0.0
	Name    string   // display name; the id when the file sets none
	Args    []string // extra flags handed to the server verbatim
	Choices []Choice // the axes this entry varies on, in file order; none for a flat entry
}

// Choice is one axis an entry varies on: a named set of options, exactly one of
// which is picked for a launch. cria folds the picked option into the command
// line and never interprets what its flags mean (docs/specs/CONFIG.md).
type Choice struct {
	Name    string
	Options []ChoiceOption // at least one, in file order; the first is the config default
}

// ChoiceOption is one pick of a choice: what it replaces and what it adds when it
// is the picked one. Which combinations of options make sense is the author's
// knowledge, not cria's — flags that must vary together live in one choice.
type ChoiceOption struct {
	Name  string
	Quant string   // llama only; replaces the entry's quant when set
	Repo  string   // replaces the entry's repo when set
	Args  []string // appended to the entry's args
}

// Settings is config.toml: the defaults entries fall back to and the tool paths
// that override PATH lookup. The file is optional, so the zero value is a valid
// tree-wide configuration.
type Settings struct {
	DefaultPort int // 0 when config.toml sets none
	DefaultHost string
	Tools       Tools
}

// Tools holds absolute paths that override PATH lookup for the managed tools
// (docs/specs/TOOLS.md). An empty field means "look it up on PATH".
type Tools struct {
	LlamaServer string
	MLXLMServer string
	HF          string
}

// Tree is a loaded config tree: the entries cria can act on, plus the entry
// files it had to disable and why. A broken entry disables only itself
// (docs/specs/CONFIG.md), so both lists are part of a successful load.
type Tree struct {
	Root     string
	Settings Settings
	Entries  []Entry       // valid entries, ordered by id
	Broken   []BrokenEntry // entry files that failed to load, ordered by id
}

// BrokenEntry is an entry file cria refused. It is reported rather than
// swallowed: the TUI and the CLI name the file and the offending key so the
// author can fix it.
type BrokenEntry struct {
	ID   string
	Path string
	Err  error // *KeyError for a schema violation, a parse or read error otherwise
}

// KeyError names the config key that failed and why. Every schema violation
// takes this shape, so a report can always pair the offending key with the file
// it came from (docs/specs/CONFIG.md).
type KeyError struct {
	Key    string
	Reason string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("key %q: %s", e.Key, e.Reason)
}

// Entry finds the entry an id names among the ones that loaded — the lookup
// every caller acting on a named entry makes, wherever the name came from: a
// command line, a keypress, or a state record being started again
// (serve.Replay).
//
// A tree nobody has read yet declares nothing. That is the honest answer for a
// caller holding one — a frame drawn before the first load — rather than a
// panic, and it is the same answer as an id the tree does not have: there is no
// such entry to act on.
func (t *Tree) Entry(id string) (Entry, bool) {
	if t == nil {
		return Entry{}, false
	}
	for _, entry := range t.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

// Root is the config tree's one location — the same path on macOS and Linux
// (docs/TECH-STACK.md). Load takes its root as an argument so tests can point it
// elsewhere; production callers pass this.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory that holds ~/.config/cria: %w", err)
	}
	return filepath.Join(home, ".config", "cria"), nil
}
