// Package cli routes a command line to the subcommand that serves it. It owns the
// dispatch table only — each subcommand's behavior lives in the subsystem package
// that implements it.
package cli

import (
	"fmt"
	"os"
	"strings"

	"cria/internal/config"
)

// Process exit codes. A subcommand returns exitFailure when the thing it was asked
// for is not true or not done (docs/specs/CLI.md); exitUsage is reserved for a
// command line cria cannot route at all.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// subcommands is the whole v1 surface (docs/specs/CLI.md). Bare `cria` opens the
// TUI instead of naming a subcommand.
var subcommands = []string{"start", "stop", "status", "docs"}

// Dispatch runs the invocation described by args — os.Args without the program
// name — and returns the process exit code.
func Dispatch(args []string, version string) int {
	if len(args) == 0 {
		return runTUI()
	}

	switch args[0] {
	case "--version":
		fmt.Printf("cria %s\n", version)
		return exitOK
	case "start":
		return notImplemented("start")
	case "stop":
		return notImplemented("stop")
	case "status":
		return notImplemented("status")
	case "docs":
		fmt.Print(config.Docs())
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "cria: unknown subcommand %q; valid subcommands are: %s\n",
			args[0], strings.Join(subcommands, ", "))
		return exitUsage
	}
}

// runTUI is the placeholder for the bubbletea app that bare `cria` opens.
func runTUI() int {
	fmt.Fprintln(os.Stderr, "cria: the TUI is not implemented yet")
	return exitFailure
}

func notImplemented(subcommand string) int {
	fmt.Fprintf(os.Stderr, "cria %s: not implemented yet\n", subcommand)
	return exitFailure
}
