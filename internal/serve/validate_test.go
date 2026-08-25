package serve

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/procs"
)

// The server a validation would displace is the live record holding the
// target's port — whatever entry that record belongs to. The config doctrine is
// one port shared by many entries, one model at a time behind a stable
// endpoint, so the port is what names the server to stop.
func TestDisplacedNamesTheServerHoldingTheTargetsPort(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	running, _ := startOne(t, manager, host, llamaEntry(), 4242)

	// Another entry entirely, declaring the same port.
	target := mlxEntry()
	displaced, err := manager.Displaced(target)
	if err != nil {
		t.Fatalf("asking what starting %s would displace: %v", target.ID, err)
	}

	if displaced.Free() {
		t.Fatal("a port with a server on it reads as free")
	}
	if displaced.Port != target.Port {
		t.Errorf("the displacement is on port %d, want the target's own port %d", displaced.Port, target.Port)
	}
	if displaced.Holder == nil {
		t.Fatalf("the displacement is %+v, want the record of %s", displaced, running.EntryID)
	}
	if displaced.Holder.EntryID != running.EntryID || displaced.Holder.PID != running.PID {
		t.Errorf("the server to displace is %s (pid %d), want %s (pid %d)",
			displaced.Holder.EntryID, displaced.Holder.PID, running.EntryID, running.PID)
	}
	if len(displaced.Foreign) != 0 {
		t.Errorf("cria attributed the port to the operating system as well: %+v", displaced.Foreign)
	}
}

// An entry holding its own port is not a special case: the record found there
// is the one to stop and the one to restore, exactly as for any other entry.
// Validating a new combination of the running model is the reason that matters.
func TestDisplacedNamesTheTargetWhenItHoldsItsOwnPort(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	running, _ := startOne(t, manager, host, llamaEntry(), 4242)

	displaced, err := manager.Displaced(llamaEntry())
	if err != nil {
		t.Fatalf("asking what starting %s would displace: %v", running.EntryID, err)
	}
	if displaced.Holder == nil || displaced.Holder.EntryID != running.EntryID {
		t.Fatalf("the displacement is %+v, want the target's own record", displaced)
	}
	if displaced.Holder.PID != running.PID {
		t.Errorf("the server to displace is pid %d, want the running pid %d", displaced.Holder.PID, running.PID)
	}
}

// A record whose process is gone holds nothing (docs/specs/SERVE.md): there is
// no server to stop and nothing to put back afterwards.
func TestDisplacedIgnoresAnExitedRecord(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	startOne(t, manager, host, llamaEntry(), 4242)
	delete(host.alive, 4242)

	displaced, err := manager.Displaced(llamaEntry())
	if err != nil {
		t.Fatalf("asking what starting the entry would displace: %v", err)
	}
	if !displaced.Free() {
		t.Errorf("a crashed server's port reads as held: %+v", displaced)
	}
}

// Nothing on the port is a validation with nothing to displace: start, prove,
// stop. Left as found includes leaving nothing running.
func TestDisplacedFindsNothingOnAFreePort(t *testing.T) {
	manager := newManager(t, &fakeHost{})

	displaced, err := manager.Displaced(llamaEntry())
	if err != nil {
		t.Fatalf("asking what starting the entry would displace: %v", err)
	}
	if !displaced.Free() {
		t.Errorf("an empty port reads as held: %+v", displaced)
	}
	if displaced.Port != llamaEntry().Port {
		t.Errorf("the displacement is on port %d, want the target's own port %d", displaced.Port, llamaEntry().Port)
	}
}

