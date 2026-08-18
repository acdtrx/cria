package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	// entriesDir holds one file per launchable entry.
	entriesDir = "models"
	// settingsFile carries the tree-wide defaults; a tree works without it.
	settingsFile = "config.toml"
	// tomlExt is the extension that makes a file in entriesDir an entry; the id is
	// the filename without it.
	tomlExt = ".toml"
	// defaultBindHost is the bind address an entry gets when neither it nor
	// config.toml names one: servers are reachable from the LAN out of the box
	// (docs/specs/CONFIG.md).
	defaultBindHost = "0.0.0.0"
)

// Load reads the whole config tree under root. A broken entry file disables only
// itself and comes back in Tree.Broken; only a broken config.toml — which every
// entry resolves against — fails the load outright (docs/specs/CONFIG.md).
//
// A tree with no root directory or no models/ directory is empty, not an error:
// creating them is the first-run scaffold's job, and Load only reads.
func Load(root string) (*Tree, error) {
	settings, err := loadSettings(filepath.Join(root, settingsFile))
	if err != nil {
		return nil, err
	}
	tree := &Tree{Root: root, Settings: settings}

	dir := filepath.Join(root, entriesDir)
	files, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return tree, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the entries directory %s: %w", dir, err)
	}

	// os.ReadDir yields filename order, and an id is its filename minus .toml, so
	// both result lists come out ordered by id.
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), tomlExt) {
			continue
		}
		id := strings.TrimSuffix(file.Name(), tomlExt)
		path := filepath.Join(dir, file.Name())
		entry, err := loadEntry(id, path, settings)
		if err != nil {
			tree.Broken = append(tree.Broken, BrokenEntry{ID: id, Path: path, Err: err})
			continue
		}
		tree.Entries = append(tree.Entries, *entry)
	}
	return tree, nil
}

// loadSettings reads config.toml. The file is optional, so a missing one yields
// the zero Settings; anything else wrong with it is a tree-level failure.
func loadSettings(path string) (Settings, error) {
	var settings Settings

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("cannot read %s: %w", path, err)
	}

	table, err := parseTable(data)
	if err != nil {
		return settings, fmt.Errorf("%s: %w", path, err)
	}
	if err := treeSchema.check(table, ""); err != nil {
		return settings, fmt.Errorf("%s: %w", path, err)
	}

	settings.DefaultPort = optInt(table, "default_port")
	settings.DefaultHost = optString(table, "default_host")
	if tools, ok := table["tools"].(map[string]any); ok {
		settings.Tools = Tools{
			LlamaServer: optString(tools, "llama_server"),
			MLXLMServer: optString(tools, "mlx_lm_server"),
			HF:          optString(tools, "hf"),
		}
	}
	return settings, nil
}

// loadEntry reads one entry file and resolves it against the tree settings. Every
// error it returns belongs to this entry alone — it disables this entry and
// nothing else.
func loadEntry(id, path string, settings Settings) (*Entry, error) {
	if !isName(id) {
		return nil, fmt.Errorf("invalid entry id %q: an id is the filename minus .toml and may hold only letters, digits, '-', '_' and '.'", id)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	table, err := parseTable(data)
	if err != nil {
		return nil, err
	}
	if err := entrySchema.check(table, ""); err != nil {
		return nil, err
	}
	return resolveEntry(id, path, table, settings)
}

// resolveEntry turns a checked table into an Entry, applying the rules that need
// more than one key: quant belongs to llama, and port, host and name fall back to
// the tree settings and the id (docs/specs/CONFIG.md).
func resolveEntry(id, path string, table map[string]any, settings Settings) (*Entry, error) {
	entry := Entry{
		ID:      id,
		Path:    path,
		Backend: Backend(optString(table, "backend")),
		Repo:    optString(table, "repo"),
		Quant:   optString(table, "quant"),
		Port:    optInt(table, "port"),
		Host:    optString(table, "host"),
		Name:    optString(table, "name"),
		Args:    optStrings(table, "args"),
	}

	if entry.Quant != "" && entry.Backend != BackendLlama {
		return nil, &KeyError{
			Key:    "quant",
			Reason: fmt.Sprintf("only the %q backend takes a quant; an mlx quantization is its own repo", BackendLlama),
		}
	}

	if entry.Port == 0 {
		if settings.DefaultPort == 0 {
			return nil, &KeyError{
				Key:    "port",
				Reason: "required: this entry sets no port and " + settingsFile + " sets no default_port",
			}
		}
		entry.Port = settings.DefaultPort
	}

	if entry.Host == "" {
		entry.Host = settings.DefaultHost
	}
	if entry.Host == "" {
		entry.Host = defaultBindHost
	}

	if entry.Name == "" {
		entry.Name = id
	}
	return &entry, nil
}

// parseTable parses one config file into a plain table. Decoding into a map
// rather than a Go struct is what lets the schema be the contract: it alone
// decides which keys exist, what type each takes and what an error says.
func parseTable(data []byte) (map[string]any, error) {
	table := map[string]any{}
	if err := toml.Unmarshal(data, &table); err != nil {
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			row, column := decodeErr.Position()
			return nil, fmt.Errorf("line %d column %d: %w", row, column, err)
		}
		return nil, err
	}
	return table, nil
}

// The three readers below run after schema.check has proved the file's types, so
// a value of the wrong type is unreachable; an absent key reads as the zero
// value, which resolveEntry and loadSettings turn into the documented default.

func optString(table map[string]any, name string) string {
	value, ok := table[name].(string)
	if !ok {
		return ""
	}
	return value
}

func optInt(table map[string]any, name string) int {
	value, ok := table[name].(int64)
	if !ok {
		return 0
	}
	return int(value)
}

func optStrings(table map[string]any, name string) []string {
	list, ok := table[name].([]any)
	if !ok {
		return nil
	}
	values := make([]string, len(list))
	for i, element := range list {
		values[i] = element.(string)
	}
	return values
}
