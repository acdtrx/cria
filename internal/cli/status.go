package cli

import (
	"encoding/json"
	"strings"
	"time"

	"cria/internal/format"
	"cria/internal/serve"
)

// status runs `cria status [--json]`.
//
// Both faces report the same facts — the ones the TUI's status box shows
// (docs/specs/SERVE.md) — for every record cria holds, live or exited, plus the
// record files it could not read. The exit code is the question a script asks
// with it: zero when at least one server is live, non-zero when none is
// (docs/specs/CLI.md).
//
// This host's router is one of those servers, and it is reported here for that
// reason alone: cria started it and holds its record. It reads differently
// because it is a different thing — no entry, no model reference, and the models
// it holds are its own to report — so it is a block of its own rather than a row
// among the entries' (docs/specs/SERVE.md, The router).
func (a *app) status(args []string) int {
	rest, asJSON, unknown := splitFlag(args, jsonFlag)
	if unknown != "" {
		return a.usage("status: unknown flag %s; usage: cria status [%s]", unknown, jsonFlag)
	}
	if len(rest) > 0 {
		return a.usage("status: takes no arguments (got %s); usage: cria status [%s]",
			strings.Join(rest, ", "), jsonFlag)
	}

	manager, err := a.servers()
	if err != nil {
		return a.fail("status: %v", err)
	}
	listing, err := manager.Snapshots()
	if err != nil {
		return a.fail("status: %v", err)
	}
	router, err := observeRouter(manager)
	if err != nil {
		return a.fail("status: %v", err)
	}

	if asJSON {
		document, err := json.MarshalIndent(statusDocumentOf(listing, router), "", "  ")
		if err != nil {
			return a.fail("status: cannot encode the status document: %v", err)
		}
		a.printf("%s\n", document)
	} else {
		a.reportStatus(listing, router)
	}

	for _, server := range listing.Servers {
		if server.Phase != serve.PhaseExited {
			return exitOK
		}
	}
	if router.Found && router.Status.Phase != serve.PhaseExited {
		return exitOK
	}
	return exitFailure
}

// routerObservation is this host's router as a status reports it: the
// supervisor's own observation and what it says it holds. Found is false when
// cria holds no record of a router here — the ordinary state of a host that runs
// one server per entry.
type routerObservation struct {
	Found    bool
	Status   serve.Status
	Children serve.RouterChildren
}

// observeRouter takes that observation, in the order `cria router status` takes
// it (router.go): the record, then one look at the process and its port, and the
// models only from a router that is still there — an exited record is a crash
// report, and there is nothing to ask it.
func observeRouter(manager servers) (routerObservation, error) {
	server, found, err := manager.RouterServer()
	if err != nil || !found {
		return routerObservation{}, err
	}
	status, err := manager.RouterSnapshot(server.Record)
	if err != nil {
		return routerObservation{}, err
	}
	if status.Phase == serve.PhaseExited {
		return routerObservation{Found: true, Status: status}, nil
	}
	return routerObservation{Found: true, Status: status, Children: manager.RouterChildren(status.Record)}, nil
}

// reportStatus writes the human report: one block per record, then the records
// cria refused, then the router this host runs.
func (a *app) reportStatus(listing serve.StatusListing, router routerObservation) {
	if len(listing.Servers) == 0 && len(listing.Broken) == 0 && !router.Found {
		a.printf("no servers: cria holds no state records; start one with `cria start <id>`\n")
		return
	}

	blocks := 0
	for _, server := range listing.Servers {
		if blocks > 0 {
			a.printf("\n")
		}
		a.reportServer(server)
		blocks++
	}
	for _, broken := range listing.Broken {
		if blocks > 0 {
			a.printf("\n")
		}
		a.printf("%s  unreadable record  %s\n", broken.EntryID, broken.Path)
		a.printf("  %v\n", broken.Err)
		a.printf("  delete that file once the pid it names is gone\n")
		blocks++
	}
	if router.Found {
		if blocks > 0 {
			a.printf("\n")
		}
		a.reportRouter(router.Status)
		if router.Status.Phase != serve.PhaseExited {
			a.reportRouterChildren(router.Children)
		}
	}
}

