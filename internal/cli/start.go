package cli

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/picks"
	"cria/internal/serve"
)

// startSynopsis is the command line every start refusal points back at
// (docs/specs/CLI.md).
const startSynopsis = "cria start <id> [choice=option ...] [" + waitFlag + "]"

// start runs `cria start <id> [choice=option ...] [--wait]`: the command line,
// down to the entry it names and the picks it launches under.
func (a *app) start(args []string) int {
	rest, wait, unknown := splitFlag(args, waitFlag)
	if unknown != "" {
		return a.usage("start: unknown flag %s; usage: %s", unknown, startSynopsis)
	}
	ids, explicit, err := splitPicks(rest)
	if err != nil {
		return a.usage("start: %v; usage: %s", err, startSynopsis)
	}
	if len(ids) == 0 {
		return a.usage("start: no entry named; usage: %s", startSynopsis)
	}
	if len(ids) > 1 {
		return a.usage("start: one entry at a time (got %s); usage: %s",
			strings.Join(ids, ", "), startSynopsis)
	}
	id := ids[0]

	tree, err := a.tree()
	if err != nil {
		return a.fail("start %s: %v", id, err)
	}
	entry, found := entryNamed(tree, id)
	if !found {
		return a.refuseUnknownEntry(tree, id)
	}

	// The three layers a launch is decided by (docs/specs/CONFIG.md, Choices):
	// the entry's config defaults, under the picks the store holds, under the
	// ones typed here. The store is read and left alone — a pick on the command
	// line is one-shot, so an agent's experiment never changes what the next bare
	// start launches.
	//
	// A flat entry has nothing to pick, so nothing to look up: its launch is what
	// it always was, and a store cria cannot read is not its problem. A pick typed
	// against it is still refused, by the merge, naming what the entry has.
	var stored config.Selection
	if len(entry.Choices) > 0 {
		stored = a.storedPicks()[entry.ID]
	}
	selection, err := picks.Merge(entry, stored, explicit)
	if err != nil {
		return a.fail("start %s: %v", id, err)
	}
	return a.startEntry(tree, entry, selection, wait)
}

// splitPicks separates the entry id on a start's command line from the picks
// beside it.
//
// `=` is what tells them apart: an id cannot contain one (docs/specs/CONFIG.md,
// the id charset), so `quant=q4` is never an id and `qwen` is never a pick, and
// the two may be typed in any order like the flag they share the line with. The
// split is at the *first* `=`: a choice's name cannot hold one either, so a
// second `=` belongs to the option and is answered where the option is — by
// name, against the options the entry has.
//
// Both halves must be named. `=q4` and `quant=` are neither an id nor a pick,
// and reading them as one would launch something nobody asked for.
func splitPicks(args []string) ([]string, config.Selection, error) {
	var ids []string
	explicit := config.Selection{}

	for _, arg := range args {
		choice, option, isPick := strings.Cut(arg, "=")
		if !isPick {
			ids = append(ids, arg)
			continue
		}
		if choice == "" || option == "" {
			return nil, nil, fmt.Errorf("%q is not a pick; a pick is choice=option, both named", arg)
		}
		if picked, twice := explicit[choice]; twice {
			return nil, nil, fmt.Errorf("choice %q is picked twice (%s and %s); one option per choice",
				choice, picked, option)
		}
		explicit[choice] = option
	}
	return ids, explicit, nil
}

// startEntry is the start sequence itself, from a named entry and a settled
// selection.
//
// The order is the one docs/specs/SERVE.md settles, and every step of it refuses
// before anything is spawned: the picks have to resolve, the entry must not
// already be running, its backend's tool has to be usable, and its port has to be
// free. Only then is a server started — and only then can --wait have something to
// watch.
func (a *app) startEntry(tree *config.Tree, entry config.Entry, selection config.Selection, wait bool) int {
	id := entry.ID

	// Resolution sits with entry validation, ahead of both gates
	// (docs/specs/SERVE.md, Start 1): a pick that names nothing has to be
	// answered with the valid names rather than with a busy port or a missing
	// tool. serve resolves again when it composes the command — the read is pure,
	// and having it there is what makes every frontend refuse identically.
	if _, err := config.Resolve(entry, selection); err != nil {
		return a.fail("start %s: %v", id, err)
	}

	manager, err := a.servers()
	if err != nil {
		return a.fail("start %s: %v", id, err)
	}

	// Already running comes first (docs/specs/SERVE.md): it costs a record read,
	// where the gates behind it exec programs — and an entry that is already up
	// deserves that answer, not a tool or port complaint.
	if held, running, err := manager.Running(entry.ID); err != nil {
		return a.fail("start %s: %v", id, err)
	} else if running {
		return a.fail("start %s: %s is already running as pid %d on port %d; stop it first",
			id, held.EntryID, held.PID, held.Port)
	}

	// The tool gate before the port check: a host without llama-server has to
	// hear about llama-server, not about a busy port (docs/specs/SERVE.md).
	report := a.tools(tree.Settings)
	if _, err := serve.LaunchTool(entry.Backend, report); err != nil {
		return a.fail("start %s: %v", id, err)
	}

	// The port has to be free before anything is spawned (docs/specs/SERVE.md).
	use, err := manager.PortUse(entry.Port)
	if err != nil {
		return a.fail("start %s: %v", id, err)
	}
	if refusal := portRefusal(entry, use); refusal != "" {
		return a.fail("start %s: %s", id, refusal)
	}

	record, err := manager.Start(entry, selection, report)
	if err != nil {
		return a.fail("start %s: %v", id, err)
	}

	a.printf("started %s as pid %d on %s\n", record.EntryID, record.PID, address(record))
	a.printf("  command %s\n", strings.Join(record.Command, " "))
	a.printf("  log %s\n", record.LogPath)
	if !wait {
		a.printf("  not serving yet; `cria status` reports its phase, `cria start %s %s` blocks until it does\n", id, waitFlag)
		a.noteLazyLoad(record)
		return exitOK
	}
	return a.await(manager, record)
}

