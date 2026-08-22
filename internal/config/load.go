package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// ValidID reports whether id may name an entry: the charset a filename must hold
// for the loader to read it back as an entry (docs/specs/CONFIG.md). `cria new`
// asks before it creates anything, so it never writes a file the tree would then
// refuse.
func ValidID(id string) bool {
	return isName(id)
}

// loadEntry reads one entry file and resolves it against the tree settings. Every
// error it returns belongs to this entry alone — it disables this entry and
// nothing else.
func loadEntry(id, path string, settings Settings) (*Entry, error) {
	if !ValidID(id) {
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
// more than one key: a key belonging to one backend, and port, host and name
// falling back to the tree settings and the id (docs/specs/CONFIG.md).
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

	if err := refuseOtherBackendKeys(entrySchema, table, "", entry.Backend); err != nil {
		return nil, err
	}

	choices, err := resolveChoices(table, entry.Backend, entry.Args)
	if err != nil {
		return nil, err
	}
	entry.Choices = choices

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

// refuseOtherBackendKeys refuses a key the schema binds to a backend this entry
// does not run, from the same declaration `cria docs` renders that backend's
// example from. prefix qualifies the key name the way the schema check does.
func refuseOtherBackendKeys(s schema, table map[string]any, prefix string, backend Backend) error {
	for _, k := range s {
		if k.onlyBackend == "" || k.onlyBackend == backend {
			continue
		}
		if _, present := table[k.name]; present {
			return &KeyError{
				Key:    prefix + k.name,
				Reason: fmt.Sprintf("only the %q backend takes it; this entry's backend is %q", k.onlyBackend, backend),
			}
		}
	}
	return nil
}

// resolveChoices turns an entry's checked [[choice]] tables into its axes. It
// holds every rule that needs more than one option in view: names that identify
// a choice and a pick, a typed key replaced by one axis only, and the flag
// collisions below (docs/specs/CONFIG.md).
func resolveChoices(table map[string]any, backend Backend, baseArgs []string) ([]Choice, error) {
	tables := optTables(table, "choice")
	if len(tables) == 0 {
		return nil, nil
	}

	choices := make([]Choice, 0, len(tables))
	axisNames := map[string]bool{}
	quantAxis, repoAxis := "", ""

	for _, choiceTable := range tables {
		choice := Choice{Name: optString(choiceTable, "name")}
		if axisNames[choice.Name] {
			return nil, &KeyError{
				Key:    "choice.name",
				Reason: fmt.Sprintf("the entry already has a choice named %q; a name identifies one axis", choice.Name),
			}
		}
		axisNames[choice.Name] = true

		optionNames := map[string]bool{}
		for _, optionTable := range optTables(choiceTable, "option") {
			if err := refuseOtherBackendKeys(choiceOptionSchema, optionTable, "choice.option.", backend); err != nil {
				return nil, err
			}
			option := ChoiceOption{
				Name:  optString(optionTable, "name"),
				Quant: optString(optionTable, "quant"),
				Repo:  optString(optionTable, "repo"),
				Args:  optStrings(optionTable, "args"),
			}
			if optionNames[option.Name] {
				return nil, &KeyError{
					Key:    "choice.option.name",
					Reason: fmt.Sprintf("choice %q already has an option named %q; a name identifies one pick", choice.Name, option.Name),
				}
			}
			optionNames[option.Name] = true

			// A key two axes both replace has no answer once both are picked — the same
			// reason a flag may not live in two axes.
			if option.Quant != "" {
				if quantAxis != "" && quantAxis != choice.Name {
					return nil, &KeyError{Key: "choice.option.quant", Reason: replacedTwice(quantAxis, choice.Name)}
				}
				quantAxis = choice.Name
			}
			if option.Repo != "" {
				if repoAxis != "" && repoAxis != choice.Name {
					return nil, &KeyError{Key: "choice.option.repo", Reason: replacedTwice(repoAxis, choice.Name)}
				}
				repoAxis = choice.Name
			}

			choice.Options = append(choice.Options, option)
		}
		choices = append(choices, choice)
	}

	if err := refuseFlagCollisions(baseArgs, choices); err != nil {
		return nil, err
	}
	return choices, nil
}

// replacedTwice phrases the refusal of a key two axes both replace; both are
// named because either one of them is the one to move.
func replacedTwice(first, second string) string {
	return fmt.Sprintf("the options of choices %q and %q both replace it, and both are picked at once; keep it on one axis", first, second)
}

// argsHome is one part of a launch that carries flags: the entry's own args, or
// one option's. Options of the same choice are alternatives — they never compose
// together, so they share flags freely; anything else can meet in one command
// line (docs/specs/CONFIG.md).
type argsHome struct {
	label  string
	choice int // the choice this home belongs to; -1 for the entry's own args
	tokens []string
}

// refuseFlagCollisions refuses a flag two parts of a launch could both
// contribute. It compares the parts pairwise rather than enumerating the
// combinations they make: the pairs are what a collision is made of, and there
// are few of them where combinations multiply.
func refuseFlagCollisions(baseArgs []string, choices []Choice) error {
	homes := []argsHome{{label: "the entry's args", choice: -1, tokens: flagTokens(baseArgs)}}
	for i, choice := range choices {
		for _, option := range choice.Options {
			homes = append(homes, argsHome{
				label:  fmt.Sprintf("option %q of choice %q", option.Name, choice.Name),
				choice: i,
				tokens: flagTokens(option.Args),
			})
		}
	}

	for i, home := range homes {
		for _, earlier := range homes[:i] {
			if earlier.choice == home.choice {
				continue
			}
			for _, token := range home.tokens {
				if !slices.Contains(earlier.tokens, token) {
					continue
				}
				return &KeyError{
					Key: "choice.option.args",
					Reason: fmt.Sprintf("%s is set by %s and by %s, which compose into one launch; keep it in one of them",
						token, earlier.label, home.label),
				}
			}
		}
	}
	return nil
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

// The readers below run after schema.check has proved the file's types, so a
// value of the wrong type is unreachable; an absent key reads as the zero value,
// which resolveEntry and loadSettings turn into the documented default.

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

func optTables(table map[string]any, name string) []map[string]any {
	list, ok := table[name].([]any)
	if !ok {
		return nil
	}
	tables := make([]map[string]any, len(list))
	for i, element := range list {
		tables[i] = element.(map[string]any)
	}
	return tables
}
