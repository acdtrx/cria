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

// Backend names one way of serving a model — the id its engine file, its state
// records and, where an entry may declare it, its backend key carry. The values
// below are the whole set cria knows how to launch (docs/specs/TOOLS.md).
type Backend string

const (
	BackendLlama  Backend = "llama"
	BackendMLX    Backend = "mlx"
	BackendRouter Backend = "router"
)

// engineFacts is one engine as the config tree knows it: the id its files carry,
// the flag cria composes the models it serves under, and whether an entry file
// may name it at all.
type engineFacts struct {
	id Backend

	// modelFlag is the flag cria composes this engine's models under: the Hub
	// reference of the one model an entry's server serves, or the preset file a
	// router serves many from. An args list may not restate it, since restating
	// it would fight the composed command line rather than change it
	// (docs/specs/CONFIG.md).
	//
	// The engine passes its models under that flag (internal/engine), which is
	// the one thing this package cannot read from there.
	modelFlag string

	// perEntry reports whether a models/<id>.toml may declare this engine: an
	// engine that runs one server per entry is named by the entry it serves,
	// while one whose single server serves every model included in it is
	// configured and started as itself and never named by an entry
	// (docs/specs/CONFIG.md).
	perEntry bool
}

// engines is every engine cria has, in the order `cria docs` presents them. It
// is the set engine files are read for, the set the per-engine schema metadata
// is read against, and — narrowed to the per-entry ones — the set an entry's
// backend key may name.
//
// The engines that implement these ids live in internal/engine, and that package
// imports this one: the set cannot be read from there without a cycle, which is
// why it is declared here rather than derived. internal/engine holds the tests
// that check the two against each other, so an engine that exists on one side
// only — or a model flag that is not the one its engine composes — is a red
// suite rather than a schema that documents something nothing serves.
var engines = []engineFacts{
	{id: BackendLlama, modelFlag: "-hf", perEntry: true},
	{id: BackendMLX, modelFlag: "--model", perEntry: true},
	{id: BackendRouter, modelFlag: "--models-preset"},
}

// Engines lists every engine the tree may configure: the ids engines/<id>.toml
// is read for, and the ids `cria docs` renders an engine example for.
func Engines() []Backend {
	ids := make([]Backend, 0, len(engines))
	for _, engine := range engines {
		ids = append(ids, engine.id)
	}
	return ids
}

// Backends lists the engines an entry may declare — the set the parser accepts
// under the backend key, the set entry examples are rendered for, and the
// refusal a file naming something else gets.
func Backends() []Backend {
	ids := make([]Backend, 0, len(engines))
	for _, engine := range engines {
		if engine.perEntry {
			ids = append(ids, engine.id)
		}
	}
	return ids
}

// ModelFlag is the flag cria composes this engine's models under. An engine the
// tree does not know has no flag.
func ModelFlag(backend Backend) string {
	for _, engine := range engines {
		if engine.id == backend {
			return engine.modelFlag
		}
	}
	return ""
}

// Entry is one launchable thing: a models/<id>.toml file whose keys have been
// validated and whose port and host are already resolved against the tree
// settings. Everything the lifecycle needs to compose a command line is here.
type Entry struct {
	ID         string   // the filename minus .toml; the id in the TUI and on the CLI
	Path       string   // the file this entry was read from
	Backend    Backend  // which server program serves it
	Repo       string   // Hugging Face repo id, org/name
	Quant      string   // llama only; empty means the server picks the repo's default
	Port       int      // resolved: the entry's own port, else default_port
	Host       string   // resolved: the entry's own host, else default_host, else 0.0.0.0
	Name       string   // display name; the id when the file sets none
	Args       []string // the entry's own args tokens, in file order
	EngineArgs []string // resolved: engines/<backend>.toml's args, the level this entry's own args override
	Choices    []Choice // the axes this entry varies on, in file order; none for a flat entry
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
	Args  []string // merged over the entry's args when this option is picked
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
	VLLM        string
	HF          string
}

// RouterConfig is engines/router.toml: what this machine's one router process
// runs as. The router is not an entry — no models/<id>.toml declares it — so its
// port, its bind address and its flags live in its engine file and nowhere else
// (docs/specs/CONFIG.md).
type RouterConfig struct {
	Path       string   // the file these came from, named by every refusal — set whether or not the file exists
	Port       int      // 0 when the file sets none: the router has no port to serve on and cannot start
	Host       string   // resolved: the file's own host, else default_host, else 0.0.0.0
	Args       []string // what every model the router serves starts from: the composed preset's [*] block
	RouterArgs []string // the router process's own flags, passed verbatim after the ones cria composes
}

// Tree is a loaded config tree: the entries cria can act on, the engine-level
// configuration around them, plus the entry files it had to disable and why. A
// broken entry disables only itself (docs/specs/CONFIG.md), so both lists are
// part of a successful load.
type Tree struct {
	Root     string
	Settings Settings
	Entries  []Entry       // valid entries, ordered by id
	Broken   []BrokenEntry // entry files that failed to load, ordered by id
	Router   RouterConfig  // engines/router.toml, resolved; zero-valued when the file is absent
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
