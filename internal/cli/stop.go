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
// question — an entry with no record was never started, or was already stopped —
// and a record whose process has gone is cleared by the same command, which is
// what a stop of an exited entry asks for (docs/specs/SERVE.md).
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
	return a.fail("stop %s: cria has no server record for %s; nothing to stop%s", id, id, whatIsRecorded(listing))
}

// whatIsRecorded names the entries cria does hold records for, for the refusal
// that found none under the id it was given. A stop cria refuses is exactly the
// moment the caller needs to know which servers cria is actually tracking — and
// that a server it never started is not one it can stop.
func whatIsRecorded(listing serve.Listing) string {
	ids := make([]string, 0, len(listing.Servers)+len(listing.Broken))
	for _, server := range listing.Servers {
		ids = append(ids, server.EntryID)
	}
	for _, broken := range listing.Broken {
		ids = append(ids, broken.EntryID)
	}
	if len(ids) == 0 {
		return " (cria holds no server records at all; a server it did not start is not cria's to stop)"
	}
	return fmt.Sprintf(" (cria holds records for: %s)", strings.Join(ids, ", "))
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
//
// The second line reports the judgement rather than a claim about the world:
// what cria knows is that the pid is no longer the process it launched, and
// saying only "it exited" would be a lie in the one case where it is not — a
// record written without an identity, which matches nothing however healthy the
// server is.
func (a *app) stopServer(manager servers, server serve.Server) int {
	if err := manager.Stop(server.Record); err != nil {
		return a.fail("stop %s: %v", server.EntryID, err)
	}
	if server.Live {
		a.printf("stopped %s (pid %d on %s)\n", server.EntryID, server.PID, address(server.Record))
		return exitOK
	}
	a.printf("%s had already exited (pid %d is no longer the process cria launched); removed its record (log: %s)\n",
		server.EntryID, server.PID, server.LogPath)
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
	return fmt.Sprintf(" (%s record(s) remain; `cria status` shows them, `cria stop <id>` clears one)", strings.Join(held, " and "))
}
