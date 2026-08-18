package serve

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/hubapi"
	"cria/internal/hubcache"
	"cria/internal/procs"
)

// fakeProbe stands in for the HTTP probe: it answers whatever the test set it
// to, and records every URL it was asked about — an observation that probes a
// server that has exited is a bug this catches.
type fakeProbe struct {
	health Health
	asked  []string
}

func (p *fakeProbe) answer(url string) Health {
	p.asked = append(p.asked, url)
	health := p.health
	health.URL = url
	return health
}

// The three answers a probe gets from a real server: the port is not open yet,
// llama-server is loading its model, and the model is loaded.
func refused() Health { return Health{Detail: "connection refused"} }
func loading() Health { return Health{Status: 503, Detail: "503 Service Unavailable"} }
func serving() Health { return Health{Green: true, Status: 200, Detail: "200 OK"} }

// fakeCache is the hub cache a test hands over, and a count of how often it was
// walked: the walk sizes every blob in the cache, so how many times a snapshot
// takes it is part of the contract.
type fakeCache struct {
	cache *hubcache.Cache
	err   error
	walks int
}

func (c *fakeCache) read() (*hubcache.Cache, error) {
	c.walks++
	return c.cache, c.err
}

// fakeHubTotals is what the Hub says a model comes to, and every model it was
// asked about.
type fakeHubTotals struct {
	total hubapi.Total
	asked []string
}

func (h *fakeHubTotals) ask(entry config.Entry) hubapi.Total {
	h.asked = append(h.asked, entry.Repo+":"+entry.Quant)
	return h.total
}

// cacheOf is a hub cache holding these repos and nothing else.
func cacheOf(repos ...hubcache.Repo) *hubcache.Cache {
	return &hubcache.Cache{Root: "/tmp/hub", Repos: repos}
}

// cachedRepo is a repo whose quant is whole: starting the entry that serves it
// downloads nothing.
func cachedRepo(id, quant string, bytes int64) hubcache.Repo {
	return hubcache.Repo{
		ID:       id,
		Type:     hubcache.RepoModel,
		Kind:     hubcache.KindGGUF,
		Items:    []hubcache.Item{{Label: quant, Bytes: bytes, Complete: true}},
		Bytes:    bytes,
		Complete: true,
	}
}

// downloadingRepo is a repo mid-download: bytes sitting in blobs/ that no
// snapshot reaches yet, which is what a partly-fetched quant looks like on disk
// (docs/specs/CACHE.md).
func downloadingRepo(id string, bytes int64) hubcache.Repo {
	return hubcache.Repo{
		ID:           id,
		Type:         hubcache.RepoModel,
		Kind:         hubcache.KindGGUF,
		Bytes:        bytes,
		PartialBytes: bytes,
	}
}

// observed is a manager whose three observation seams are fakes: the health
// probe, the cache walk and the Hub.
type observed struct {
	manager *Manager
	host    *fakeHost
	probe   *fakeProbe
	cache   *fakeCache
	hub     *fakeHubTotals
}

// newObserved builds one. Its cache holds the model llamaEntry serves, whole,
// and its probe answers with a closed port — the ordinary start, where only the
// probe has anything left to say.
func newObserved(t *testing.T) *observed {
	t.Helper()
	entry := llamaEntry()
	host := &fakeHost{}
	o := &observed{
		manager: newManager(t, host),
		host:    host,
		probe:   &fakeProbe{health: refused()},
		cache:   &fakeCache{cache: cacheOf(cachedRepo(entry.Repo, entry.Quant, 3000))},
		hub:     &fakeHubTotals{},
	}
	o.manager.probe = o.probe.answer
	o.manager.cache = o.cache.read
	o.manager.hub = o.hub.ask
	return o
}

// start launches one entry through the fake spawner, leaving the process table
// holding its pid.
func (o *observed) start(t *testing.T, entry config.Entry, pid int) Record {
	t.Helper()
	record, _ := startOne(t, o.manager, o.host, entry, pid)
	return record
}

// snapshot observes one record, failing the test if the observation itself
// could not be made.
func (o *observed) snapshot(t *testing.T, record Record) Status {
	t.Helper()
	status, err := o.manager.Snapshot(record)
	if err != nil {
		t.Fatalf("observing %s: %v", record.EntryID, err)
	}
	return status
}