// A process cria did not start has no record to restore from, so it is reported
// rather than displaced — with everything a refusal has to name: pid, command
// line and working directory (docs/specs/SERVE.md).
func TestDisplacedReportsAProcessCriaDidNotStart(t *testing.T) {
	host := &fakeHost{
		alive:     map[int]procs.Identity{99: identityOf("/opt/homebrew/bin/llama-server -m gemma.gguf --port 8080")},
		dirs:      map[int]string{99: "/Users/someone/models"},
		listening: map[int][]int{8080: {99}},
	}
	manager := newManager(t, host)

	displaced, err := manager.Displaced(llamaEntry())
	if err != nil {
		t.Fatalf("asking what starting the entry would displace: %v", err)
	}
	if displaced.Free() {
		t.Fatal("a port held by a foreign process reads as free")
	}
	if displaced.Holder != nil {
		t.Fatalf("a process cria did not start was offered as a server to displace: %+v", displaced.Holder)
	}
	if len(displaced.Foreign) != 1 {
		t.Fatalf("the holders are %+v, want the one pid the port has", displaced.Foreign)
	}
	holder := displaced.Foreign[0]
	if holder.PID != 99 {
		t.Errorf("the holder is pid %d, want 99", holder.PID)
	}
	if !strings.Contains(holder.Command, "llama-server") {
		t.Errorf("the holder's command is %q, want what the process table says it runs", holder.Command)
	}
	if holder.WorkingDir != "/Users/someone/models" {
		t.Errorf("the holder's working directory is %q, want where the process runs", holder.WorkingDir)
	}
}

// The stop a swap makes: the ordinary one, with the record kept as a value
// first. The file on disk goes with the server (docs/specs/SERVE.md), so what is
// held in memory is the only thing that can put it back.
func TestDisplaceStopsTheServerAndHoldsWhatPutsItBack(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	picked := config.Selection{"quant": "q6", "context": "long"}
	running, _ := startPicked(t, manager, host, choicesEntry(), picked, 4242)

	held, err := manager.Displace(running)
	if err != nil {
		t.Fatalf("displacing %s: %v", running.EntryID, err)
	}

	if len(host.sent) != 1 || host.sent[0] != "TERM 4242" {
		t.Errorf("the swap sent %v, want the ordinary stop's SIGTERM", host.sent)
	}
	if left := records(t, manager); len(left) != 0 {
		t.Errorf("the state root still holds %v, want the stop to have taken the record with the server", left)
	}
	if held.EntryID != running.EntryID || held.Port != running.Port || held.Repo != running.Repo || held.Quant != running.Quant {
		t.Errorf("the held record is %+v, want what %s was serving", held, running.EntryID)
	}
	if !maps.Equal(held.Selection, picked) {
		t.Errorf("the held picks are %v, want the combination that was running (%v)", held.Selection, picked)
	}

	// The picks are the swap's own copy: whatever a caller does to the map it
	// handed over, what goes back is what ran.
	running.Selection["quant"] = "q4"
	if held.Selection["quant"] != "q6" {
		t.Errorf("the held picks read %v after the caller wrote into its own map", held.Selection)
	}
}

// A restore replays the record, not the entry: the combination that was serving
// comes back even where the entry's own defaults are something else.
func TestRestoreStartsTheHeldRecordsOwnCombination(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	entry := choicesEntry()
	picked := config.Selection{"quant": "q6", "context": "long"}

	running, _ := startPicked(t, manager, host, entry, picked, 4242)
	held, err := manager.Displace(running)
	if err != nil {
		t.Fatalf("displacing %s: %v", running.EntryID, err)
	}

	command := composedFor(t, entry, picked)
	spawner := &fakeSpawner{pid: 4343}
	manager.spawn = spawner.launch
	host.alive[4343] = identityOf(strings.Join(command, " "))

	back, err := manager.Restore(treeOf(entry), held, usableReport())
	if err != nil {
		t.Fatalf("putting %s back: %v", held.EntryID, err)
	}
	if len(spawner.launches) != 1 {
		t.Fatalf("the restore spawned %d servers, want the one it displaced", len(spawner.launches))
	}
	if !slices.Equal(spawner.last().Command, command) {
		t.Errorf("the restore launched %v, want the record's own combination %v", spawner.last().Command, command)
	}
	if back.Quant != "UD-Q6_K_XL" {
		t.Errorf("the restored server serves quant %q, want the record's own rather than the entry's default", back.Quant)
	}
	if !maps.Equal(back.Selection, picked) {
		t.Errorf("the restored record's picks are %v, want the held ones (%v)", back.Selection, picked)
	}
	if left := records(t, manager); len(left) != 1 {
		t.Errorf("the state root holds %v, want the restored server recorded again", left)
	}
}

