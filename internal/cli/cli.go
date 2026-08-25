// Package cli routes a command line to the subcommand that serves it and renders
// what the subsystems answer (docs/specs/CLI.md). It owns three things —
// parsing, ordering and output — and no judgement: which entries exist is
// config's, which tools may be used is tools', and everything about a server's
// life is serve's.
//
// The subsystems reach this package through one struct, so an invocation is a
// value: where its output goes, how it loads the config tree, how it checks the
// tools, how it reaches the state directory and what has been picked in it.
// Dispatch builds the real one;
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
	"cria/internal/picks"
	"cria/internal/procs"
	"cria/internal/selfupdate"
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

// subcommands is the whole v1 surface (docs/specs/CLI.md), in the order the help
// page presents it: the server lifecycle, then the config tree, then the two
// generators, then the binary's own upkeep. Bare `cria` opens the TUI instead
// of naming a subcommand.
var subcommands = []string{"start", "stop", "status", "validate", "bench", "list", "new", "edit", "docs", "wired-limit", "update"}

// The flags the surface has, all booleans (docs/specs/CLI.md). `cria new` takes
// two because they are peers: the backend it scaffolds can be named either way,
// and neither is a special case.
const (
	waitFlag  = "--wait"
	jsonFlag  = "--json"
	pathsFlag = "--paths"
	llamaFlag = "--llama"
	mlxFlag   = "--mlx"
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
	Start(entry config.Entry, selection config.Selection, report tools.Report) (serve.Record, error)
	Stop(record serve.Record) error
	List() (serve.Listing, error)
	Running(entryID string) (serve.Server, bool, error)
	Snapshot(record serve.Record) (serve.Status, error)
	Snapshots() (serve.StatusListing, error)
	PortUse(port int) (serve.PortUse, error)
	ListensOn(record serve.Record) (bool, []int, error)
	Warm(record serve.Record) error
	Bench(record serve.Record, spec serve.BenchSpec, report func(serve.BenchStep)) serve.BenchResult

	// The five a validation is composed of (validate.go): who holds the target's
	// port, whether that server may be stopped right now, the stop that keeps
	// what puts it back, that restore, and the one completion that proves a
	// server serves.
	Displaced(entry config.Entry) (serve.Displacement, error)
	Generating(record serve.Record) serve.Generation
	Displace(holder serve.Record) (serve.Record, error)
	Restore(tree *config.Tree, held serve.Record, report tools.Report) (serve.Record, error)
	Prove(record serve.Record) error
}

// updater is the part of selfupdate the update subcommand drives — named on the
// consumer's side for the same reason servers is: the component tests exercise
// the whole subcommand with no GitHub and no binary to replace.
type updater interface {
	LatestVersion() (string, error)
	Install(version string) (string, error)
}

