package cli

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/format"
	"cria/internal/picks"
	"cria/internal/serve"
)

// The command lines this subcommand refuses back at (docs/specs/CLI.md). The
// verbs split in two: the router's own lifecycle, and the models it holds — one
// process the whole tree shares, and which of the tree's entries reach clients
// through it.
const (
	routerSynopsis        = "cria router [status|start|stop|models|include|exclude|load|unload]"
	routerIncludeSynopsis = "cria router include <id> [choice=option ...]"
	routerExcludeSynopsis = "cria router exclude <id>"
	routerLoadSynopsis    = "cria router load <id>"
	routerUnloadSynopsis  = "cria router unload <id> [" + ignoreBusyFlag + "]"
)

// router runs `cria router <verb>`: the life of the one router process this host
// runs, and the models it serves.
//
// It is a subcommand of its own rather than an id `cria start` takes, because the
// router is not an entry: nothing in models/ declares it, an entry could be named
// "router" without being it, and which models it holds is a question about a
// process the whole tree shares (docs/specs/CLI.md). A bare `cria router`
// reports; the verbs that change the host are typed.
func (a *app) router(args []string) int {
	verb, rest := "status", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, rest = args[0], args[1:]
	}

	switch verb {
	case "status":
		if code, refused := a.refuseRouterArgs(verb, rest); refused {
			return code
		}
		return a.routerStatus()
	case "start":
		if code, refused := a.refuseRouterArgs(verb, rest); refused {
			return code
		}
		return a.routerStart()
	case "stop":
		if code, refused := a.refuseRouterArgs(verb, rest); refused {
			return code
		}
		return a.routerStop()
	case "models":
		if code, refused := a.refuseRouterArgs(verb, rest); refused {
			return code
		}
		return a.routerListModels()
	case "include":
		return a.routerInclude(rest)
	case "exclude":
		return a.routerExclude(rest)
	case "load":
		return a.routerLoadModel(rest)
	case "unload":
		return a.routerUnloadModel(rest)
	default:
		return a.usage("router: no such verb %q; usage: %s", verb, routerSynopsis)
	}
}

// refuseRouterArgs answers a verb that takes no arguments being given some.
func (a *app) refuseRouterArgs(verb string, args []string) (int, bool) {
	if len(args) == 0 {
		return exitOK, false
	}
	return a.usage("router %s: takes no arguments (got %s); usage: %s",
		verb, strings.Join(args, ", "), routerSynopsis), true
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

	record, models, err := manager.StartRouter(tree, report)
	if err != nil {
		return a.fail("router start: %v", err)
	}

	a.printf("started the router as pid %d on %s\n", record.PID, address(record))
	a.printf("  preset %s\n", record.Preset)
	a.printf("  command %s\n", strings.Join(record.Command, " "))
	a.printf("  log %s\n", record.LogPath)
	a.reportComposedModels(models)
	a.printf("  not serving yet; `cria router status` reports its phase\n")
	return exitOK
}

// reportComposedModels says what the preset a start just wrote carries, under
// the launch it belongs to: the models the router will serve, and the ones it
// will not with the reason each was left out. A model dropped silently would
// show up as a client's 404 hours later (docs/specs/SERVE.md).
func (a *app) reportComposedModels(models serve.RouterModels) {
	if len(models.Served) == 0 {
		a.printf("  no models: nothing is included yet; `%s` adds one\n", routerIncludeSynopsis)
	} else {
		a.printf("  serving %s\n", strings.Join(servedIDs(models), ", "))
	}
	for _, skipped := range models.Skipped {
		a.printf("  skipped %s: %s\n", skipped.ID, skipped.Reason)
	}
}