// A record is self-contained, but a start needs the file: an entry deleted or
// edited while the swap was in flight fails the restore, naming which of the two
// it was — and nothing is spawned on a guess.
func TestRestoreRefusesWhatTheTreeNoLongerDeclares(t *testing.T) {
	renamed := choicesEntry()
	renamed.Choices[0].Options = []config.ChoiceOption{
		{Name: "q4", Quant: "UD-Q4_K_XL"},
		{Name: "q8", Quant: "UD-Q8_K_XL"},
	}

	tests := []struct {
		name string
		tree *config.Tree
		want []string
	}{
		{
			name: "the entry is no longer in the tree",
			tree: &config.Tree{Root: "/home/u/.config/cria"},
			want: []string{"not an entry cria can read any more"},
		},
		{
			name: "the option it was running under is gone",
			tree: treeOf(renamed),
			want: []string{`choice "quant"`, `no option named "q6"`, "q4, q8"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := &fakeHost{dieOnTerm: true}
			manager := newManager(t, host)
			running, _ := startPicked(t, manager, host, choicesEntry(), config.Selection{"quant": "q6", "context": "long"}, 4242)
			held, err := manager.Displace(running)
			if err != nil {
				t.Fatalf("displacing %s: %v", running.EntryID, err)
			}

			spawner := &fakeSpawner{pid: 4343}
			manager.spawn = spawner.launch

			back, err := manager.Restore(test.tree, held, usableReport())
			if err == nil {
				t.Fatalf("a record the tree can no longer launch came back as %+v", back)
			}
			for _, want := range append(test.want, "cannot put qwen-choices back on port 8080") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the failure reads %q, want it to carry %q", err, want)
				}
			}
			if len(spawner.launches) != 0 {
				t.Errorf("a refused restore launched %v anyway", spawner.launches)
			}
		})
	}
}

// A restart that the host itself refuses is the third way a restore fails, and
// it says so with what the spawn answered — the caller has to report a machine
// that is no longer as it found it.
func TestRestoreReportsARestartThatFailed(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	entry := choicesEntry()

	running, _ := startPicked(t, manager, host, entry, config.DefaultSelection(entry), 4242)
	held, err := manager.Displace(running)
	if err != nil {
		t.Fatalf("displacing %s: %v", running.EntryID, err)
	}

	refused := errors.New("fork/exec /opt/homebrew/bin/llama-server: resource temporarily unavailable")
	manager.spawn = (&fakeSpawner{failWith: refused}).launch

	back, err := manager.Restore(treeOf(entry), held, usableReport())
	if err == nil {
		t.Fatalf("a restart the host refused came back as %+v", back)
	}
	for _, want := range []string{"cannot put qwen-choices back on port 8080", "resource temporarily unavailable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure reads %q, want it to carry %q", err, want)
		}
	}
}

// The proof is one real completion, and every backend gets it: this is
// fit-proofing rather than the lazy-load warming Warm gates on backend, so the
// llama server a warm never touches is asked here exactly like the mlx one.
func TestProveTakesOneRealCompletionFromEitherBackend(t *testing.T) {
	for _, entry := range []config.Entry{llamaEntry(), mlxEntry()} {
		t.Run(string(entry.Backend), func(t *testing.T) {
			var (
				requests     int
				method, path string
				sent         completionRequest
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				method, path = r.Method, r.URL.Path
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Errorf("the proof sent something the endpoint cannot read: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"choices":[{"text":"."}]}`)
			}))
			defer server.Close()

			manager := newManager(t, &fakeHost{})
			manager.complete = newHTTPCompletion()
			record := recordAt(t, entry, server.URL)

			if err := manager.Prove(record); err != nil {
				t.Fatalf("proving a server that answered: %v", err)
			}
			if requests != 1 {
				t.Errorf("the proof sent %d requests, want the one completion", requests)
			}
			if method != http.MethodPost || path != completionPath {
				t.Errorf("the proof was %s %s, want POST %s", method, path, completionPath)
			}
			if sent.Model != record.Repo {
				t.Errorf("the proof asked for model %q, want the record's own (%q)", sent.Model, record.Repo)
			}
			if strings.TrimSpace(sent.Prompt) == "" {
				t.Errorf("the proof sent the prompt %q; an empty prompt wedges mlx_lm.server", sent.Prompt)
			}
		})
	}
}

