package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"cria/internal/config"
	"cria/internal/format"
)

// newUsage is the one line every refusal of this subcommand ends with. It names
// one flag per backend an entry may declare, so the alternatives it offers are
// the entries cria can actually scaffold.
var newUsage = "usage: cria new <id> [" + strings.Join(backendFlags(), "|") + "]"

// backendFlag is the flag that scaffolds one backend's entry: the flag prefix
// and the backend's own id. There is no table mapping flags to backends — a
// backend an entry may declare is one `cria new` can scaffold, spelled the way
// the entry file spells it.
func backendFlag(backend config.Backend) string { return "--" + string(backend) }

// backendFlags names them all, in the order the tree declares them in.
func backendFlags() []string {
	declared := config.Backends()
	flags := make([]string, 0, len(declared))
	for _, backend := range declared {
		flags = append(flags, backendFlag(backend))
	}
	return flags
}

// newEntry runs `cria new <id> [--llama|--mlx|--vllm]`: it creates the entry file and
// opens it in the user's editor — the two steps of adding a model, in one
// command.
//
// What it writes is the example `cria docs` prints for that backend, from the
// same schema definitions the parser checks the file against (config.ExampleEntry).
// There is no second template: the page an agent is told to read and the file a
// person is handed are one string (CLAUDE.md: schema and docs are one source).
//
// This is cria's only write into the tree beyond the first-run scaffold, and it
// creates — it never rewrites. Everything after the file exists is reporting:
// what the editor did to it, and whether the tree can now serve it.
func (a *app) newEntry(args []string) int {
	id, backend, refusal := parseNew(args)
	if refusal != "" {
		return a.usage("new: %s; %s", refusal, newUsage)
	}
	if !config.ValidID(id) {
		return a.fail("new %s: %q cannot name an entry; an id is a filename minus .toml and holds letters, digits, '-', '_' and '.' only", id, id)
	}

	tree, err := a.tree()
	if err != nil {
		return a.fail("new %s: %v", id, err)
	}

	path, err := config.CreateEntry(tree.Root, id, backend)
	if errors.Is(err, fs.ErrExist) {
		// Asking and creating are one step, so this answers for a refused entry
		// file too: it exists, cria did not write it, and `cria edit` opens it.
		return a.fail("new %s: %s already exists; `cria edit %s` opens it", id, path, id)
	}
	if err != nil {
		return a.fail("new %s: %v", id, err)
	}
	a.printf("created %s\n", path)

	command := editorCommand()
	if len(command) == 0 {
		// `cria edit` refuses here, because opening the file is all it was asked
		// for. `cria new` has already done the thing it was asked for — the file
		// is written — so it says what would have opened it and exits zero.
		a.note("no editor is set; set $EDITOR (or $VISUAL) and `cria new` will open the file it writes")
		return exitOK
	}
	if code := a.openEditor(command, "new", id, path); code != exitOK {
		return code
	}
	return a.reportNewEntry(id, path)
}

// parseNew reads the command line: one id, and at most one backend flag. Every
// backend an entry may declare has one, the default included — the backend a
// bare invocation takes is named as well as implied, so none of them is the
// unspoken one. refusal is empty when the invocation is routable.
func parseNew(args []string) (id string, backend config.Backend, refusal string) {
	var ids []string
	named := config.Backend("")
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			ids = append(ids, arg)
			continue
		}
		flagged, ok := backendNamedBy(arg)
		if !ok {
			if engine, asked := engineNamedBy(arg); asked {
				return "", "", nothingToScaffold(engine)
			}
			return "", "", "unknown flag " + arg
		}
		if named != "" && named != flagged {
			return "", "", backendFlag(named) + " and " + arg + " name different backends; pass one or neither"
		}
		named = flagged
	}

	if len(ids) == 0 {
		return "", "", "no entry named"
	}
	if len(ids) > 1 {
		return "", "", "one entry at a time (got " + strings.Join(ids, ", ") + ")"
	}
	if named != "" {
		return ids[0], named, ""
	}
	// A bare invocation scaffolds the first backend the tree declares: the one
	// that exists on every host cria runs on (docs/TECH-STACK.md).
	return ids[0], config.Backends()[0], ""
}

// backendNamedBy reads one flag as a backend choice.
func backendNamedBy(flag string) (config.Backend, bool) {
	for _, backend := range config.Backends() {
		if flag == backendFlag(backend) {
			return backend, true
		}
	}
	return "", false
}

// engineNamedBy reads a flag that names an engine no entry may declare — the
// router, whose models are ordinary entries. It is spotted rather than left to
// the unknown-flag refusal because asking for it is a reasonable guess, and the
// answer is a fact about how that engine is fed rather than a typo.
func engineNamedBy(flag string) (config.Backend, bool) {
	for _, id := range config.Engines() {
		if flag == backendFlag(id) && !slices.Contains(config.Backends(), id) {
			return id, true
		}
	}
	return "", false
}

// nothingToScaffold answers a flag naming an engine that has no entry file of
// its own. There is no template to write: the models such an engine serves are
// entries of the backends that do have one, and what makes them its own lives
// outside the tree (docs/specs/CONFIG.md).
func nothingToScaffold(engine config.Backend) string {
	return fmt.Sprintf("the %q engine serves ordinary entries, so there is no %q entry file to scaffold; "+
		"write the model's own entry (%s) and configure the engine in engines/%s.toml",
		engine, engine, strings.Join(backendFlags(), " or "), engine)
}

// reportNewEntry says what the tree makes of the file the editor just closed:
// the entry it now declares, or the key that disables it. The file is on disk
// either way — the exit code is about whether it can serve.
func (a *app) reportNewEntry(id, path string) int {
	tree, err := a.tree()
	if err != nil {
		return a.fail("new %s: %v", id, err)
	}
	for _, entry := range tree.Entries {
		if entry.ID == id {
			a.printf("%s: %s %s — start it: cria start %s --wait\n",
				entry.ID, entry.Backend, format.HubReference(entry.Repo, entry.Quant), id)
			return exitOK
		}
	}
	for _, broken := range tree.Broken {
		if broken.ID == id {
			return a.fail("new %s: %v; fix it: cria edit %s", id, broken.Err, id)
		}
	}
	return a.fail("new %s: nothing is left at %s; the editor did not save an entry file there", id, path)
}