// reportServer is one server's block. An exited record is a crash report rather
// than a server, so nothing is claimed about what it costs or what it answers —
// it carries its launch, its command and its log, which is what a crash is read
// from (docs/specs/SERVE.md).
func (a *app) reportServer(status serve.Status) {
	a.printf("%s  %s  %s  %s\n", status.EntryID, status.Phase, status.Backend,
		format.HubReference(status.Repo, status.Quant))

	if status.Phase == serve.PhaseExited {
		a.printf("  pid %d on %s is gone; launched %s\n",
			status.PID, address(status.Record), status.LaunchedAt.Format(time.DateTime))
	} else {
		a.printf("  pid %d on %s, up %s\n", status.PID, address(status.Record), status.Uptime.Round(time.Second))
		if status.Stats.RSSBytes > 0 || status.Stats.CPUPercent > 0 {
			a.printf("  memory %s, cpu %.1f%%\n", format.Bytes(status.Stats.RSSBytes), status.Stats.CPUPercent)
		}
		a.printf("  health %s: %s\n", status.Health.URL, status.Health.Detail)
		if status.Phase == serve.PhaseDownloading {
			a.printf("  downloading %s\n", downloaded(status.Progress))
		}
	}

	// The picks sit against the command they composed: a combination is what the
	// server is, and the line below is what that came to (docs/specs/SERVE.md).
	// A flat entry picked nothing, so there is no line — its block is the one it
	// always was.
	if len(status.Selection) > 0 {
		a.printf("  picks %s\n", format.Picks(status.Selection))
	}
	a.printf("  command %s\n", strings.Join(status.Command, " "))
	a.printf("  log %s\n", status.LogPath)
}

// The `cria status --json` document is a projection, not a marshalled snapshot.
// A record also carries the process identity `ps` handed back at launch — cria's
// own bookkeeping, meaningless to a script and unstable in shape — and a
// document built by marshalling the snapshot struct would publish it and would
// change every time the internals do.
//
// So the field names below are the machine contract (docs/specs/CLI.md): every
// one of them is always present, so a script never has to tell "absent" from
// "zero", and none of them is dropped when empty.
//
// `picks` is the one field that is absent rather than empty, because absent is
// what it means: a flat entry has no combination at all, where `{}` would read
// as an entry with axes and nothing picked on them — a state that cannot exist.
// The record spells it the same way (docs/specs/SERVE.md).
//
// `router` is the one field whose value may be null, and null is what it means:
// this host has no router record at all. A router is one per host rather than a
// list, so an empty list would deny the shape, and a zeroed object would read as
// a router with pid 0 (docs/specs/SERVE.md, The router).
type statusDocument struct {
	Servers []serverDocument `json:"servers"`
	Broken  []brokenDocument `json:"broken"`
	Router  *routerDocument  `json:"router"` // null when cria holds no record of a router here
}

type serverDocument struct {
	Entry         string            `json:"entry"`
	Backend       string            `json:"backend"`
	Repo          string            `json:"repo"`
	Quant         string            `json:"quant"`
	Picks         map[string]string `json:"picks,omitempty"` // choice → picked option; absent for a flat entry
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	PID           int               `json:"pid"`
	Phase         string            `json:"phase"`
	UptimeSeconds float64           `json:"uptime_seconds"`
	RSSBytes      int64             `json:"rss_bytes"`
	CPUPercent    float64           `json:"cpu_percent"`
	Health        healthDocument    `json:"health"`
	Progress      progressDocument  `json:"progress"`
	Command       []string          `json:"command"`
	Log           string            `json:"log"`
	LaunchedAt    time.Time         `json:"launched_at"`
}