// A server that refuses the completion is a server that does not fit, and what
// it said is the evidence: the status, and the reason it gave, quoted.
func TestAProofCarriesWhatTheServerRefusedWith(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"code":500,"message":"failed to allocate compute buffers"}}`)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	manager.complete = newHTTPCompletion()

	err := manager.Prove(recordAt(t, llamaEntry(), server.URL))
	if err == nil {
		t.Fatal("a refused completion came back as a server that serves")
	}
	for _, want := range []string{"qwen did not answer a completion", completionPath, "500 Internal Server Error", "failed to allocate compute buffers"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure reads %q, want it to carry %q", err, want)
		}
	}
}

// A port with nothing behind it fails the proof too: a green health signal is
// not what is being asked for here, an answer is.
func TestAProofOfAServerThatIsNotAnsweringFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	record := recordAt(t, llamaEntry(), server.URL)
	server.Close()

	manager := newManager(t, &fakeHost{})
	manager.complete = newHTTPCompletion()

	err := manager.Prove(record)
	if err == nil {
		t.Fatal("a server that answered nothing came back as a server that serves")
	}
	for _, want := range []string{"qwen did not answer a completion", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure reads %q, want it to carry %q", err, want)
		}
	}
}

// treeOf is a config tree declaring one entry — what a restore reads its entry
// back out of.
func treeOf(entry config.Entry) *config.Tree {
	return &config.Tree{Root: "/home/u/.config/cria", Entries: []config.Entry{entry}}
}

// Where the gate asks, for every shape a bind address takes: a wildcard on
// loopback, anything else exactly where the server listens
// (docs/specs/CONFIG.md), at llama-server's slot endpoint.
func TestSlotsURL(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   string
	}{
		{
			name:   "a wildcard bind is asked on loopback",
			record: Record{Backend: config.BackendLlama, Host: "0.0.0.0", Port: 8080},
			want:   "http://127.0.0.1:8080/slots",
		},
		{
			name:   "a bind on the LAN is asked where it listens",
			record: Record{Backend: config.BackendLlama, Host: "192.168.1.10", Port: 9090},
			want:   "http://192.168.1.10:9090/slots",
		},
		{
			name:   "the IPv6 wildcard is asked on the IPv6 loopback",
			record: Record{Backend: config.BackendLlama, Host: "::", Port: 8080},
			want:   "http://[::1]:8080/slots",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := slotsURL(test.record); got != test.want {
				t.Errorf("the gate asks %q, want %q", got, test.want)
			}
		})
	}
}

// The gate against a real listener: the per-slot is_processing flag decides,
// any one working slot is enough, and everything else the endpoint publishes is
// ignored rather than refused.
func TestGeneratingReadsTheSlotsOfALlamaServer(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    Busy
	}{
		{
			name:    "a slot answering a request",
			payload: `[{"id":0,"n_ctx":16384,"n_prompt_tokens":812,"is_processing":true,"next_token":{"n_decoded":41}}]`,
			want:    BusyGenerating,
		},
		{
			name:    "every slot free",
			payload: `[{"id":0,"n_ctx":16384,"is_processing":false},{"id":1,"n_ctx":16384,"is_processing":false}]`,
			want:    BusyIdle,
		},
		{
			name:    "one of several slots working",
			payload: `[{"id":0,"is_processing":false},{"id":1,"is_processing":true}]`,
			want:    BusyGenerating,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var asked string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, test.payload)
			}))
			defer server.Close()

			manager := newManager(t, &fakeHost{})
			manager.slots = newHTTPSlots()

			generation := manager.Generating(recordAt(t, llamaEntry(), server.URL))
			if generation.Busy != test.want {
				t.Errorf("the gate reads %q, want %q", generation.Busy, test.want)
			}
			if generation.Detail != "" {
				t.Errorf("the gate carries %q, want nothing to have stood in its way", generation.Detail)
			}
			if asked != slotsPath {
				t.Errorf("the gate asked %q, want the documented slot endpoint %q", asked, slotsPath)
			}
		})
	}
}

// A signal that is not there is unverifiable, never idle: a build that does not
// publish the endpoint, an answer that is not a slot listing, and a listing with
// nothing in it all leave the gate with no evidence, and each says so.
func TestGeneratingCannotVerifyWhatItCannotRead(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		payload string
		want    []string
	}{
		{
			name:    "a build whose slot endpoint is not enabled",
			status:  http.StatusNotImplemented,
			payload: `{"error":{"message":"This server does not support slots endpoint."}}`,
			want:    []string{"501 Not Implemented", "does not support slots endpoint"},
		},
		{
			name:    "an answer that is not a slot listing",
			status:  http.StatusOK,
			payload: `{"error":"slots are disabled"}`,
			want:    []string{"cannot read as a slot listing"},
		},
		{
			name:    "a server that lists no slots at all",
			status:  http.StatusOK,
			payload: `[]`,
			want:    []string{"listed no slots"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				io.WriteString(w, test.payload)
			}))
			defer server.Close()

			manager := newManager(t, &fakeHost{})
			manager.slots = newHTTPSlots()

			generation := manager.Generating(recordAt(t, llamaEntry(), server.URL))
			if generation.Busy != BusyUnverifiable {
				t.Fatalf("the gate reads %q, want %q", generation.Busy, BusyUnverifiable)
			}
			for _, want := range append(test.want, server.URL+slotsPath) {
				if !strings.Contains(generation.Detail, want) {
					t.Errorf("the gate says %q, want it to carry %q", generation.Detail, want)
				}
			}
		})
	}
}

// A server that is not answering leaves the gate with nothing to read either —
// and that is a fact about the look cria took, not about the server being idle.
func TestGeneratingCannotVerifyAServerThatIsNotAnswering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	record := recordAt(t, llamaEntry(), server.URL)
	server.Close()

	manager := newManager(t, &fakeHost{})
	manager.slots = newHTTPSlots()

	generation := manager.Generating(record)
	if generation.Busy != BusyUnverifiable {
		t.Fatalf("the gate reads %q for a server that is not answering, want %q", generation.Busy, BusyUnverifiable)
	}
	if !strings.Contains(generation.Detail, "connection refused") {
		t.Errorf("the gate says %q, want it to say the connection was refused", generation.Detail)
	}
	if !strings.Contains(generation.Detail, slotsPath) {
		t.Errorf("the gate says %q, want it to name where it asked", generation.Detail)
	}
}

// mlx_lm.server documents no per-slot signal, so cria asks it nothing at all:
// the answer is unverifiable, named as such, without a request leaving the
// machine.
func TestGeneratingNeverAsksAnMLXServer(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	manager.slots = func(url string) Generation {
		t.Errorf("cria asked an mlx server for its slots at %s", url)
		return Generation{Busy: BusyIdle}
	}

	generation := manager.Generating(Record{
		EntryID: "qwen-mlx",
		Backend: config.BackendMLX,
		Host:    "0.0.0.0",
		Port:    8080,
	})
	if generation.Busy != BusyUnverifiable {
		t.Fatalf("the gate reads %q for an mlx server, want %q", generation.Busy, BusyUnverifiable)
	}
	if !strings.Contains(generation.Detail, "mlx_lm.server") {
		t.Errorf("the gate says %q, want it to name the server whose signal is missing", generation.Detail)
	}
	if publishesSlots(config.BackendMLX) || !publishesSlots(config.BackendLlama) {
		t.Error("the slot-signal rule names the wrong backend")
	}
}