// entryNamed finds the entry an argument names among the ones that loaded.
func entryNamed(tree *config.Tree, id string) (config.Entry, bool) {
	for _, entry := range tree.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return config.Entry{}, false
}

// refuseUnknownEntry answers an id that named no usable entry. An id whose file
// is there but broken gets its own failure back — the author needs the offending
// key, not a list of the entries that happen to parse (docs/specs/CONFIG.md).
func (a *app) refuseUnknownEntry(tree *config.Tree, id string) int {
	for _, broken := range tree.Broken {
		if broken.ID == id {
			return a.fail("start %s: %s: %v; fix that file and start again", id, broken.Path, broken.Err)
		}
	}
	if len(tree.Entries) == 0 {
		return a.fail("start %s: no entry named %q, and %s holds no usable entry; write one and run `cria docs` for the schema",
			id, id, tree.Root)
	}
	return a.fail("start %s: no entry named %q in %s; available entries: %s",
		id, id, tree.Root, strings.Join(entryIDs(tree), ", "))
}

// entryIDs lists the ids a start could name, in the order config.Load returns
// them — the order they appear in the tree.
func entryIDs(tree *config.Tree) []string {
	ids := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

// portRefusal is why an entry cannot have the port it asked for, or the empty
// string when it can.
//
// The two refusals differ in what the caller can do about them. A server cria
// started is stopped by naming its entry — that is the whole fix. Anything else
// is foreign: cria reports its pid, what it runs and where it runs, and leaves
// it alone, because the kill is the TUI's offer, never the CLI's
// (docs/specs/SERVE.md).
func portRefusal(entry config.Entry, use serve.PortUse) string {
	if held := use.Managed; held != nil {
		if held.EntryID == entry.ID {
			return fmt.Sprintf("%s is already running as pid %d on port %d; stop it first",
				entry.ID, held.PID, held.Port)
		}
		return fmt.Sprintf("port %d is already serving %s (pid %d); stop %s first",
			entry.Port, held.EntryID, held.PID, held.EntryID)
	}
	if len(use.Holders) == 0 {
		return ""
	}

	var message strings.Builder
	fmt.Fprintf(&message, "port %d is held by a process cria did not start:", entry.Port)
	for _, holder := range use.Holders {
		fmt.Fprintf(&message, "\n  pid %d  %s", holder.PID, orUnknown(holder.Command, "command unreadable"))
		fmt.Fprintf(&message, "\n          working directory %s", orUnknown(holder.WorkingDir, "unreadable"))
	}
	fmt.Fprintf(&message, "\nstop that process, or give %s a port of its own in %s, and start again",
		entry.ID, entry.Path)
	return message.String()
}

// orUnknown keeps a refusal readable when one of a holder's two details could
// not be read: the pid is the part that matters, and it is already printed.
func orUnknown(value, missing string) string {
	if value == "" {
		return "(" + missing + ")"
	}
	return value
}

// await is --wait: watch a start until its phase settles (docs/specs/CLI.md).
// Running is the answer the caller asked for; exited and unhealthy are failures
// with the log path attached, since the log is the only crash evidence cria has
// (docs/specs/SERVE.md).
//
// The window is the budget the wait is bound by, and the phase chooses it: a
// start that has to fetch its model is bound by the network, one that does not
// is bound by how long a cached model takes to load. Once a download has been
// seen the larger budget stays — a fetch that finishes and hands over to a slow
// load must not be cut off by the smaller one.
func (a *app) await(manager servers, record serve.Record) int {
	began := time.Now()
	window := a.startWindow
	var lastProgress time.Time

	for {
		status, err := manager.Snapshot(record)
		if err != nil {
			return a.fail("start %s: cannot tell whether it is serving: %v", record.EntryID, err)
		}

		switch status.Phase {
		case serve.PhaseRunning:
			if refusal := a.listenerRefusal(manager, record); refusal != "" {
				return a.fail("start %s: %s", record.EntryID, refusal)
			}
			if refusal := a.loadRefusal(manager, record, began); refusal != "" {
				return a.fail("start %s: %s", record.EntryID, refusal)
			}
			a.printf("%s is running after %s: %s answered %s\n",
				record.EntryID, since(began), status.Health.URL, status.Health.Detail)
			return exitOK
		case serve.PhaseExited:
			return a.fail("start %s: it exited after %s without serving; its log is the crash report: %s",
				record.EntryID, since(began), record.LogPath)
		case serve.PhaseUnhealthy:
			return a.fail("start %s: it stopped answering after %s (%s said %s); log: %s",
				record.EntryID, since(began), status.Health.URL, status.Health.Detail, record.LogPath)
		case serve.PhaseDownloading:
			window = a.downloadWindow
			if lastProgress.IsZero() || time.Since(lastProgress) >= a.progressEvery {
				a.printf("  downloading %s\n", downloaded(status.Progress))
				lastProgress = time.Now()
			}
		}

		if time.Since(began) >= window {
			return a.fail("start %s: it is still %s after %s; log: %s",
				record.EntryID, status.Phase, window, record.LogPath)
		}
		// Sleeping between observations, not around a race: the phase lives in
		// another process and there is no event to wait on (CODING-RULES §6).
		time.Sleep(a.poll)
	}
}

// listenerRefusal is why a green port is not proof that the server cria started
// is the one answering it, or the empty string when nothing contradicts the
// health signal.
//
// It is asked only once the probe has gone green, which is what makes a mismatch
// meaningful: a server still coming up holds no port yet and is nobody's
// contradiction, while a green port held by another pid means cria was about to
// report a start that never happened — the answering server is someone else's
// (docs/specs/SERVE.md).
//
// The health signal is primary and this is corroboration, so a port lookup cria
// could not run leaves the verdict standing: `lsof` degrades attribution only
// (docs/specs/TOOLS.md), and failing a start because a diagnostic could not run
// would refuse servers that are demonstrably serving.
func (a *app) listenerRefusal(manager servers, record serve.Record) string {
	listening, pids, err := manager.ListensOn(record)
	if err != nil {
		a.note("cannot confirm that pid %d is what answers on port %d: %v", record.PID, record.Port, err)
		return ""
	}
	if listening {
		return ""
	}
	return fmt.Sprintf("port :%d answers, but the listener is not the server cria started (pid %d, listener(s) %s)",
		record.Port, record.PID, listenerPIDs(pids))
}

// listenerPIDs spells who holds a port, for the refusal that has to name them. A
// port with nothing listening at the moment cria looked is said so rather than
// left blank.
func listenerPIDs(pids []int) string {
	if len(pids) == 0 {
		return "none"
	}
	numbers := make([]string, 0, len(pids))
	for _, pid := range pids {
		numbers = append(numbers, strconv.Itoa(pid))
	}
	return strings.Join(numbers, ", ")
}

// loadRefusal is why a green server is not yet the ready server --wait promises,
// or the empty string when it is.
//
// mlx_lm.server answers /v1/models before it has read a single weight and loads
// them on the first completion instead (docs/specs/SERVE.md), so a wait that
// stopped at green would hand back a server whose first real request pays
// minutes. --wait means ready: cria sends that first completion itself, and the
// verdict waits for the answer.
//
// A completion that does not come back fails the start. The server may well
// still be up — the load can outlast even the budget serve gives it, and nothing
// about this says the process died — so the refusal reports what happened and
// where the log is rather than claiming the server is gone.
func (a *app) loadRefusal(manager servers, record serve.Record, began time.Time) string {
	if !serve.LoadsLazily(record.Backend) {
		return ""
	}

	// Said before the wait rather than after it, and on stderr: this is what
	// cria is doing while a caller sits there, not the answer they asked for.
	a.waiting("loading model weights (mlx loads lazily; this can take a while)…")
	err := manager.Warm(record)
	if err == nil {
		return ""
	}
	return fmt.Sprintf("it answered after %s but %v; the server may still be up — its log is %s",
		since(began), err, record.LogPath)
}

// noteLazyLoad is the same fact on the path that does not wait: the server is
// started, and the weights are not loaded until something asks it for a
// completion. The note names the one command that loads them now.
func (a *app) noteLazyLoad(record serve.Record) {
	if !serve.LoadsLazily(record.Backend) {
		return
	}
	a.note("mlx loads model weights on the first request; `cria start %s %s` loads them now",
		record.EntryID, waitFlag)
}

// address is where a server listens, as its record spells it.
func address(record serve.Record) string {
	return net.JoinHostPort(record.Host, strconv.Itoa(record.Port))
}

// since is how long a wait has taken, at the resolution a person reads.
func since(began time.Time) time.Duration { return time.Since(began).Round(time.Second) }