// servedIDs names the models a composition carries, in the order its sections
// were written.
func servedIDs(models serve.RouterModels) []string {
	ids := make([]string, 0, len(models.Served))
	for _, model := range models.Served {
		ids = append(ids, model.ID)
	}
	return ids
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
	a.reportRouterChildren(manager.RouterChildren(status.Record))
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

// reportRouterChildren is what the running router says about each model it
// holds: the name clients address it by, the router's own word for what it is
// doing, and the model reference it lists.
//
// The router's word is what is printed. cria's phases can say three of those
// states and not the other three (internal/serve, routerChildPhase), and a model
// nobody has asked for yet is neither starting nor exited — so the display shows
// the state that was published rather than a phase cria would have to invent.
func (a *app) reportRouterChildren(children serve.RouterChildren) {
	if children.Detail != "" {
		a.printf("  models: cria could not ask which ones it holds (%s)\n", children.Detail)
		return
	}
	if len(children.Children) == 0 {
		a.printf("  models: it holds none\n")
		return
	}

	rows := make([][]string, 0, len(children.Children))
	for _, child := range children.Children {
		rows = append(rows, []string{childName(child), child.State, child.Model})
	}
	a.printf("  models\n")
	for _, line := range aligned(rows) {
		a.printf("    %s\n", line)
	}
}

// childName is what a client sends to reach one of the router's models: the
// alias its preset section carries, which is the entry id cria included it under
// (STEP-4's ruling). A model the router found on its own has none, and answers
// to its reference.
func childName(child serve.RouterChild) string {
	if len(child.Aliases) == 0 {
		return "(no alias)"
	}
	return strings.Join(child.Aliases, ", ")
}

// routerListModels runs `cria router models`: which entries the router holds and
// what the next start would serve them as.
//
// It asks the store and the tree, never the router, so it answers the same
// whether or not one is running — what a start would compose. The exit code is
// about the read: a router holding nothing is a true answer to "which models",
// and a model that could not be composed is reported with its reason rather than
// costing the listing its code (the same rule `cria list` follows).
func (a *app) routerListModels() int {
	tree, err := a.tree()
	if err != nil {
		return a.fail("router models: %v", err)
	}
	manager, err := a.servers()
	if err != nil {
		return a.fail("router models: %v", err)
	}
	models, err := manager.RouterModels(tree)
	if err != nil {
		return a.fail("router models: %v", err)
	}

	if len(models.Served) == 0 && len(models.Skipped) == 0 {
		a.printf("the router holds no models; `%s` adds one\n", routerIncludeSynopsis)
		return exitOK
	}

	rows := make([][]string, 0, len(models.Served))
	for _, model := range models.Served {
		rows = append(rows, []string{model.ID, format.HubReference(model.Repo, model.Quant), picksLine(model.Selection)})
	}
	for _, line := range aligned(rows) {
		a.printf("%s\n", strings.TrimRight(line, " "))
	}
	for _, skipped := range models.Skipped {
		a.printf("%s  skipped: %s\n", skipped.ID, skipped.Reason)
	}
	return exitOK
}

// picksLine is the combination one model is held under, spelled the way the
// command line that sets it takes — `context=long quant=q6`. The order is by
// choice name: a stored selection is a map, and the entry's file order belongs
// to the entry, which this line is not showing. A flat entry has nothing here.
func picksLine(selection config.Selection) string {
	if len(selection) == 0 {
		return ""
	}
	picked := make([]string, 0, len(selection))
	for _, choice := range slices.Sorted(maps.Keys(selection)) {
		picked = append(picked, choice+"="+selection[choice])
	}
	return strings.Join(picked, " ")
}

// routerInclude runs `cria router include <id> [choice=option ...]`: hold one of
// the tree's entries under the router, in the combination named.
//
// Inclusion is state, edited like a pick (OVERVIEW ruling 2): the entry file
// says nothing about the router, and the same entry can be held here under a
// different combination than the one the llama engine starts it under. Running
// it again on an entry the router already holds changes that combination — one
// verb settles which models the router holds and what each is held as, rather
// than two ways to write one file.
func (a *app) routerInclude(args []string) int {
	ids, explicit, err := splitPicks(args)
	if err != nil {
		return a.usage("router include: %v; usage: %s", err, routerIncludeSynopsis)
	}
	if len(ids) != 1 {
		return a.usage("router include: one entry at a time (got %s); usage: %s",
			strings.Join(ids, ", "), routerIncludeSynopsis)
	}
	id := ids[0]

	tree, err := a.tree()
	if err != nil {
		return a.fail("router include %s: %v", id, err)
	}
	entry, found := entryNamed(tree, id)
	if !found {
		return a.fail("router include %s: %s", id, unknownEntry(tree, id))
	}
	if err := engine.RouterServes(entry.Backend); err != nil {
		return a.fail("router include %s: %v", id, err)
	}

	held, err := a.routerModels()
	if err != nil {
		return a.fail("router include %s: %v", id, err)
	}
	// The picks typed now over the ones it is already held under, over the
	// entry's config defaults — the layering every launch uses, with the result
	// stored because here the combination *is* the state (docs/specs/CONFIG.md,
	// Choices).
	selection, err := picks.Merge(entry, held.Picks(id), explicit)
	if err != nil {
		return a.fail("router include %s: %v", id, err)
	}

	already := held.Holds(id)
	held.Include(id, selection)
	if err := a.saveRouterModels(held); err != nil {
		return a.fail("router include %s: %v", id, err)
	}

	what := "included " + id + " in the router"
	if already {
		what = id + " was already in the router"
	}
	a.printf("%s: %s\n", what, format.HubReference(entry.Repo, entry.Quant))
	if line := picksLine(selection); line != "" {
		a.printf("  held under %s\n", line)
	}
	a.printf("  clients address it as %q; the router composes its preset at every start, so restart it to serve this change\n", id)
	return exitOK
}

// routerExclude runs `cria router exclude <id>`: stop holding one entry under
// the router.
//
// It never reads the config tree. An entry whose file was renamed or deleted is
// exactly the one an operator needs to drop, and refusing to exclude what the
// tree no longer declares would leave that inclusion with no way out.
func (a *app) routerExclude(args []string) int {
	if len(args) != 1 {
		return a.usage("router exclude: one entry at a time (got %s); usage: %s",
			strings.Join(args, ", "), routerExcludeSynopsis)
	}
	id := args[0]

	held, err := a.routerModels()
	if err != nil {
		return a.fail("router exclude %s: %v", id, err)
	}
	if !held.Exclude(id) {
		return a.fail("router exclude %s: the router does not hold %q; `cria router models` lists what it holds", id, id)
	}
	if err := a.saveRouterModels(held); err != nil {
		return a.fail("router exclude %s: %v", id, err)
	}

	a.printf("excluded %s from the router\n", id)
	a.printf("  the router composes its preset at every start, so restart it to stop serving it\n")
	return exitOK
}

// routerLoadModel runs `cria router load <id>`: make the router hold one of its
// models in memory now.
//
// The router loads a model on the first request that names it, which is what
// clients rely on. This verb exists because the mechanism must be invocable on
// its own (CODING-RULES §7) — a model wanted resident before the first request
// is a thing to ask for, not a thing to arrange by sending a fake completion.
func (a *app) routerLoadModel(args []string) int {
	manager, record, id, code := a.routerModelTarget(args, "load", routerLoadSynopsis)
	if code != exitOK {
		return code
	}
	if err := manager.RouterLoad(record, id); err != nil {
		return a.fail("router load %s: %v", id, err)
	}
	a.printf("loaded %s\n", id)
	return exitOK
}

// routerUnloadModel runs `cria router unload <id> [--ignore-busy]`: let one of
// the router's models go, freeing what it holds.
//
// A model answering a request right now is refused, the way a validation refuses
// to displace a busy server (validate.go): the unload would cut that answer off,
// and the decision belongs to the human whose request it is. The override is the
// same one, spelled the same way.
func (a *app) routerUnloadModel(args []string) int {
	rest, ignoreBusy, unknown := splitFlag(args, ignoreBusyFlag)
	if unknown != "" {
		return a.usage("router unload: unknown flag %s; usage: %s", unknown, routerUnloadSynopsis)
	}

	manager, record, id, code := a.routerModelTarget(rest, "unload", routerUnloadSynopsis)
	if code != exitOK {
		return code
	}

	generation := manager.RouterGenerating(record, id)
	switch generation.Busy {
	case serve.BusyGenerating:
		if !ignoreBusy {
			return a.fail("router unload %s: it is answering a request right now, and unloading it would cut that answer off; let it finish, or ask again once it has", id)
		}
		a.note("%s is answering a request right now; %s was given, so cria unloads it mid-answer", id, ignoreBusyFlag)
	case serve.BusyUnverifiable:
		a.note("cria cannot tell whether %s is generating right now (%s); unloading it anyway, so a request in flight would die with it",
			id, generation.Detail)
	}

	if err := manager.RouterUnload(record, id); err != nil {
		return a.fail("router unload %s: %v", id, err)
	}
	a.printf("unloaded %s\n", id)
	return exitOK
}

// routerModelTarget is what the two per-model verbs share: the running router,
// and one of the models it actually holds.
//
// The name is checked against the router's own listing before anything is sent.
// That is what makes `?model=` addressing honest — cria only ever names a model
// the router has just said it holds — and it is the difference between "include
// it and restart" and a bare 404 from somebody else's endpoint.
func (a *app) routerModelTarget(args []string, verb, synopsis string) (servers, serve.Record, string, int) {
	if len(args) != 1 {
		return nil, serve.Record{}, "", a.usage("router %s: one model at a time (got %s); usage: %s",
			verb, strings.Join(args, ", "), synopsis)
	}
	id := args[0]

	manager, err := a.servers()
	if err != nil {
		return nil, serve.Record{}, id, a.fail("router %s %s: %v", verb, id, err)
	}
	server, found, err := manager.RouterServer()
	if err != nil {
		return nil, serve.Record{}, id, a.fail("router %s %s: %v", verb, id, err)
	}
	if !found || !server.Live {
		return nil, serve.Record{}, id, a.fail("router %s %s: no router is running on this host; start it with `cria router start`", verb, id)
	}

	children := manager.RouterChildren(server.Record)
	if children.Detail != "" {
		return nil, serve.Record{}, id, a.fail("router %s %s: cria cannot tell which models the router holds (%s)", verb, id, children.Detail)
	}
	if _, holds := children.Child(id); !holds {
		return nil, serve.Record{}, id, a.fail("router %s %s: the router does not hold %q; it holds %s",
			verb, id, id, heldNames(children))
	}
	return manager, server.Record, id, exitOK
}

// heldNames lists what a running router holds, for the refusal that has to say
// what could have been named instead.
func heldNames(children serve.RouterChildren) string {
	if len(children.Children) == 0 {
		return "nothing; include an entry and restart it"
	}
	names := make([]string, 0, len(children.Children))
	for _, child := range children.Children {
		names = append(names, childName(child))
	}
	return strings.Join(names, ", ")
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
