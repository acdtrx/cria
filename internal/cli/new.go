package cli

import (
	"errors"
	"io/fs"
	"strings"

	"cria/internal/config"
	"cria/internal/format"
)

// newUsage is the one line every refusal of this subcommand ends with.
const newUsage = "usage: cria new <id> [" + llamaFlag + "|" + mlxFlag + "]"

// newEntry runs `cria new <id> [--llama|--mlx]`: it creates the entry file and
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

// parseNew reads the command line: one id, and at most one of the two backend
// flags. Neither flag is llama — the default backend is named as well as
// implied, so `--mlx` is not the only spelled-out choice. refusal is empty when
// the invocation is routable.
func parseNew(args []string) (id string, backend config.Backend, refusal string) {
	var ids []string
	llama, mlx := false, false
	for _, arg := range args {
		switch {
		case arg == llamaFlag:
			llama = true
		case arg == mlxFlag:
			mlx = true
		case strings.HasPrefix(arg, "-"):
			return "", "", "unknown flag " + arg
		default:
			ids = append(ids, arg)
		}
	}

	if llama && mlx {
		return "", "", llamaFlag + " and " + mlxFlag + " name different backends; pass one or neither"
	}
	if len(ids) == 0 {
		return "", "", "no entry named"
	}
	if len(ids) > 1 {
		return "", "", "one entry at a time (got " + strings.Join(ids, ", ") + ")"
	}
	if mlx {
		return ids[0], config.BackendMLX, ""
	}
	return ids[0], config.BackendLlama, ""
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