// otherEntry is a second entry, on its own port: what proves one server's
// memory is not another's.
func otherEntry() config.Entry {
	entry := llamaEntry()
	entry.ID = "gemma"
	entry.Repo = "unsloth/gemma-3-27b-it-GGUF"
	entry.Quant = "Q4_K_M"
	entry.Port = 8081
	return entry
}

// The whole phase rule as docs/specs/SERVE.md states it, over the four things
// an observation sees. The order between them is the judgement: green outranks
// everything, a server that answered before and stopped is unhealthy while one
// that never has is still on its way up, and the cache is only consulted for
// the server that is not answering yet.
func TestPhaseMatrix(t *testing.T) {
	tests := []struct {
		name string
		seen observation
		want Phase
	}{
		{
			name: "a record whose process is gone is exited",
			seen: observation{},
			want: PhaseExited,
		},
		{
			name: "a server that served and then died is exited, not unhealthy",
			seen: observation{wasGreen: true, cached: true},
			want: PhaseExited,
		},
		{
			name: "alive, not answering, model not all there: downloading",
			seen: observation{live: true},
			want: PhaseDownloading,
		},
		{
			name: "alive, not answering, model cached: starting",
			seen: observation{live: true, cached: true},
			want: PhaseStarting,
		},
		{
			name: "the health endpoint answers green: running",
			seen: observation{live: true, green: true, cached: true},
			want: PhaseRunning,
		},
		{
			name: "green outranks a cache that looks incomplete",
			seen: observation{live: true, green: true},
			want: PhaseRunning,
		},
		{
			name: "green again after a red patch: running",
			seen: observation{live: true, green: true, wasGreen: true, cached: true},
			want: PhaseRunning,
		},
		{
			name: "red after having been green: unhealthy",
			seen: observation{live: true, wasGreen: true, cached: true},
			want: PhaseUnhealthy,
		},
		{
			name: "having been green outranks the cache too",
			seen: observation{live: true, wasGreen: true},
			want: PhaseUnhealthy,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := derivePhase(test.seen); got != test.want {
				t.Errorf("phase is %q, want %q", got, test.want)
			}
		})
	}
}

// The starting/unhealthy line, driven through a manager: a server that has
// never answered is starting however it refuses — a closed port and
// llama-server's 503 while it loads a model are both "not yet". Once it has
// answered green, the same refusals mean it stopped serving.
func TestAServerIsStartingUntilItHasAnsweredGreenOnce(t *testing.T) {
	o := newObserved(t)
	record := o.start(t, llamaEntry(), 4242)

	steps := []struct {
		name   string
		health Health
		want   Phase
	}{
		{name: "the port is not open yet", health: refused(), want: PhaseStarting},
		{name: "the model is still loading", health: loading(), want: PhaseStarting},
		{name: "the model is loaded", health: serving(), want: PhaseRunning},
		{name: "it answers 503 again", health: loading(), want: PhaseUnhealthy},
		{name: "it stops answering at all", health: refused(), want: PhaseUnhealthy},
		{name: "it recovers", health: serving(), want: PhaseRunning},
	}

	for _, step := range steps {
		o.probe.health = step.health
		status := o.snapshot(t, record)
		if status.Phase != step.want {
			t.Errorf("%s: phase is %q, want %q", step.name, status.Phase, step.want)
		}
		// The probe's own answer travels with the phase: what was asked, and
		// what came back, for the status line to show.
		if status.Health.Detail != step.health.Detail || status.Health.Status != step.health.Status {
			t.Errorf("%s: health is %+v, want the probe's answer %+v", step.name, status.Health, step.health)
		}
		if status.Health.URL != "http://127.0.0.1:8080/health" {
			t.Errorf("%s: the probe went to %q", step.name, status.Health.URL)
		}
	}
}

