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
	// enginesDir holds one file per engine: what this machine serves every entry
	// of that backend with. Each file is optional, and so is the directory.
	enginesDir = "engines"
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
// itself and comes back in Tree.Broken; a broken config.toml or engines/ file —
// which whole sets of entries resolve against — fails the load outright
// (docs/specs/CONFIG.md).
//
// A tree with no root directory or no models/ directory is empty, not an error:
// creating them is the first-run scaffold's job, and Load only reads.
func Load(root string) (*Tree, error) {
	settings, err := loadSettings(filepath.Join(root, settingsFile))
	if err != nil {
		return nil, err
	}
	engineArgs, router, err := loadEngines(root, settings)
	if err != nil {
		return nil, err
	}
	tree := &Tree{Root: root, Settings: settings, Router: router}

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
		entry, err := loadEntry(id, path, settings, engineArgs)
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
			VLLM:        optString(tools, "vllm"),
			HF:          optString(tools, "hf"),
		}
	}
	return settings, nil
}

// loadEngines reads engines/<engine>.toml for every engine cria has. A file that
// is not there is an engine with no configuration of its own — the common case,
// and never an error.
//
// A file that is there and wrong fails the whole load rather than disabling the
// entries it governs: it is tree-wide configuration, like config.toml, and the
// alternative reports one file's mistake once per entry while pointing the
// reader at the wrong file.
//
// The router's file carries more than args — it configures a process no entry
// declares — so it comes back as its own value beside the args every other
// engine's entries start from.
func loadEngines(root string, settings Settings) (map[Backend][]string, RouterConfig, error) {
	args := make(map[Backend][]string, len(engines))
	router := RouterConfig{Path: enginePath(root, BackendRouter)}

	for _, engine := range Engines() {
		path := enginePath(root, engine)

		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, RouterConfig{}, fmt.Errorf("cannot read %s: %w", path, err)
		}

		table, err := parseTable(data)
		if err != nil {
			return nil, RouterConfig{}, fmt.Errorf("%s: %w", path, err)
		}
		if err := engineSchema.check(table, ""); err != nil {
			return nil, RouterConfig{}, fmt.Errorf("%s: %w", path, err)
		}
		if err := refuseKeysOfOtherEngines(engineSchema, table, engine); err != nil {
			return nil, RouterConfig{}, fmt.Errorf("%s: %w", path, err)
		}

		args[engine] = optStrings(table, "args")
		if engine == BackendRouter {
			router.Port = optInt(table, "port")
			router.Host = optString(table, "host")
			router.Args = args[engine]
			router.RouterArgs = optStrings(table, "router_args")
		}
	}

	// The router binds by the same rule an entry does (docs/specs/CONFIG.md): its
	// own host, else the tree's default, else every address the host has.
	if router.Host == "" {
		router.Host = settings.DefaultHost
	}
	if router.Host == "" {
		router.Host = defaultBindHost
	}
	return args, router, nil
}

// enginePath is where one engine's file lives.
func enginePath(root string, engine Backend) string {
	return filepath.Join(root, enginesDir, string(engine)+tomlExt)
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
func loadEntry(id, path string, settings Settings, engineArgs map[Backend][]string) (*Entry, error) {
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
	return resolveEntry(id, path, table, settings, engineArgs)
}

// resolveEntry turns a checked table into an Entry, applying the rules that need
// more than one key: a key belonging to some backends only, and port, host, name
// and the engine's own args falling back to what the tree declares around this
// file (docs/specs/CONFIG.md).
func resolveEntry(id, path string, table map[string]any, settings Settings, engineArgs map[Backend][]string) (*Entry, error) {
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
	entry.EngineArgs = engineArgs[entry.Backend]

	choices, err := resolveChoices(table, entry.Backend)
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

// refuseOtherBackendKeys refuses a key the schema binds to backends this entry
// does not run, from the same declaration `cria docs` renders that backend's
// example from. prefix qualifies the key name the way the schema check does.
func refuseOtherBackendKeys(s schema, table map[string]any, prefix string, backend Backend) error {
	return refuseKeysOfOthers(s, table, prefix, backend, fmt.Sprintf("this entry's backend is %q", backend))
}

// refuseKeysOfOtherEngines is the same rule for an engine file: a key another
// engine takes is refused in this one, naming the engine this file configures
// rather than an entry's backend key.
func refuseKeysOfOtherEngines(s schema, table map[string]any, engine Backend) error {
	return refuseKeysOfOthers(s, table, "", engine, fmt.Sprintf("this file configures the %q engine", engine))
}

// refuseKeysOfOthers holds the rule both refusals are: a key declared for some
// ids only is an error under any other one. whose is the clause that says which
// id this file speaks for, because a file says it in its own way.
func refuseKeysOfOthers(s schema, table map[string]any, prefix string, id Backend, whose string) error {
	for _, k := range s {
		if k.takenBy(id) {
			continue
		}
		if _, present := table[k.name]; present {
			return &KeyError{
				Key:    prefix + k.name,
				Reason: fmt.Sprintf("%s; %s", k.takenByNamed(), whose),
			}
		}
	}
	return nil
}

// resolveChoices turns an entry's checked [[choice]] tables into its axes. It
// holds every rule that needs more than one option in view: names that identify
// a choice and a pick, a typed key replaced by one axis only, and the flag
// collisions below (docs/specs/CONFIG.md).
func resolveChoices(table map[string]any, backend Backend) ([]Choice, error) {
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

	if err := refuseFlagCollisions(choices); err != nil {
		return nil, err
	}
	return choices, nil
}

// replacedTwice phrases the refusal of a key two axes both replace; both are
// named because either one of them is the one to move.
func replacedTwice(first, second string) string {
	return fmt.Sprintf("the options of choices %q and %q both replace it, and both are picked at once; keep it on one axis", first, second)
}

// argsHome is one option's args, with the axis it belongs to. Options of the
// same choice are alternatives — they never compose together, so they share
// flags freely; options of two different choices are both picked at once, and a
// flag in both of them has no value to take (docs/specs/CONFIG.md).
//
// The entry's own args are not a home here, and neither are its engine's: those
// are levels of their own, which the level above is entitled to override.
type argsHome struct {
	label  string
	choice int
	flags  []string
}

// refuseFlagCollisions refuses a flag two options of different axes could both
// contribute. It compares the options pairwise rather than enumerating the
// combinations they make: the pairs are what a collision is made of, and there
// are few of them where combinations multiply.
func refuseFlagCollisions(choices []Choice) error {
	var homes []argsHome
	for i, choice := range choices {
		for _, option := range choice.Options {
			homes = append(homes, argsHome{
				label:  fmt.Sprintf("option %q of choice %q", option.Name, choice.Name),
				choice: i,
				flags:  flagTokens(option.Args),
			})
		}
	}

	for i, home := range homes {
		for _, earlier := range homes[:i] {
			if earlier.choice == home.choice {
				continue
			}
			for _, flag := range home.flags {
				if !slices.Contains(earlier.flags, flag) {
					continue
				}
				return &KeyError{
					Key: "choice.option.args",
					Reason: fmt.Sprintf("%s is set by %s and by %s, which are picked at once; keep it in one of them",
						flag, earlier.label, home.label),
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
	strs := make([]string, len(list))
	for i, element := range list {
		strs[i] = element.(string)
	}
	return strs
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
