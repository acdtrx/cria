package serve

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cria/internal/config"
	"cria/internal/hubapi"
	"cria/internal/hubcache"
	"cria/internal/procs"
)

// Phase is where a server is in the life docs/specs/SERVE.md lays out:
// starting → (downloading →) running, with unhealthy and exited as the failure
// states. It is derived at every observation and never stored — nothing on disk
// holds a phase, because a phase is only true at the moment it was read.
type Phase string

const (
	PhaseStarting    Phase = "starting"
	PhaseDownloading Phase = "downloading"
	PhaseRunning     Phase = "running"
	PhaseUnhealthy   Phase = "unhealthy"
	PhaseExited      Phase = "exited"
)

// Status is one server as cria sees it right now: what it wrote down at launch,
// the phase this moment's observation derives, what the process costs, what the
// health endpoint answered, and — while downloading — how far the model has
// got. One shape for both renderers: the TUI's status box and `cria status
// --json` show these same facts (docs/specs/TUI.md, docs/specs/CLI.md).
type Status struct {
	Record
	Phase    Phase         `json:"phase"`
	Uptime   time.Duration `json:"uptime"`   // since LaunchedAt; zero for a record whose process is gone
	Stats    procs.Stats   `json:"stats"`    // what the pid costs; zero when the process table had nothing to say
	Health   Health        `json:"health"`   // this observation's probe; zero for an exited record, which is never probed
	Progress Progress      `json:"progress"` // the download, while there is one
}

// Progress is how far a downloading server has got: the bytes of its model that
// are on disk against what the Hub says the whole thing comes to. The server
// does the fetching; cria only watches the cache (docs/specs/SERVE.md).
//
// It is filled while the phase is downloading and zero otherwise: a finished
// download has no progress to show, and a server that never had to download has
// none to begin with.
type Progress struct {
	Bytes  int64  `json:"bytes"`            // on disk now, unfinished downloads included
	Total  int64  `json:"total,omitempty"`  // what it comes to when complete; meaningful only when Known
	Known  bool   `json:"known"`            // the Hub answered, so there is a percentage to render
	Reason string `json:"reason,omitempty"` // why there is no total; empty exactly when Known
}

// StatusListing is every record cria holds, observed. It mirrors Listing: the
// servers, and the record files cria refused — a broken record names a pid cria
// started, so it is reported rather than dropped (docs/specs/SERVE.md).
type StatusListing struct {
	Servers []Status
	Broken  []BrokenRecord
}

// cacheReader is one read of the hub cache, and hubReader one question to the
// Hub about a model's size. Both are injected for the same reason the process
// table is: the phase rules have to be exercised without a cache on disk or a
// network in reach.
type (
	cacheReader func() (*hubcache.Cache, error)
	hubReader   func(config.Entry) hubapi.Total
)

// Snapshot observes one record: whether it is still the process cria launched,
// whether its port answers, and — only when neither has settled the phase — how
// much of its model the cache holds.
func (m *Manager) Snapshot(record Record) (Status, error) {
	live, err := m.Live(record)
	if err != nil {
		return Status{}, err
	}
	return m.observe(Server{Record: record, Live: live}, m.cacheOnce())
}

// Snapshots observes every record cria holds — how a fresh invocation renders
// the servers a previous one started.
func (m *Manager) Snapshots() (StatusListing, error) {
	listing, err := m.List()
	if err != nil {
		return StatusListing{}, err
	}

	cache := m.cacheOnce()
	statuses := make([]Status, 0, len(listing.Servers))
	for _, server := range listing.Servers {
		status, err := m.observe(server, cache)
		if err != nil {
			return StatusListing{}, err
		}
		statuses = append(statuses, status)
	}
	return StatusListing{Servers: statuses, Broken: listing.Broken}, nil
}