// The memory of a green answer belongs to one server, not to cria: another
// entry that has never answered is still starting, and restarting an entry
// starts its window over rather than inheriting the last process's verdict.
func TestGreenIsRememberedPerServer(t *testing.T) {
	o := newObserved(t)
	other := otherEntry()
	o.cache.cache.Repos = append(o.cache.cache.Repos, cachedRepo(other.Repo, other.Quant, 2000))

	qwen := o.start(t, llamaEntry(), 4242)
	gemma := o.start(t, other, 4343)

	o.probe.health = serving()
	if status := o.snapshot(t, qwen); status.Phase != PhaseRunning {
		t.Fatalf("the server that answered green is %q, want %q", status.Phase, PhaseRunning)
	}

	o.probe.health = refused()
	if status := o.snapshot(t, qwen); status.Phase != PhaseUnhealthy {
		t.Errorf("the server that stopped answering is %q, want %q", status.Phase, PhaseUnhealthy)
	}
	if status := o.snapshot(t, gemma); status.Phase != PhaseStarting {
		t.Errorf("a second server that has never answered is %q, want %q", status.Phase, PhaseStarting)
	}

	// The same entry, launched again: a new process gets its own startup
	// window, whatever the last one managed to answer.
	delete(o.host.alive, 4242)
	if err := o.manager.Dismiss(qwen); err != nil {
		t.Fatalf("dismissing the exited record: %v", err)
	}
	restarted := o.start(t, llamaEntry(), 4444)
	if status := o.snapshot(t, restarted); status.Phase != PhaseStarting {
		t.Errorf("a restarted server is %q, want %q", status.Phase, PhaseStarting)
	}
}

// A server that is not answering because its model is still coming down shows
// the download: the bytes on disk against what the Hub says the model comes to.
func TestDownloadProgress(t *testing.T) {
	entry := llamaEntry()

	tests := []struct {
		name  string
		total hubapi.Total
		want  Progress
	}{
		{
			name:  "the Hub answered",
			total: hubapi.Total{Bytes: 18_000_000_000, Known: true},
			want:  Progress{Bytes: 4_500_000_000, Total: 18_000_000_000, Known: true},
		},
		{
			// An unreachable Hub costs the percentage and nothing else
			// (docs/specs/SERVE.md): the bytes still climb, with the reason
			// there is no total beside them.
			name:  "the Hub could not be reached",
			total: hubapi.Total{Reason: "the Hub is unreachable"},
			want:  Progress{Bytes: 4_500_000_000, Reason: "the Hub is unreachable"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := newObserved(t)
			o.cache.cache = cacheOf(downloadingRepo(entry.Repo, 4_500_000_000))
			o.hub.total = test.total
			record := o.start(t, entry, 4242)

			status := o.snapshot(t, record)

			if status.Phase != PhaseDownloading {
				t.Fatalf("phase is %q, want %q", status.Phase, PhaseDownloading)
			}
			if status.Progress != test.want {
				t.Errorf("progress is %+v, want %+v", status.Progress, test.want)
			}

			// The expected size does not change while a download runs, so the
			// Hub is asked once per model however often the display refreshes.
			o.snapshot(t, record)
			if want := []string{entry.Repo + ":" + entry.Quant}; !slices.Equal(o.hub.asked, want) {
				t.Errorf("the Hub was asked %v, want %v", o.hub.asked, want)
			}
		})
	}
}

// A running server has no download to show: the progress fields stay zero, and
// the cache is never walked for it — the probe already settled the phase.
func TestARunningServerShowsNoProgress(t *testing.T) {
	o := newObserved(t)
	record := o.start(t, llamaEntry(), 4242)
	o.probe.health = serving()

	status := o.snapshot(t, record)

	if status.Phase != PhaseRunning {
		t.Fatalf("phase is %q, want %q", status.Phase, PhaseRunning)
	}
	if (status.Progress != Progress{}) {
		t.Errorf("a running server shows progress %+v", status.Progress)
	}
	if o.cache.walks != 0 {
		t.Errorf("the cache was walked %d times for a server that is already serving", o.cache.walks)
	}
	if len(o.hub.asked) != 0 {
		t.Errorf("the Hub was asked %v for a server that is already serving", o.hub.asked)
	}
}

