package cli

import (
	"fmt"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/format"
	"cria/internal/serve"
)

// routerSynopsis is the command line every refusal of this subcommand points
// back at (docs/specs/CLI.md).
const routerSynopsis = "cria router [start|stop|status]"

// router runs `cria router <verb>`: the lifecycle of the one router process this
// host runs.
//
// It is a subcommand of its own rather than an id `cria start` takes, because the
// router is not an entry: nothing in models/ declares it, an entry could be named
// "router" without being it, and the verbs it grows next — which models it holds,
// and loading them — are about a process the whole tree shares
// (docs/specs/CLI.md). A bare `cria router` reports; the verbs that change the
// host are typed.
func (a *app) router(args []string) int {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return a.usage("router: unknown flag %s; usage: %s", arg, routerSynopsis)
		}
	}
	if len(args) > 1 {
		return a.usage("router: one verb at a time (got %s); usage: %s", strings.Join(args, ", "), routerSynopsis)
	}

	verb := "status"
	if len(args) == 1 {
		verb = args[0]
	}
	switch verb {
	case "start":
		return a.routerStart()
	case "stop":
		return a.routerStop()
	case "status":
		return a.routerStatus()
	default:
		return a.usage("router: no such verb %q; usage: %s", verb, routerSynopsis)
	}
}

// routerStart starts the router, in the order every start refuses in
// (docs/specs/SERVE.md): what the tree declares, then whether it is already
// running, then the tool, then the port — each answered before anything on the
// host has changed.
func (a *app) routerStart() int {
	tree, err := a.tree()
	if err != nil {
		return a.fail("router start: %v", err)
	}
	port, err := serve.RouterPort(tree.Router)
	if err != nil {
		return a.fail("router start: %v", err)
	}

	manager, err := a.servers()
	if err != nil {
		return a.fail("router start: %v", err)
	}
	if held, found, err := manager.RouterServer(); err != nil {
		return a.fail("router start: %v", err)
	} else if found && held.Live {
		return a.fail("router start: the router is already running as pid %d on %s; stop it first",
			held.PID, address(held.Record))
	}

	served, err := engine.For(config.BackendRouter)
	if err != nil {
		return a.fail("router start: %v", err)
	}
	report := a.tools(tree.Settings)
	if _, err := engine.LaunchTool(served, report); err != nil {
		return a.fail("router start: %v", err)
	}

	use, err := manager.PortUse(port)
	if err != nil {
		return a.fail("router start: %v", err)
	}
	if refusal := routerPortRefusal(tree.Router, port, use); refusal != "" {
		return a.fail("router start: %s", refusal)
	}

	record, err := manager.StartRouter(tree.Router, report)
	if err != nil {
		return a.fail("router start: %v", err)
	}

	a.printf("started the router as pid %d on %s\n", record.PID, address(record))
	a.printf("  preset %s\n", record.Preset)
	a.printf("  command %s\n", strings.Join(record.Command, " "))
	a.printf("  log %s\n", record.LogPath)
	a.printf("  not serving yet; `cria router status` reports its phase\n")
	return exitOK
}

// routerStop stops the router. A record whose process has already gone is
// cleared by the same command, which is what stopping an exited router asks for
// (docs/specs/SERVE.md).
func (a *app) routerStop() int {
	manager, err := a.servers()
	if err != nil {
		return a.fail("router stop: %v", err)
	}
	server, found, err := manager.RouterServer()
	if err != nil {
		return a.fail("router stop: %v", err)
	}
	if !found {
		return a.fail("router stop: cria has no record of a router on this host; nothing to stop")
	}
	if err := manager.StopRouter(server.Record); err != nil {
		return a.fail("router stop: %v", err)
	}

	if server.Live {
		a.printf("stopped the router (pid %d on %s)\n", server.PID, address(server.Record))
		return exitOK
	}
	a.printf("the router had already exited (pid %d is no longer the process cria launched); removed its record (log: %s)\n",
		server.PID, server.LogPath)
	return exitOK
}

// routerStatus reports what the router is doing right now, and exits by the same
// question `cria status` answers: zero while it is serving or on its way there,
// non-zero when there is none (docs/specs/CLI.md).
func (a *app) routerStatus() int {
	manager, err := a.servers()
	if err != nil {
		return a.fail("router status: %v", err)
	}
	server, found, err := manager.RouterServer()
	if err != nil {
		return a.fail("router status: %v", err)
	}
	if !found {
		a.printf("no router: cria holds no record of one on this host; start it with `cria router start`\n")
		return exitFailure
	}

	status, err := manager.RouterSnapshot(server.Record)
	if err != nil {
		return a.fail("router status: %v", err)
	}
	a.reportRouter(status)
	if status.Phase == serve.PhaseExited {
		return exitFailure
	}
	return exitOK
}

// reportRouter is the router's status block, in the shape one server's block has
// under `cria status` — with the preset it serves from where an entry's model
// reference stands, because that is what this server was started to serve.
func (a *app) reportRouter(status serve.Status) {
	a.printf("%s  %s  %s\n", status.EntryID, status.Phase, status.Backend)

	if status.Phase == serve.PhaseExited {
		a.printf("  pid %d on %s is gone; launched %s\n",
			status.PID, address(status.Record), status.LaunchedAt.Format(time.DateTime))
	} else {
		a.printf("  pid %d on %s, up %s\n", status.PID, address(status.Record), status.Uptime.Round(time.Second))
		if status.Stats.RSSBytes > 0 || status.Stats.CPUPercent > 0 {
			a.printf("  memory %s, cpu %.1f%%\n", format.Bytes(status.Stats.RSSBytes), status.Stats.CPUPercent)
		}
		a.printf("  health %s: %s\n", status.Health.URL, status.Health.Detail)
	}
	a.printf("  preset %s\n", status.Preset)
	a.printf("  command %s\n", strings.Join(status.Command, " "))
	a.printf("  log %s\n", status.LogPath)
}

// routerPortRefusal is why the router cannot have the port its engine file asked
// for, or the empty string when it can. The two refusals are the entries' own
// (docs/specs/SERVE.md): a server cria started is stopped by naming it, and
// anything else is reported and left alone.
func routerPortRefusal(router config.RouterConfig, port int, use serve.PortUse) string {
	if held := use.Managed; held != nil {
		return fmt.Sprintf("port %d is already serving %s (pid %d); stop %s first, or give the router a port of its own in %s",
			port, held.EntryID, held.PID, held.EntryID, router.Path)
	}
	if len(use.Holders) == 0 {
		return ""
	}

	var message strings.Builder
	fmt.Fprintf(&message, "port %d is held by a process cria did not start:", port)
	for _, holder := range use.Holders {
		fmt.Fprintf(&message, "\n  pid %d  %s", holder.PID, orUnknown(holder.Command, "command unreadable"))
		fmt.Fprintf(&message, "\n          working directory %s", orUnknown(holder.WorkingDir, "unreadable"))
	}
	fmt.Fprintf(&message, "\nstop that process, or give the router a port of its own in %s, and try again", router.Path)
	return message.String()
}
