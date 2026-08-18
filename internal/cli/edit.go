package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"cria/internal/config"
)

// The two environment variables that name an editor, in the order every tool
// that opens one consults them: $VISUAL is the full-screen editor a terminal can
// hand itself over to, $EDITOR the fallback.
const (
	visualEnv = "VISUAL"
	editorEnv = "EDITOR"
)

// edit runs `cria edit <id>`: it opens that entry's file in the user's editor and
// waits for it to close.
//
// cria still never writes into the config tree — the editor does, on the user's
// behalf, and the file that comes back is whatever they saved (docs/specs/CONFIG.md).
// So there is no validation here either: `cria list` and `cria start <id> --wait`
// are what say whether the result parses and serves.
//
// A broken entry is editable, and that is the point of the subcommand: the file
// that fails to parse is exactly the one someone needs to open.
func (a *app) edit(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return a.usage("edit: one entry id; usage: cria edit <id>")
	}
	id := args[0]

	tree, err := a.tree()
	if err != nil {
		return a.fail("edit %s: %v", id, err)
	}
	path, found := entryFile(tree, id)
	if !found {
		return a.refuseUnknownFile(tree, id)
	}

	command := editorCommand()
	if len(command) == 0 {
		return a.fail("edit %s: no editor is set; set $EDITOR (or $VISUAL) to use cria edit — the file is %s", id, path)
	}

	editor := exec.Command(command[0], append(command[1:], path)...)
	// The editor takes over the terminal cria was invoked from: it reads keys and
	// draws a screen, so it gets the process's own streams rather than this
	// invocation's writers. Nothing about an editor session is cria's output.
	editor.Stdin, editor.Stdout, editor.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := editor.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return a.fail("edit %s: %s exited %d; check %s before starting the entry", id, command[0], exit.ExitCode(), path)
		}
		return a.fail("edit %s: cannot run %s: %v", id, strings.Join(command, " "), err)
	}
	return exitOK
}

// editorCommand is the editor to run, split into a program and its leading
// arguments — $VISUAL first, then $EDITOR, the order every tool uses.
//
// The split is on spaces, which is what makes `EDITOR="code -w"` work. It is not
// shell parsing: an editor whose own path holds a space is not supported here,
// because handing the value to a shell to find out would make every $EDITOR a
// script cria executes.
func editorCommand() []string {
	for _, name := range []string{visualEnv, editorEnv} {
		if command := strings.Fields(os.Getenv(name)); len(command) > 0 {
			return command
		}
	}
	return nil
}

// entryFile is the file one id names, whether that entry loaded or was refused.
func entryFile(tree *config.Tree, id string) (string, bool) {
	for _, entry := range tree.Entries {
		if entry.ID == id {
			return entry.Path, true
		}
	}
	for _, broken := range tree.Broken {
		if broken.ID == id {
			return broken.Path, true
		}
	}
	return "", false
}

// refuseUnknownFile answers an id that names no file at all — the answer to
// "what did I mistype". Every id in the tree is listed, refused ones included:
// they are editable, and they are the likeliest thing someone is reaching for.
func (a *app) refuseUnknownFile(tree *config.Tree, id string) int {
	ids := entryIDs(tree)
	for _, broken := range tree.Broken {
		ids = append(ids, broken.ID)
	}
	if len(ids) == 0 {
		return a.fail("edit %s: no entry named %q, and %s holds no entry file at all; `cria docs` prints the schema for writing one",
			id, id, tree.Root)
	}
	return a.fail("edit %s: no entry named %q in %s; available entries: %s",
		id, id, tree.Root, strings.Join(ids, ", "))
}
