// Command cria serves local LLMs from a config tree: bare `cria` opens the TUI,
// subcommands drive the same subsystems scriptably (docs/specs/CLI.md).
package main

import (
	"fmt"
	"os"

	"cria/internal/cli"
	"cria/internal/config"
	"cria/internal/tui"
)

// version identifies this binary; it is what `cria --version` prints and what the
// TUI header shows. Release builds inject the tag
// (-ldflags "-X main.version=<tag>", .github/workflows/release.yml); a local
// build honestly says dev rather than claiming a release it may have drifted
// from.
var version = "dev"

func main() {
	scaffoldConfigTree()
	os.Exit(cli.Dispatch(os.Args[1:], version, tui.Run))
}

// scaffoldConfigTree gives every invocation — TUI or subcommand — a config tree to
// read, creating only what is missing (docs/specs/CONFIG.md). A tree cria cannot
// create is reported rather than fatal: `cria docs` still prints the schema, which
// is exactly what someone facing an unwritable config directory needs next, and
// the subcommands that do need the tree fail naming the same path.
func scaffoldConfigTree() {
	root, err := config.Root()
	if err == nil {
		err = config.Scaffold(root)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "cria: %v\n", err)
	}
}
