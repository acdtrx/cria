package cli

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/serve"
)

// start runs `cria start <id> [--wait]`.
//
// The order is the one docs/specs/SERVE.md settles, and every step of it refuses
// before anything is spawned: the entry has to exist and parse, its backend's
// tool has to be usable, and its port has to be free. Only then is a server
// started — and only then can --wait have something to watch.
func (a *app) start(args []string) int {
	ids, wait, unknown := splitFlag(args, waitFlag)
	if unknown != "" {
		return a.usage("start: unknown flag %s; usage: cria start <id> [%s]", unknown, waitFlag)
	}
	if len(ids) == 0 {
		return a.usage("start: no entry named; usage: cria start <id> [%s]", waitFlag)
	}
	if len(ids) > 1 {
		return a.usage("start: one entry at a time (got %s); usage: cria start <id> [%s]",
			strings.Join(ids, ", "), waitFlag)
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

	record, err := manager.Start(entry, report)
	if err != nil {
		return a.fail("start %s: %v", id, err)
	}

	a.printf("started %s as pid %d on %s\n", record.EntryID, record.PID, address(record))
	a.printf("  command %s\n", strings.Join(record.Command, " "))
	a.printf("  log %s\n", record.LogPath)
	if !wait {
		a.printf("  not serving yet; `cria status` reports its phase, `cria start %s %s` blocks until it does\n", id, waitFlag)
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

// address is where a server listens, as its record spells it.
func address(record serve.Record) string {
	return net.JoinHostPort(record.Host, strconv.Itoa(record.Port))
}

// since is how long a wait has taken, at the resolution a person reads.
func since(began time.Time) time.Duration { return time.Since(began).Round(time.Second) }
