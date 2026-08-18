package cli

import (
	"fmt"
	"strings"

	"cria/internal/serve"
)

// stop runs `cria stop [<id>]`.
//
// With no argument it stops the only running server; with several running the
// entry id is required, because guessing which one a script meant is the one
// mistake a stop cannot take back (docs/specs/SERVE.md).
func (a *app) stop(args []string) int {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return a.usage("stop: unknown flag %s; usage: cria stop [<id>]", arg)
		}
	}
	if len(args) > 1 {
		return a.usage("stop: one entry at a time (got %s); usage: cria stop [<id>]",
			strings.Join(args, ", "))
	}

	manager, err := a.servers()
	if err != nil {
		return a.fail("stop: %v", err)
	}
	listing, err := manager.List()
	if err != nil {
		return a.fail("stop: %v", err)
	}

	if len(args) == 1 {
		return a.stopNamed(manager, listing, args[0])
	}
	return a.stopTheOnlyServer(manager, listing)
}

// stopNamed stops the server one entry id names. The record is the whole
// question — an entry with no record was never started, or was already stopped.
func (a *app) stopNamed(manager servers, listing serve.Listing, id string) int {
	for _, server := range listing.Servers {
		if server.EntryID == id {
			return a.stopServer(manager, server)
		}
	}
	for _, broken := range listing.Broken {
		if broken.EntryID == id {
			return a.fail("stop %s: its state record %s cannot be read: %v; delete that file once the pid it names is gone",
				id, broken.Path, broken.Err)
		}
	}
	return a.fail("stop %s: cria has no server record for %s; nothing to stop", id, id)
}

// stopTheOnlyServer is `cria stop` with nothing named: it acts when exactly one
// server is running, and otherwise says which case this is.
func (a *app) stopTheOnlyServer(manager servers, listing serve.Listing) int {
	var live []serve.Server
	for _, server := range listing.Servers {
		if server.Live {
			live = append(live, server)
		}
	}

	switch len(live) {
	case 0:
		return a.fail("stop: nothing is running%s", whatElseIsRecorded(listing))
	case 1:
		return a.stopServer(manager, live[0])
	default:
		ids := make([]string, 0, len(live))
		for _, server := range live {
			ids = append(ids, server.EntryID)
		}
		return a.fail("stop: %d servers are running (%s); name the one to stop: cria stop <id>",
			len(live), strings.Join(ids, ", "))
	}
}

// stopServer performs the stop and reports what it did. A record whose process
// had already gone is not a failure: the record is removed, which is the state
// the caller asked for (docs/specs/SERVE.md).
func (a *app) stopServer(manager servers, server serve.Server) int {
	if err := manager.Stop(server.Record); err != nil {
		return a.fail("stop %s: %v", server.EntryID, err)
	}
	if server.Live {
		a.printf("stopped %s (pid %d on %s)\n", server.EntryID, server.PID, address(server.Record))
		return exitOK
	}
	a.printf("%s had already exited; removed its record (log: %s)\n", server.EntryID, server.LogPath)
	return exitOK
}

// whatElseIsRecorded names what the state directory still holds when nothing is
// running: crash reports and records cria could not read both live there, and
// "nothing is running" is a more useful answer with them counted.
func whatElseIsRecorded(listing serve.Listing) string {
	var held []string
	if exited := len(listing.Servers); exited > 0 {
		held = append(held, fmt.Sprintf("%d exited", exited))
	}
	if broken := len(listing.Broken); broken > 0 {
		held = append(held, fmt.Sprintf("%d unreadable", broken))
	}
	if len(held) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s record(s) remain; `cria status` shows them)", strings.Join(held, " and "))
}