// app is one invocation: its two output streams and the subsystems it drives.
type app struct {
	out io.Writer
	err io.Writer

	tree       func() (*config.Tree, error)       // the config tree, read on demand
	tools      func(config.Settings) tools.Report // the host's managed tools
	servers    func() (servers, error)            // the state directory and the process table
	picksStore func() (picks.Picks, error)        // what was picked before this invocation
	memoryMB   func() (int, error)                // the machine's memory; refuses off macOS
	updater    func() updater                     // GitHub's releases and the binary on disk

	// tui is the program bare `cria` opens. It arrives as a function rather
	// than an import so this package stays a command-line router: routing to
	// the TUI is a decision here, but nothing about a terminal program is
	// (CODING-RULES §7 — main.go does the wiring).
	tui func() error

	// interrupts arms the Ctrl-C watch a command that must not be left halfway
	// runs under, and hands back the two halves of it: whether the operator has
	// asked cria to stop, and the disarm (validate.go). It is a field so a
	// component test can interrupt a wait without signalling the process the
	// suite runs in.
	interrupts func() (interrupted func() bool, disarm func())

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
// config.Root puts it, the host's tools, a manager over the state directory and
// the picks stored beside it.
func newApp(tui func() error) *app {
	return &app{
		out:            os.Stdout,
		err:            os.Stderr,
		tree:           loadTree,
		tools:          tools.Check,
		servers:        newManager,
		picksStore:     loadPicks,
		memoryMB:       physicalMemoryMB,
		updater:        func() updater { return selfupdate.New() },
		tui:            tui,
		interrupts:     watchInterrupt,
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
	case "--help", "-h", "help":
		a.printf("%s", helpPage)
		return exitOK
	case "start":
		return a.start(args[1:])
	case "stop":
		return a.stop(args[1:])
	case "status":
		return a.status(args[1:])
	case "validate":
		return a.validate(args[1:])
	case "bench":
		return a.bench(args[1:])
	case "list":
		return a.list(args[1:])
	case "new":
		return a.newEntry(args[1:])
	case "edit":
		return a.edit(args[1:])
	case "docs":
		a.printf("%s", config.Docs())
		return exitOK
	case "wired-limit":
		return a.wiredLimit(args[1:])
	case "update":
		return a.update(args[1:], version)
	default:
		return a.usage("unknown subcommand %q; valid subcommands are: %s; `cria --help` says what each one does",
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

// loadPicks reads the picks store out of the state root, where it sits beside
// the records: picks are cria's own state, not config (docs/specs/CONFIG.md,
// Choices). It is read on demand for the same reason the manager is built on
// demand — the subcommands that never look at an entry's choices have no
// business resolving a state directory.
func loadPicks() (picks.Picks, error) {
	root, err := serve.Root()
	if err != nil {
		return picks.Picks{}, err
	}
	return picks.Load(root)
}

// storedPicks is what was picked before this invocation. Reading is all the CLI
// ever does with the store: a `choice=option` argument overrides one start and
// is gone after it, and the picker is the only thing that writes
// (docs/specs/CONFIG.md, Choices).
//
// A store cria could not read is an aside, never a refusal. The file is cria's
// own state and it always answers with usable picks — the config defaults —
// so the launch that follows is a launch, and the note is what says why it may
// not be the one that was picked last.
func (a *app) storedPicks() picks.Picks {
	stored, err := a.picksStore()
	if err != nil {
		a.note("%v", err)
	}
	return stored
}

// printf writes what the invocation was asked for: the answer, the progress
// lines while waiting, and the JSON document. Everything a script reads goes
// here.
func (a *app) printf(format string, args ...any) {
	fmt.Fprintf(a.out, format, args...)
}

// note writes an aside: something true and worth knowing that is not the answer
// the invocation asked for. It goes to stderr so stdout stays the answer alone —
// a script reading `cria start --wait` reads its verdict from the exit code and
// its output from stdout, and neither changes because cria had something to add.
func (a *app) note(format string, args ...any) {
	fmt.Fprintf(a.err, "note: "+format+"\n", args...)
}

// waiting writes what cria is doing while a caller waits on it. It is not an
// aside about the answer but the reason the answer is taking this long, so it
// carries no "note:" — and it goes to stderr for the same reason a note does.
func (a *app) waiting(format string, args ...any) {
	fmt.Fprintf(a.err, format+"\n", args...)
}

// fail reports that the asked-for thing is not true or not done, and carries the
// exit code that says so (docs/specs/CLI.md). Every message it prints names the
// failing thing and what clears it.
func (a *app) fail(format string, args ...any) int {
	return a.failWith(exitFailure, format, args...)
}

// usage reports a command line cria cannot route: a flag it does not know, or
// the wrong number of arguments.
func (a *app) usage(format string, args ...any) int {
	return a.failWith(exitUsage, format, args...)
}

// failWith writes one refusal line and hands back the code that says what kind
// of refusal it was. Every non-zero exit cria takes reports its reason this way:
// one line on stderr, under the program's own name, naming the failing thing and
// what clears it — validate's further codes (validate.go) included.
func (a *app) failWith(code int, format string, args ...any) int {
	fmt.Fprintf(a.err, "cria "+format+"\n", args...)
	return code
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