// An exited record is a crash report, not a server: cria asks nothing about it —
// no probe, no cache walk, no process cost — and shows what it wrote down at
// launch, the log path included (docs/specs/SERVE.md).
func TestAnExitedRecordIsObservedByItsFactsAlone(t *testing.T) {
	o := newObserved(t)
	record := o.start(t, llamaEntry(), 4242)
	delete(o.host.alive, 4242)

	status := o.snapshot(t, record)

	if status.Phase != PhaseExited {
		t.Fatalf("phase is %q, want %q", status.Phase, PhaseExited)
	}
	if len(o.probe.asked) != 0 {
		t.Errorf("an exited record was probed at %v", o.probe.asked)
	}
	if o.cache.walks != 0 {
		t.Errorf("an exited record walked the cache %d times", o.cache.walks)
	}
	if len(o.host.statsAsked) != 0 {
		t.Errorf("an exited record asked the process table for the cost of %v", o.host.statsAsked)
	}
	if status.Uptime != 0 || status.Stats != (procs.Stats{}) || status.Health != (Health{}) {
		t.Errorf("an exited record carries uptime %s, stats %+v, health %+v", status.Uptime, status.Stats, status.Health)
	}
	if status.LogPath != record.LogPath || status.PID != record.PID {
		t.Errorf("the crash report lost the record's facts: %+v", status.Record)
	}
}

// What the status box shows about a live server beyond its phase: how long it
// has been up, counted from the launch cria recorded, and what it costs right
// now.
func TestUptimeAndCost(t *testing.T) {
	o := newObserved(t)
	record := o.start(t, llamaEntry(), 4242)
	o.host.costs = map[int]procs.Stats{4242: {RSSBytes: 18 << 30, CPUPercent: 342.5}}
	record.LaunchedAt = time.Now().Add(-90 * time.Second)

	status := o.snapshot(t, record)

	if status.Uptime < 90*time.Second || status.Uptime > 95*time.Second {
		t.Errorf("uptime is %s, want about 90s", status.Uptime)
	}
	if want := (procs.Stats{RSSBytes: 18 << 30, CPUPercent: 342.5}); status.Stats != want {
		t.Errorf("stats are %+v, want %+v", status.Stats, want)
	}
	if !slices.Equal(o.host.statsAsked, []int{4242}) {
		t.Errorf("the process table was asked about %v, want the server's pid once", o.host.statsAsked)
	}
}

// A whole listing is observed with one cache walk, however many records need
// presence — and the records cria refused travel with it, because a broken
// record still names a pid cria started.
func TestSnapshotsWalkTheCacheOnce(t *testing.T) {
	o := newObserved(t)
	other := otherEntry()
	o.cache.cache = cacheOf(downloadingRepo(llamaEntry().Repo, 1000), downloadingRepo(other.Repo, 2000))
	o.start(t, llamaEntry(), 4242)
	o.start(t, other, 4343)
	gone := o.start(t, thirdEntry(), 4444)
	delete(o.host.alive, 4444)

	listing, err := o.manager.Snapshots()
	if err != nil {
		t.Fatalf("observing every record: %v", err)
	}

	if o.cache.walks != 1 {
		t.Errorf("the cache was walked %d times for one listing, want once", o.cache.walks)
	}
	phases := map[string]Phase{}
	for _, status := range listing.Servers {
		phases[status.EntryID] = status.Phase
	}
	want := map[string]Phase{"qwen": PhaseDownloading, "gemma": PhaseDownloading, gone.EntryID: PhaseExited}
	for id, phase := range want {
		if phases[id] != phase {
			t.Errorf("%s is %q, want %q", id, phases[id], phase)
		}
	}
}

// A cache that cannot be walked is not reported as "nothing left to download":
// that answer is plausible and wrong, and it would show a downloading server as
// starting for as long as the walk keeps failing.
func TestACacheThatCannotBeWalkedFailsTheObservation(t *testing.T) {
	o := newObserved(t)
	o.cache.err = errors.New("permission denied")
	record := o.start(t, llamaEntry(), 4242)

	_, err := o.manager.Snapshot(record)

	if err == nil {
		t.Fatal("an unreadable cache produced a phase anyway")
	}
	if !strings.Contains(err.Error(), "qwen") || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the failure is %v, want it to name the entry and what went wrong", err)
	}
}

// thirdEntry is a third entry, for the listing test.
func thirdEntry() config.Entry {
	entry := llamaEntry()
	entry.ID = "mistral"
	entry.Repo = "unsloth/Mistral-Small-GGUF"
	entry.Port = 8082
	return entry
}