// observe is one look at one server, in the order that asks for the least: the
// record's own facts, then the process table, then one probe, and the cache
// only if the probe left the phase open. Everything an observation costs is
// paid once per snapshot — no retry, no second opinion.
func (m *Manager) observe(server Server, cache cacheReader) (Status, error) {
	status := Status{Record: server.Record, Phase: PhaseExited}
	if !server.Live {
		// An exited record is a crash report, not a server: nothing is probed,
		// nothing is walked, and what cria wrote down at launch — with the log
		// path — is the whole answer (docs/specs/SERVE.md).
		return status, nil
	}

	status.Uptime = time.Since(server.LaunchedAt)
	stats, found, err := m.host.Stats(server.PID)
	if err != nil {
		return Status{}, err
	}
	if found {
		status.Stats = stats
	}

	status.Health = m.probe(probeURL(server.Record))
	seen := observation{
		live:     true,
		green:    status.Health.Green,
		wasGreen: m.rememberGreen(server.EntryID, server.PID, status.Health.Green),
	}

	// The cache is read only when the probe has settled nothing. Green and
	// was-green both outrank the disk in derivePhase, so a walk skipped here
	// could not have changed the phase — and the walk sizes every blob in the
	// cache, far too much work to do on a refresh tick that does not need it.
	//
	// The Hub is asked before the phase rather than after it, because which
	// blobs the model is made of today is one of the phase's own inputs. It is
	// asked only where its answer can matter: something unfinished is sitting in
	// the entry's repo, or the model is not all there. A start whose model is
	// whole and whose repo is quiet asks nothing — the ordinary case, and the
	// one that must not put a network round trip in front of the display.
	var presence hubcache.Presence
	var total hubapi.Total
	var landed int64
	if !seen.green && !seen.wasGreen {
		read, err := m.walk(server.Record, cache)
		if err != nil {
			return Status{}, err
		}
		presence = read.Presence(server.entry())
		seen.cached = presence.Cached

		repo, held := read.Repo(server.Repo)
		unfinished := held && len(repo.Partials) > 0
		if unfinished || !seen.cached {
			total = m.modelTotal(server.Record)
		}
		if unfinished {
			landed, seen.fetching = fetching(repo, total)
		}
	}

	status.Phase = derivePhase(seen)
	if status.Phase == PhaseDownloading {
		// The numerator is whatever this phase was derived from. A model the
		// cache holds nothing of is measured by what has arrived for it; a model
		// that reads complete and is being fetched again is measured by the
		// copy that is landing, because the bytes already on disk are the old
		// copy's and counting them would show a download that starts finished.
		bytes := presence.Bytes
		if seen.fetching {
			bytes = landed
		}
		status.Progress = Progress{Bytes: bytes, Total: total.Bytes, Known: total.Known, Reason: total.Reason}
	}
	return status, nil
}

// observation is one round of looking at a server: everything the phase is
// derived from, and nothing else. Keeping it a value is what makes the
// derivation a pure function — the whole phase matrix is then a table, with no
// process, no port and no cache in it.
type observation struct {
	live     bool // the record still names the process cria launched
	green    bool // the documented health endpoint answered 2xx just now
	wasGreen bool // this server has answered green at least once during this cria invocation
	cached   bool // the entry's model is fully present in the cache
	fetching bool // the cache is receiving the entry's model right now, whatever it already holds of it
}

// derivePhase is the whole phase rule (docs/specs/SERVE.md), and its order is
// the judgement.
//
// A green probe is proof of serving, whatever else is true. Below it sits the
// line between the two red states: a server that has answered before and has
// stopped is unhealthy, while one that has never answered is still on its way
// up — llama-server replies 503 at /health for as long as it is loading a
// model, and calling that unhealthy would make every normal start look like a
// failure. Only then does the cache matter, and it answers in two ways: a
// server whose model is still arriving is downloading, and so is one whose
// model reads complete while the copy the Hub publishes today is arriving
// anyway — a provider who republishes a quant makes the server fetch gigabytes
// before it loads a thing, with nothing in its log to say so. Cached and quiet
// is what starting means.
func derivePhase(seen observation) Phase {
	switch {
	case !seen.live:
		return PhaseExited
	case seen.green:
		return PhaseRunning
	case seen.wasGreen:
		return PhaseUnhealthy
	case seen.fetching:
		return PhaseDownloading
	case !seen.cached:
		return PhaseDownloading
	default:
		return PhaseStarting
	}
}

// rememberGreen records a green answer and reports whether this server has ever
// given one. It is the memory the starting/unhealthy line needs, and nothing on
// disk holds it: a state record says what was launched, not what has answered
// since.
//
// So it is display state, kept for as long as this cria invocation runs and
// never persisted. A fresh cria that finds a server already red therefore reads
// it as starting until it goes green once — the honest answer, since that cria
// has never seen this server serve.
//
// The memory is per entry and keyed by the pid that gave the answer: restarting
// an entry is a different process, and it gets its own startup window rather
// than inheriting the last one's verdict.
func (m *Manager) rememberGreen(entryID string, pid int, green bool) bool {
	m.memory.Lock()
	defer m.memory.Unlock()

	if green {
		m.greenPID[entryID] = pid
		return true
	}
	return m.greenPID[entryID] == pid
}