type healthDocument struct {
	URL    string `json:"url"`
	Green  bool   `json:"green"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

type progressDocument struct {
	Bytes  int64  `json:"bytes"`
	Total  int64  `json:"total"`
	Known  bool   `json:"known"`
	Reason string `json:"reason"`
}

type brokenDocument struct {
	Entry string `json:"entry"`
	Path  string `json:"path"`
	Error string `json:"error"`
}

// The router's own document. It carries the same server facts an entry's does,
// with the preset it serves from where a model reference stands — a record says
// what its server was started to serve, and the router's answer is a file
// (docs/specs/SERVE.md).
//
// `models` is what the running router said it holds, and `models_detail` is why
// it said nothing — a router still coming up has nothing to answer with yet, and
// that is a fact to report rather than an error. Each model carries the router's
// own word for what it is doing; `phase` is cria's vocabulary for that word and
// is empty where cria's has none, which is normal for half the states a router
// publishes.
type routerDocument struct {
	Entry         string          `json:"entry"`
	Backend       string          `json:"backend"`
	Preset        string          `json:"preset"`
	Host          string          `json:"host"`
	Port          int             `json:"port"`
	PID           int             `json:"pid"`
	Phase         string          `json:"phase"`
	UptimeSeconds float64         `json:"uptime_seconds"`
	RSSBytes      int64           `json:"rss_bytes"`
	CPUPercent    float64         `json:"cpu_percent"`
	Health        healthDocument  `json:"health"`
	Models        []modelDocument `json:"models"`
	ModelsDetail  string          `json:"models_detail"`
	Command       []string        `json:"command"`
	Log           string          `json:"log"`
	LaunchedAt    time.Time       `json:"launched_at"`
}

type modelDocument struct {
	Model   string   `json:"model"`
	Aliases []string `json:"aliases"`
	State   string   `json:"state"`
	Phase   string   `json:"phase"`
}

// statusDocumentOf projects one listing into the document. Both lists are
// allocated empty rather than left nil: `servers: []` is what a script iterates,
// and `servers: null` is what it crashes on.
func statusDocumentOf(listing serve.StatusListing, router routerObservation) statusDocument {
	document := statusDocument{
		Servers: make([]serverDocument, 0, len(listing.Servers)),
		Broken:  make([]brokenDocument, 0, len(listing.Broken)),
		Router:  routerDocumentOf(router),
	}
	for _, status := range listing.Servers {
		document.Servers = append(document.Servers, serverDocument{
			Entry:         status.EntryID,
			Backend:       string(status.Backend),
			Repo:          status.Repo,
			Quant:         status.Quant,
			Picks:         status.Selection,
			Host:          status.Host,
			Port:          status.Port,
			PID:           status.PID,
			Phase:         string(status.Phase),
			UptimeSeconds: status.Uptime.Round(time.Millisecond).Seconds(),
			RSSBytes:      status.Stats.RSSBytes,
			CPUPercent:    status.Stats.CPUPercent,
			Health: healthDocument{
				URL:    status.Health.URL,
				Green:  status.Health.Green,
				Status: status.Health.Status,
				Detail: status.Health.Detail,
			},
			Progress: progressDocument{
				Bytes:  status.Progress.Bytes,
				Total:  status.Progress.Total,
				Known:  status.Progress.Known,
				Reason: status.Progress.Reason,
			},
			Command:    status.Command,
			Log:        status.LogPath,
			LaunchedAt: status.LaunchedAt,
		})
	}
	for _, broken := range listing.Broken {
		document.Broken = append(document.Broken, brokenDocument{
			Entry: broken.EntryID,
			Path:  broken.Path,
			Error: broken.Err.Error(),
		})
	}
	return document
}

// routerDocumentOf projects the router observation, or nothing at all when this
// host holds no router record. The model list is allocated empty for the same
// reason the server list is: a script iterates it, whatever the router answered.
func routerDocumentOf(router routerObservation) *routerDocument {
	if !router.Found {
		return nil
	}

	status := router.Status
	document := routerDocument{
		Entry:         status.EntryID,
		Backend:       string(status.Backend),
		Preset:        status.Preset,
		Host:          status.Host,
		Port:          status.Port,
		PID:           status.PID,
		Phase:         string(status.Phase),
		UptimeSeconds: status.Uptime.Round(time.Millisecond).Seconds(),
		RSSBytes:      status.Stats.RSSBytes,
		CPUPercent:    status.Stats.CPUPercent,
		Health: healthDocument{
			URL:    status.Health.URL,
			Green:  status.Health.Green,
			Status: status.Health.Status,
			Detail: status.Health.Detail,
		},
		Models:       make([]modelDocument, 0, len(router.Children.Children)),
		ModelsDetail: router.Children.Detail,
		Command:      status.Command,
		Log:          status.LogPath,
		LaunchedAt:   status.LaunchedAt,
	}
	for _, child := range router.Children.Children {
		aliases := child.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		document.Models = append(document.Models, modelDocument{
			Model:   child.Model,
			Aliases: aliases,
			State:   child.State,
			Phase:   string(child.Phase),
		})
	}
	return &document
}
