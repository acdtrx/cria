// Package cli routes a command line to the subcommand that serves it and renders
// what the subsystems answer (docs/specs/CLI.md). It owns three things —
// parsing, ordering and output — and no judgement: which entries exist is
// config's, which tools may be used is tools', and everything about a server's
// life is serve's.
//
// The subsystems reach this package through one struct, so an invocation is a
// value: where its output goes, how it loads the config tree, how it checks the
// tools, and how it reaches the state directory. Dispatch builds the real one;
// the component tests build one over fakes, which is what lets every refusal,
// exit code and rendered document be exercised with no server on the host.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/serve"
	"cria/internal/tools"
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

// The flags the surface has: one per subcommand, both booleans
// (docs/specs/CLI.md).
const (
	waitFlag = "--wait"
	jsonFlag = "--json"
)

// How `--wait` watches a start it has just triggered.
//
// The poll interval is what an observation costs, not what a person waits: each
// round is one health probe and one `ps`, and two seconds keeps the wait
// responsive without asking the host anything it has not had time to change.
//
// The two windows are budgets, chosen from what actually takes the time. A model
// already in the cache only has to be loaded and answered for, and a server that
// has not managed that in two minutes has a problem worth reporting rather than
// waiting out. A model that is still being fetched is bound by the network and
// by tens of gigabytes: half an hour is the window that lets a real download
// finish unattended, and the phase is what selects it — an entry never pays the
// download budget for a start that is not downloading.
const (
	waitPoll           = 2 * time.Second
	waitStartWindow    = 2 * time.Minute
	waitDownloadWindow = 30 * time.Minute

	// waitProgressEvery is how often a download prints where it has got to.
	// Plain lines on a scriptable path: coarse enough that half an hour of
	// waiting is a readable page rather than a thousand lines, frequent enough
	// to prove the fetch is moving.
	waitProgressEvery = 30 * time.Second
)

// servers is the part of serve.Manager the subcommands drive. Naming it on the
// consumer's side is what lets the component tests exercise the start refusals,
// stop's three no-argument cases and a whole --wait without a state directory,
// a listening port or a process on the host.
type servers interface {
	Start(entry config.Entry, report tools.Report) (serve.Record, error)
	Stop(record serve.Record) error
	List() (serve.Listing, error)
	Running(entryID string) (serve.Server, bool, error)
	Snapshot(record serve.Record) (serve.Status, error)
	Snapshots() (serve.StatusListing, error)
	PortUse(port int) (serve.PortUse, error)
}

// app is one invocation: its two output streams and the subsystems it drives.
type app struct {
	out io.Writer
	err io.Writer

	tree    func() (*config.Tree, error)       // the config tree, read on demand
	tools   func(config.Settings) tools.Report // the host's managed tools
	servers func() (servers, error)            // the state directory and the process table

	// tui is the program bare `cria` opens. It arrives as a function rather
	// than an import so this package stays a command-line router: routing to
	// the TUI is a decision here, but nothing about a terminal program is
	// (CODING-RULES §7 — main.go does the wiring).
	tui func() error

	// The --wait windows, held rather than read from the constants so a test can
	// drive a whole wait — including its timeout — without waiting one out.
	poll           time.Duration
	startWindow    time.Duration
	downloadWindow time.Duration
	progressEvery  time.Duration
}

// Dispatch runs the invocation described by args — os.Args without the program
// name — and returns the process exit code. tui is what bare `cria` opens.
func Dispatch(args []string, version string, tui func() error) int {
	return newApp(tui).run(args, version)
}

// newApp wires the real world: the process's own streams, the config tree where
// config.Root puts it, the host's tools, and a manager over the state directory.
func newApp(tui func() error) *app {
	return &app{
		out:            os.Stdout,
		err:            os.Stderr,
		tree:           loadTree,
		tools:          tools.Check,
		servers:        newManager,
		tui:            tui,
		poll:           waitPoll,
		startWindow:    waitStartWindow,
		downloadWindow: waitDownloadWindow,
		progressEvery:  waitProgressEvery,
	}
}

// run is Dispatch over one app.
func (a *app) run(args []string, version string) int {
	if len(args) == 0 {
		return a.runTUI()
	}

	switch args[0] {
	case "--version":
		a.printf("cria %s\n", version)
		return exitOK
	case "start":
		return a.start(args[1:])
	case "stop":
		return a.stop(args[1:])
	case "status":
		return a.status(args[1:])
	case "docs":
		a.printf("%s", config.Docs())
		return exitOK
	default:
		return a.usage("unknown subcommand %q; valid subcommands are: %s",
			args[0], strings.Join(subcommands, ", "))
	}
}

// runTUI opens the program bare `cria` names. A TUI that could not start says
// why on stderr and exits like any other refusal: the terminal is already back
// in the state it was handed over in, so there is nothing to render the failure
// into (docs/specs/CLI.md).
func (a *app) runTUI() int {
	if err := a.tui(); err != nil {
		return a.fail("%v", err)
	}
	return exitOK
}

// loadTree reads the config tree from its one location (docs/specs/CONFIG.md).
func loadTree() (*config.Tree, error) {
	root, err := config.Root()
	if err != nil {
		return nil, err
	}
	return config.Load(root)
}

// newManager builds the manager over the real state root and the real process
// table. It is called by the subcommands that need it rather than built with the
// app, because `cria docs` and `cria --version` have no business resolving a
// state directory — they answer on a host where one cannot be resolved at all
// (main.go).
func newManager() (servers, error) {
	root, err := serve.Root()
	if err != nil {
		return nil, err
	}
	return serve.New(root, procs.System{}), nil
}

// printf writes what the invocation was asked for: the answer, the progress
// lines while waiting, and the JSON document. Everything a script reads goes
// here.
func (a *app) printf(format string, args ...any) {
	fmt.Fprintf(a.out, format, args...)
}

// fail reports that the asked-for thing is not true or not done, and carries the
// exit code that says so (docs/specs/CLI.md). Every message it prints names the
// failing thing and what clears it.
func (a *app) fail(format string, args ...any) int {
	fmt.Fprintf(a.err, "cria "+format+"\n", args...)
	return exitFailure
}

// usage reports a command line cria cannot route: a flag it does not know, or
// the wrong number of arguments.
func (a *app) usage(format string, args ...any) int {
	fmt.Fprintf(a.err, "cria "+format+"\n", args...)
	return exitUsage
}

// splitFlag separates the one flag a subcommand takes from its arguments,
// accepting either order — `cria start --wait qwen` and `cria start qwen --wait`
// are the same invocation, and a scripted caller should not have to know which
// one cria prefers. unknown names the first argument that looks like a flag and
// is not this one; it is empty when every argument was routable.
func splitFlag(args []string, flag string) (rest []string, present bool, unknown string) {
	for _, arg := range args {
		switch {
		case arg == flag:
			present = true
		case strings.HasPrefix(arg, "-"):
			if unknown == "" {
				unknown = arg
			}
		default:
			rest = append(rest, arg)
		}
	}
	return rest, present, unknown
}