// walk reads the cache one record's phase is derived against.
//
// A walk that fails is not degraded into "nothing left to download": that
// answer is plausible and wrong, and it would show a downloading server as
// starting forever. The failure travels instead, naming the cache that could
// not be read — the same reason List refuses to report servers as exited when
// the process table cannot be read (CODING-RULES §4).
func (m *Manager) walk(record Record, cache cacheReader) (*hubcache.Cache, error) {
	read, err := cache()
	if err != nil {
		return nil, fmt.Errorf("cannot tell whether %s still has its model to download: %w", record.EntryID, err)
	}
	return read, nil
}

// fetching reports whether the unfinished downloads in a repo are this model's,
// and how many of the model's bytes have landed.
//
// The precise question is which blobs the Hub publishes for this model today:
// the cache names an unfinished download after the file it is becoming, so a
// partial carrying one of those names is this model arriving and a partial
// carrying any other name is another file of the same repo arriving.
//
// Without a total there are no names to match, and the repository is the whole
// answer that is left: anything unfinished in it is taken as this model's,
// because cria launched the server that is fetching into it. That is the same
// judgement llamaPresence already makes for a quant with nothing on disk yet,
// and it keeps the re-download visible on a host that cannot reach the Hub —
// coarser, never louder: the phase it produces is the one the bytes justify.
func fetching(repo *hubcache.Repo, total hubapi.Total) (int64, bool) {
	if !total.Known {
		return repo.PartialBytes, true
	}
	return repo.Fetching(total.Blobs)
}

// cacheOnce is a cache read taken at most once, however many records ask for
// it: one walk per Snapshots call, and none at all when every record's phase
// was settled by its probe.
func (m *Manager) cacheOnce() cacheReader { return sync.OnceValues(m.cache) }

// modelTotal answers what one record's model comes to when complete, asking the
// Hub at most once per model per cria invocation. The expected size does not
// change while a download runs, and this sits on the TUI's refresh tick: asking
// again every second would put a network round trip between the display and
// every frame.
//
// The key is the model rather than the entry, because the model is what was
// sized: two entries serving one model ask once, and an entry pointed at
// another model gets its own answer instead of the previous one's.
//
// A Hub that could not answer is remembered too. Asking again on the next tick
// would put a five-second timeout in front of the display for as long as the
// host is offline, and the cost of remembering is the one it already carries:
// bytes without a percentage until cria is next run.
func (m *Manager) modelTotal(record Record) hubapi.Total {
	key := fmt.Sprintf("%s %s:%s", record.Backend, record.Repo, record.Quant)

	m.memory.Lock()
	total, asked := m.totals[key]
	m.memory.Unlock()
	if asked {
		return total
	}

	total = m.hub(record.entry())

	m.memory.Lock()
	m.totals[key] = total
	m.memory.Unlock()
	return total
}

// entry is the record as the cache and the Hub take it. Both read exactly what
// identifies the model, which is why a record — self-contained by design — can
// answer for an entry whose config file has since changed or gone.
func (r Record) entry() config.Entry {
	return config.Entry{ID: r.EntryID, Backend: r.Backend, Repo: r.Repo, Quant: r.Quant}
}

// readCache is the real cache reader: the hub cache where huggingface_hub
// itself puts it, walked whole (internal/hubcache).
func readCache() (*hubcache.Cache, error) {
	root, err := hubcache.Root()
	if err != nil {
		return nil, err
	}
	return hubcache.Read(root)
}

// hubTotals is the real Hub reader: one client, carrying whatever credential
// this host holds. It bounds its own answers, so an unreachable Hub costs a
// snapshot its percentage and nothing else (internal/hubapi).
//
// The client is built on the first question rather than with the Manager: it
// resolves the host's Hugging Face token, and a cria that only stops a server
// has no business reading a credential.
func hubTotals() hubReader {
	client := sync.OnceValue(hubapi.New)
	return func(entry config.Entry) hubapi.Total { return client().Total(context.Background(), entry) }
}
