package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// kind classifies a config key's TOML type. It decides what the parser must find
// in the file and names the type in the `cria docs` output.
type kind int

const (
	kindString kind = iota
	kindInteger
	kindStringList
	kindTable
	kindTableArray
)

// String names the kind the way TOML and `cria docs` spell it.
func (k kind) String() string {
	switch k {
	case kindString:
		return "string"
	case kindInteger:
		return "integer"
	case kindStringList:
		return "string[]"
	case kindTable:
		return "table"
	case kindTableArray:
		return "table[]"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// holdsKeys reports whether a value of this kind is made of further keys — a
// [table] or a list of [[table]]s. Those keys carry their parent's name as a
// prefix everywhere: in an error, in the docs table and in an example.
func (k kind) holdsKeys() bool {
	return k == kindTable || k == kindTableArray
}

// key is one config key: the type it takes, the rules it must satisfy, and the
// line that documents it. The schemas below are the only place a config key is
// declared — decoding, validation and `cria docs` all read them, so a schema
// change updates the documentation by construction (docs/specs/CONFIG.md).
//
// Which backends take a key, and what value it takes there, are declared here
// too: internal/engine imports this package, so the per-backend metadata cannot
// live with the engines without a cycle. It is written to be total over
// Backends() rather than to have a default — see exampleFor.
type key struct {
	name         string             // the TOML key
	kind         kind               // the type the file must use
	required     bool               // absent is an error on its own
	onlyBackends []Backend          // the backends that take this key; empty means every backend
	rules        string             // the docs line: what the key means and what constrains it
	example      string             // a valid value in TOML syntax, the same one under every backend
	examples     map[Backend]string // a value per backend, where the backends genuinely differ
	keys         schema             // kindTable only: the sub-table's own keys
	check        func(v any) error  // the value rules this key can judge on its own
}

// takenBy reports whether an entry on this backend may set this key. A key that
// names no backends is taken by all of them; one that names some is refused for
// every backend outside the list, so a key applicable to two of three backends
// needs no third state.
func (k key) takenBy(backend Backend) bool {
	return len(k.onlyBackends) == 0 || slices.Contains(k.onlyBackends, backend)
}

// takenByNamed is the clause a refusal opens with: which engines this key
// belongs to. An entry file names its engine in its backend key and an engine
// file is named after one, so both refusals open the same way.
func (k key) takenByNamed() string {
	named := make([]string, 0, len(k.onlyBackends))
	for _, backend := range k.onlyBackends {
		named = append(named, fmt.Sprintf("%q", backend))
	}
	if len(named) == 1 {
		return "only the " + named[0] + " engine takes it"
	}
	return "only the " + strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1] + " engines take it"
}

// backendNote is how the docs table flags a key not every backend takes; a key
// they all take carries no note at all.
func (k key) backendNote() string {
	if len(k.onlyBackends) == 0 {
		return ""
	}
	named := make([]string, 0, len(k.onlyBackends))
	for _, backend := range k.onlyBackends {
		named = append(named, string(backend))
	}
	return strings.Join(named, ", ") + " only — "
}

// exampleFor is the value this key takes in one backend's example.
//
// A key declares either one example that is true under every backend or an
// example per backend — never one backend's value standing in for another's. A
// shared default would hand an agent a repo the backend it names cannot serve,
// and the example would still read as deliberate; TestEveryKeyExamplesEveryBackend
// holds the declaration to that rule.
func (k key) exampleFor(backend Backend) string {
	if value, ok := k.examples[backend]; ok {
		return value
	}
	return k.example
}

// header spells the line a keyed value opens with in a file: [table] for one
// table, [[table]] for each table of a list. prefix carries the enclosing key
// names, the way TOML nests them.
func (k key) header(prefix string) string {
	if k.kind == kindTableArray {
		return "[[" + prefix + k.name + "]]"
	}
	return "[" + prefix + k.name + "]"
}

// schema is an ordered list of keys; the order is the order `cria docs` prints
// them.
type schema []key

// entrySchema is the entry contract: one models/<id>.toml file is one launchable
// thing (docs/specs/CONFIG.md). Rules that need more than one key — a key its
// backend does not take, port falling back to default_port — live in
// resolveEntry, the only place that sees a whole entry next to the tree settings.
var entrySchema = schema{
	{
		name:     "backend",
		kind:     kindString,
		required: true,
		rules:    `the server program to run: "llama" (llama-server) or "mlx" (mlx_lm.server)`,
		examples: map[Backend]string{BackendLlama: `"llama"`, BackendMLX: `"mlx"`},
		check:    checkBackend,
	},
	{
		name:     "repo",
		kind:     kindString,
		required: true,
		rules:    "Hugging Face repo id, org/name; the server fetches the model itself",
		examples: map[Backend]string{
			BackendLlama: `"unsloth/Qwen3-30B-A3B-GGUF"`,
			BackendMLX:   `"mlx-community/Qwen3-30B-A3B-4bit"`,
		},
		check: checkRepo,
	},
	{
		name:         "quant",
		kind:         kindString,
		onlyBackends: []Backend{BackendLlama},
		rules:        "the quantization to serve, spelled exactly as the repo's files name it, UD- prefix and all; omit it and the server picks the repo's default (an mlx quantization is its own repo)",
		example:      `"UD-Q4_K_XL"`,
		check:        checkNonEmpty,
	},
	{
		name:    "port",
		kind:    kindInteger,
		rules:   "the port to serve on; optional when config.toml sets default_port, required otherwise",
		example: "8080",
		check:   checkPort,
	},
	{
		name:    "host",
		kind:    kindString,
		rules:   `bind address; defaults to config.toml default_host, else "` + defaultBindHost + `"`,
		example: `"0.0.0.0"`,
		check:   checkNonEmpty,
	},
	{
		name:  "name",
		kind:  kindString,
		rules: "display name; defaults to the entry id",
		examples: map[Backend]string{
			BackendLlama: `"Qwen3 30B A3B"`,
			BackendMLX:   `"Qwen3 30B A3B (MLX 4bit)"`,
		},
		check: checkNonEmpty,
	},
	{
		name:  "args",
		kind:  kindStringList,
		rules: "what this model is served with: the server's own flags, token for token, passed verbatim. They override the flags engines/<backend>.toml sets for every entry, and cria composes the model, port and host flags itself",
		examples: map[Backend]string{
			BackendLlama: `["--ctx-size", "16384", "--jinja"]`,
			BackendMLX:   `["--max-tokens", "32768"]`,
		},
		check: checkArgs,
	},
	{
		name:  "choice",
		kind:  kindTableArray,
		rules: "a pick-one axis this entry varies on: the [[choice.option]] tables under it are the picks, and cria folds the picked one into the launch without reading its flags — so flags that must vary together belong in the same option",
		keys:  choiceSchema,
	},
}

// choiceSchema is one [[choice]]: a named axis and the options to pick between.
// Rules that span options — names unique within the axis, a typed key or a flag
// two parts of a launch would fight over — live in resolveChoices, the only place
// that sees a whole entry's axes at once.
var choiceSchema = schema{
	{
		name:     "name",
		kind:     kindString,
		required: true,
		rules:    "the axis's name, unique within the entry; it is what `cria start <id> choice=option` names, so it is spelled like an entry id",
		example:  `"quant"`,
		check:    checkName,
	},
	{
		name:     "option",
		kind:     kindTableArray,
		required: true,
		rules:    "the picks, in file order; at least one, and the first is the default until a pick is made",
		keys:     choiceOptionSchema,
		check:    checkAtLeastOneOption,
	},
}

// choiceOptionSchema is one [[choice.option]]: what this pick replaces and what
// it adds. It is deliberately the entry's own vocabulary minus everything cria
// resolves once per entry — a pick varies the model and its flags, never the port
// it is served on.
var choiceOptionSchema = schema{
	{
		name:     "name",
		kind:     kindString,
		required: true,
		rules:    "the pick's name, unique within its choice; spelled like an entry id",
		examples: map[Backend]string{BackendLlama: `"q4"`, BackendMLX: `"4bit"`},
		check:    checkName,
	},
	{
		name:         "quant",
		kind:         kindString,
		onlyBackends: []Backend{BackendLlama},
		rules:        "replaces the entry's quant when this option is picked; only one choice's options may set it",
		example:      `"UD-Q4_K_XL"`,
		check:        checkNonEmpty,
	},
	{
		name:  "repo",
		kind:  kindString,
		rules: "replaces the entry's repo when this option is picked (an mlx quantization is its own repo); only one choice's options may set it",
		examples: map[Backend]string{
			BackendLlama: `"unsloth/Qwen3-30B-A3B-128K-GGUF"`,
			BackendMLX:   `"mlx-community/Qwen3-30B-A3B-8bit"`,
		},
		check: checkRepo,
	},
	{
		name:  "args",
		kind:  kindStringList,
		rules: "merged over the entry's args when this option is picked, so a flag set here is what that pick changes; another choice's options may not set the same flag, since those two compose into one launch",
		examples: map[Backend]string{
			BackendLlama: `["--n-cpu-moe", "24"]`,
			BackendMLX:   `["--temp", "0.7"]`,
		},
		check: checkArgs,
	},
}

// engineSchema is engines/<engine>.toml: what this machine serves every model of
// one engine with. It is the level entries override, and it is optional — an
// engine with no file has no defaults of its own (docs/specs/CONFIG.md).
//
// The router takes keys the process engines do not: it serves no entry of its
// own, so the port it answers on, the address it binds and its own flags have
// nowhere else to live (docs/specs/CONFIG.md).
var engineSchema = schema{
	{
		name:         "port",
		kind:         kindInteger,
		onlyBackends: []Backend{BackendRouter},
		rules:        "the port the router serves on; it has no entry to take one from, so without this key there is no router to start (give it a port of its own — entries keep theirs)",
		examples:     map[Backend]string{BackendRouter: "11434"},
		check:        checkPort,
	},
	{
		name:         "host",
		kind:         kindString,
		onlyBackends: []Backend{BackendRouter},
		rules:        `the router's bind address; defaults to config.toml default_host, else "` + defaultBindHost + `"`,
		examples:     map[Backend]string{BackendRouter: `"0.0.0.0"`},
		check:        checkNonEmpty,
	},
	{
		name:  "args",
		kind:  kindStringList,
		rules: "what every model this engine serves starts from — the machine's own flags, not the model's; an entry that sets the same flag overrides it. Under the router these become the composed preset's [*] block, so each flag must take one value or none",
		examples: map[Backend]string{
			BackendLlama:  `["-ngl", "99", "-fa", "on"]`,
			BackendMLX:    `["--log-level", "INFO"]`,
			BackendRouter: `["-ngl", "99", "-fa", "on"]`,
		},
		check: checkArgs,
	},
	{
		name:         "router_args",
		kind:         kindStringList,
		onlyBackends: []Backend{BackendRouter},
		rules:        "the router process's own flags, passed verbatim — how many models it may keep loaded, whether it loads them on request, and anything else llama-server takes as a router; the flags for the models it serves belong in args",
		examples:     map[Backend]string{BackendRouter: `["--models-max", "2"]`},
		check:        checkArgs,
	},
}

// treeSchema is config.toml: the tree-wide defaults and the tool path overrides.
// The file itself is optional (docs/specs/CONFIG.md).
var treeSchema = schema{
	{
		name:    "default_port",
		kind:    kindInteger,
		rules:   "the port for entries that declare none",
		example: "8080",
		check:   checkPort,
	},
	{
		name:    "default_host",
		kind:    kindString,
		rules:   `the bind address for entries that declare none; "` + defaultBindHost + `" when absent`,
		example: `"0.0.0.0"`,
		check:   checkNonEmpty,
	},
	{
		name:  "tools",
		kind:  kindTable,
		rules: "absolute paths to the managed tools, each overriding PATH lookup",
		keys: schema{
			{
				name:    "llama_server",
				kind:    kindString,
				rules:   "absolute path to llama-server",
				example: `"/opt/homebrew/bin/llama-server"`,
				check:   checkAbsPath,
			},
			{
				name:    "mlx_lm_server",
				kind:    kindString,
				rules:   "absolute path to mlx_lm.server",
				example: `"/opt/homebrew/bin/mlx_lm.server"`,
				check:   checkAbsPath,
			},
			{
				name:    "hf",
				kind:    kindString,
				rules:   "absolute path to hf",
				example: `"/opt/homebrew/bin/hf"`,
				check:   checkAbsPath,
			},
		},
	},
}

// composedFlags are the flags cria builds itself out of the tree's own keys
// (docs/specs/CONFIG.md): the bind address, the port, and the flag each engine's
// models are named under. An args list restating one of them would fight the
// composed command line, so it is refused instead of silently overriding — and
// every engine's model flag is refused under every engine, since a flag one
// server does not take is a mistake either way.
func composedFlags() []string {
	flags := make([]string, 0, len(engines)+2)
	for _, engine := range engines {
		flags = append(flags, engine.modelFlag)
	}
	return append(flags, "--host", "--port")
}

// check validates a parsed TOML table against the schema. An unknown key, a wrong
// type, a missing required key or a value its own rule rejects is an error naming
// that key, so a typo fails loudly instead of defaulting silently
// (docs/specs/CONFIG.md). prefix qualifies key names inside sub-tables.
func (s schema) check(table map[string]any, prefix string) error {
	for _, name := range sortedNames(table) {
		if s.lookup(name) == nil {
			return &KeyError{
				Key:    prefix + name,
				Reason: "unknown key; the keys allowed here are " + s.names(),
			}
		}
	}

	for _, k := range s {
		value, present := table[k.name]
		if !present {
			if k.required {
				return &KeyError{Key: prefix + k.name, Reason: "required, but the file does not set it"}
			}
			continue
		}
		if err := k.kind.match(value); err != nil {
			return &KeyError{Key: prefix + k.name, Reason: err.Error()}
		}
		if k.check != nil {
			if err := k.check(value); err != nil {
				return &KeyError{Key: prefix + k.name, Reason: err.Error()}
			}
		}
		switch k.kind {
		case kindTable:
			if err := k.keys.check(value.(map[string]any), prefix+k.name+"."); err != nil {
				return err
			}
		case kindTableArray:
			// Every table in the list is the same key, so they all check against the
			// same sub-schema under the same name: the report points at the key, and
			// the file has one place to look for it.
			for _, element := range value.([]any) {
				if err := k.keys.check(element.(map[string]any), prefix+k.name+"."); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// lookup finds the definition of name, or nil when the schema declares no such
// key.
func (s schema) lookup(name string) *key {
	for i := range s {
		if s[i].name == name {
			return &s[i]
		}
	}
	return nil
}

// names lists the schema's keys for an error message.
func (s schema) names() string {
	names := make([]string, len(s))
	for i, k := range s {
		names[i] = k.name
	}
	return strings.Join(names, ", ")
}

// sortedNames orders a parsed table's keys so that a file with more than one
// problem always reports the same one.
func sortedNames(table map[string]any) []string {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// match reports whether a value the parser produced is what this kind requires,
// describing the mismatch when it is not.
func (k kind) match(value any) error {
	switch k {
	case kindString:
		if _, ok := value.(string); !ok {
			return typeMismatch(k, value)
		}
	case kindInteger:
		if _, ok := value.(int64); !ok {
			return typeMismatch(k, value)
		}
	case kindStringList:
		list, ok := value.([]any)
		if !ok {
			return typeMismatch(k, value)
		}
		for i, element := range list {
			if _, ok := element.(string); !ok {
				return fmt.Errorf("element %d: want string, got %s", i, tomlType(element))
			}
		}
	case kindTable:
		if _, ok := value.(map[string]any); !ok {
			return typeMismatch(k, value)
		}
	case kindTableArray:
		list, ok := value.([]any)
		if !ok {
			return typeMismatch(k, value)
		}
		for i, element := range list {
			if _, ok := element.(map[string]any); !ok {
				return fmt.Errorf("element %d: want table, got %s", i, tomlType(element))
			}
		}
	}
	return nil
}

func typeMismatch(want kind, got any) error {
	return fmt.Errorf("want %s, got %s", want, tomlType(got))
}

// tomlType names the TOML type behind a decoded value, so a type error speaks the
// file's language rather than Go's.
func tomlType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case int64:
		return "integer"
	case float64:
		return "float"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "table"
	case time.Time:
		return "datetime"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// checkBackend holds the backend enum: the servers cria knows how to launch. It
// reads the registry rather than naming them, so the set a file may declare and
// the set the examples are rendered for cannot come apart.
func checkBackend(value any) error {
	declared := Backend(value.(string))
	if slices.Contains(Backends(), declared) {
		return nil
	}
	named := make([]string, 0, len(engines))
	for _, backend := range Backends() {
		named = append(named, fmt.Sprintf("%q", backend))
	}
	return fmt.Errorf("want one of %s, got %q", strings.Join(named, ", "), declared)
}

// checkRepo holds the Hub reference shape — org/name, the form both servers take
// on their command line.
func checkRepo(value any) error {
	repo := value.(string)
	org, name, split := strings.Cut(repo, "/")
	if !split || !isName(org) || !isName(name) {
		return fmt.Errorf("want a Hugging Face repo id of the form org/name, got %q", repo)
	}
	return nil
}

// checkPort keeps a port inside the range a server can actually bind.
func checkPort(value any) error {
	port := value.(int64)
	if port < 1 || port > 65535 {
		return fmt.Errorf("want a port between 1 and 65535, got %d", port)
	}
	return nil
}

// checkNonEmpty refuses a key written with a blank value: the author meant
// something, and an empty string would silently behave like an omitted key.
func checkNonEmpty(value any) error {
	if strings.TrimSpace(value.(string)) == "" {
		return fmt.Errorf("want a value, got an empty string; remove the key instead")
	}
	return nil
}

// checkName holds the charset of a name an author gives a choice or one of its
// options: both are typed on the command line (`cria start <id> choice=option`),
// so they are spelled exactly like an entry id.
func checkName(value any) error {
	name := value.(string)
	if !isName(name) {
		return fmt.Errorf("want letters, digits, '-', '_' and '.', got %q", name)
	}
	return nil
}

// checkAtLeastOneOption refuses an axis with nothing on it: a choice exists to be
// picked from, and one option is already meaningful — a named block of args that
// is always on.
func checkAtLeastOneOption(value any) error {
	if len(value.([]any)) == 0 {
		return fmt.Errorf("want at least one option to pick between, got an empty list")
	}
	return nil
}

// checkAbsPath refuses a relative tool path: the override exists precisely to
// bypass PATH lookup, so it must name the binary outright.
func checkAbsPath(value any) error {
	path := value.(string)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("want an absolute path, got %q", path)
	}
	return nil
}

// checkArgs refuses args that restate a flag cria composes itself, in both the
// separate-value and --flag=value spellings. Everything else in a list is the
// server's business: the rules that need more than one list in view — a flag two
// axes both set — live in load.go.
func checkArgs(value any) error {
	composed := composedFlags()
	for i, element := range value.([]any) {
		flag, ok := flagToken(element.(string))
		if !ok {
			continue
		}
		if slices.Contains(composed, flag) {
			return fmt.Errorf("element %d: cria composes %s itself from this entry's keys; remove it from args", i, flag)
		}
	}
	return nil
}

// isName reports whether s is spelled with the characters cria allows in an entry
// id and in each half of a Hub repo id — the two charsets are the same set
// (docs/specs/CONFIG.md).
func isName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
