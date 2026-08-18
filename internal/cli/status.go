package cli

import (
	"encoding/json"
	"strings"
	"time"

	"cria/internal/serve"
)

// status runs `cria status [--json]`.
//
// Both faces report the same facts — the ones the TUI's status box shows
// (docs/specs/SERVE.md) — for every record cria holds, live or exited, plus the
// record files it could not read. The exit code is the question a script asks
// with it: zero when at least one server is live, non-zero when none is
// (docs/specs/CLI.md).
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

	if asJSON {
		document, err := json.MarshalIndent(statusDocumentOf(listing), "", "  ")
		if err != nil {
			return a.fail("status: cannot encode the status document: %v", err)
		}
		a.printf("%s\n", document)
	} else {
		a.reportStatus(listing)
	}

	for _, server := range listing.Servers {
		if server.Phase != serve.PhaseExited {
			return exitOK
		}
	}
	return exitFailure
}

// reportStatus writes the human report: one block per record, then the records
// cria refused.
func (a *app) reportStatus(listing serve.StatusListing) {
	if len(listing.Servers) == 0 && len(listing.Broken) == 0 {
		a.printf("no servers: cria holds no state records; start one with `cria start <id>`\n")
		return
	}

	for i, server := range listing.Servers {
		if i > 0 {
			a.printf("\n")
		}
		a.reportServer(server)
	}
	for _, broken := range listing.Broken {
		if len(listing.Servers) > 0 {
			a.printf("\n")
		}
		a.printf("%s  unreadable record  %s\n", broken.EntryID, broken.Path)
		a.printf("  %v\n", broken.Err)
		a.printf("  delete that file once the pid it names is gone\n")
	}
}

// reportServer is one server's block. An exited record is a crash report rather
// than a server, so nothing is claimed about what it costs or what it answers —
// it carries its launch, its command and its log, which is what a crash is read
// from (docs/specs/SERVE.md).
func (a *app) reportServer(status serve.Status) {
	a.printf("%s  %s  %s  %s\n", status.EntryID, status.Phase, status.Backend, modelReference(status.Record))

	if status.Phase == serve.PhaseExited {
		a.printf("  pid %d on %s is gone; launched %s\n",
			status.PID, address(status.Record), status.LaunchedAt.Format(time.DateTime))
	} else {
		a.printf("  pid %d on %s, up %s\n", status.PID, address(status.Record), status.Uptime.Round(time.Second))
		if status.Stats.RSSBytes > 0 || status.Stats.CPUPercent > 0 {
			a.printf("  memory %s, cpu %.1f%%\n", formatBytes(status.Stats.RSSBytes), status.Stats.CPUPercent)
		}
		a.printf("  health %s: %s\n", status.Health.URL, status.Health.Detail)
		if status.Phase == serve.PhaseDownloading {
			a.printf("  downloading %s\n", downloaded(status.Progress))
		}
	}

	a.printf("  command %s\n", strings.Join(status.Command, " "))
	a.printf("  log %s\n", status.LogPath)
}

// modelReference is the model a record serves, spelled the way the entry named
// it: the repo, qualified by its quantization when there is one.
func modelReference(record serve.Record) string {
	if record.Quant == "" {
		return record.Repo
	}
	return record.Repo + ":" + record.Quant
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
type statusDocument struct {
	Servers []serverDocument `json:"servers"`
	Broken  []brokenDocument `json:"broken"`
}

type serverDocument struct {
	Entry         string           `json:"entry"`
	Backend       string           `json:"backend"`
	Repo          string           `json:"repo"`
	Quant         string           `json:"quant"`
	Host          string           `json:"host"`
	Port          int              `json:"port"`
	PID           int              `json:"pid"`
	Phase         string           `json:"phase"`
	UptimeSeconds float64          `json:"uptime_seconds"`
	RSSBytes      int64            `json:"rss_bytes"`
	CPUPercent    float64          `json:"cpu_percent"`
	Health        healthDocument   `json:"health"`
	Progress      progressDocument `json:"progress"`
	Command       []string         `json:"command"`
	Log           string           `json:"log"`
	LaunchedAt    time.Time        `json:"launched_at"`
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

// statusDocumentOf projects one listing into the document. Both lists are
// allocated empty rather than left nil: `servers: []` is what a script iterates,
// and `servers: null` is what it crashes on.
func statusDocumentOf(listing serve.StatusListing) statusDocument {
	document := statusDocument{
		Servers: make([]serverDocument, 0, len(listing.Servers)),
		Broken:  make([]brokenDocument, 0, len(listing.Broken)),
	}
	for _, status := range listing.Servers {
		document.Servers = append(document.Servers, serverDocument{
			Entry:         status.EntryID,
			Backend:       string(status.Backend),
			Repo:          status.Repo,
			Quant:         status.Quant,
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
