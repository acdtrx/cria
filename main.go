// Command cria serves local LLMs from a config tree: bare `cria` opens the TUI,
// subcommands drive the same subsystems scriptably (docs/specs/CLI.md).
package main

import (
	"os"

	"cria/internal/cli"
)

// version identifies this binary; it is what `cria --version` prints and what the
// TUI header shows.
const version = "0.1.0-dev"

func main() {
	os.Exit(cli.Dispatch(os.Args[1:], version))
}
