package serve

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
)

// Where a warm is sent: the same address rule the probe follows — loopback for a
// wildcard bind, the bound address otherwise (docs/specs/CONFIG.md) — at the
// documented completion endpoint.
func TestWarmURL(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   string
	}{
		{
			name:   "a wildcard bind is warmed on loopback",
			record: Record{Backend: config.BackendMLX, Host: "0.0.0.0", Port: 8080},
			want:   "http://127.0.0.1:8080/v1/completions",
		},
		{
			name:   "a bind on the LAN is warmed where it listens",
			record: Record{Backend: config.BackendMLX, Host: "192.168.1.10", Port: 9090},
			want:   "http://192.168.1.10:9090/v1/completions",
		},
		{
			name:   "the IPv6 wildcard is warmed on the IPv6 loopback",
			record: Record{Backend: config.BackendMLX, Host: "::", Port: 8080},
			want:   "http://[::1]:8080/v1/completions",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := completionURL(test.record); got != test.want {
				t.Errorf("the warm goes to %q, want %q", got, test.want)
			}
		})
	}
}

// The warm against a real listener: one POST of the cheapest completion the
// endpoint takes — the model the record launched, a prompt that is never empty,
// one token of answer — and a server that answers it is warm.
func TestWarmingARealServer(t *testing.T) {
	var (
		method, path, contentType string
		sent                      completionRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The warm asks the health endpoint whether there is anything to warm
		// before it asks for a completion (warm.go); only the completion is what
		// this test is reading.
		if r.URL.Path == mlxHealthPath {
			w.WriteHeader(http.StatusOK)
			return
		}
		method, path, contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the warm's body: %v", err)
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Errorf("the warm sent %q, which is not the JSON the endpoint takes: %v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"text":","}]}`)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	manager.complete = newHTTPCompletion()
	record := recordAt(t, mlxEntry(), server.URL)

	if err := manager.Warm(record); err != nil {
		t.Fatalf("warming a server that answered: %v", err)
	}
	if method != http.MethodPost || path != completionPath {
		t.Errorf("the warm was %s %s, want POST %s", method, path, completionPath)
	}
	if contentType != "application/json" {
		t.Errorf("the warm was sent as %q, want application/json", contentType)
	}
	if sent.Model != record.Repo {
		t.Errorf("the warm asked for model %q, want the repo the server was launched with (%q)", sent.Model, record.Repo)
	}
	if sent.MaxTokens != 1 {
		t.Errorf("the warm asked for %d tokens, want the single one that proves the model generated", sent.MaxTokens)
	}
	// An empty prompt is the one shape that must never be sent: mlx_lm.server
	// takes it and never answers, wedging the server the warm was meant to make
	// ready (warm.go).
	if strings.TrimSpace(sent.Prompt) == "" {
		t.Errorf("the warm sent the prompt %q; an empty prompt wedges mlx_lm.server", sent.Prompt)
	}
}

// A backend that loads its model before it answers has nothing to warm, and cria
// sends it nothing: the rule lives in serve rather than in each caller.
func TestALlamaServerIsNeverWarmed(t *testing.T) {
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	manager.complete = newHTTPCompletion()

	if err := manager.Warm(recordAt(t, llamaEntry(), server.URL)); err != nil {
		t.Fatalf("a llama record came back with %v, want nothing to have happened", err)
	}
	if asked != 0 {
		t.Errorf("cria sent %d request(s) to a llama server, want none", asked)
	}
	if LoadsLazily(config.BackendLlama) || !LoadsLazily(config.BackendMLX) {
		t.Error("the lazy-loading rule names the wrong backend")
	}
}

// A server that refuses the completion fails the warm, naming the status and the
// reason it gave: the endpoint answers its errors as a short JSON object, and
// that is the half the status code does not carry.
func TestARefusedWarmCarriesTheReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mlxHealthPath {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error": "Repository Not Found for url: .../mlx-community/Qwen3-30B-A3B-4bit"}`)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	manager.complete = newHTTPCompletion()

	err := manager.Warm(recordAt(t, mlxEntry(), server.URL))
	if err == nil {
		t.Fatal("a refused completion came back as a warm that worked")
	}
	for _, want := range []string{"qwen-mlx did not load its weights", completionPath, "404 Not Found", "Repository Not Found"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure reads %q, want it to carry %q", err, want)
		}
	}
}

// A server that answers but never finishes the completion ends the warm at its
// budget, saying so — the failure is that no completion came back, not that the
// server is gone.
func TestAWarmThatIsNeverAnsweredEndsAtItsBudget(t *testing.T) {
	held := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mlxHealthPath {
			w.WriteHeader(http.StatusOK)
			return
		}
		<-held
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(held)

	manager := newManager(t, &fakeHost{})
	manager.complete = newHTTPCompletion()
	manager.completionWithin = 50 * time.Millisecond

	began := time.Now()
	err := manager.Warm(recordAt(t, mlxEntry(), server.URL))
	if err == nil {
		t.Fatal("a completion that never came back was reported as loaded")
	}
	if !strings.Contains(err.Error(), "no answer within 50ms") {
		t.Errorf("the failure reads %q, want the budget it was bound by", err)
	}
	if waited := time.Since(began); waited > time.Second {
		t.Errorf("the warm took %s to give up on a 50ms budget", waited)
	}
}

// A server cria has just spawned is not listening yet: mlx_lm.server binds its
// port seconds after the spawn, and the TUI fires the warm the moment the
// record is written. So the warm waits for the server to answer — on the
// backend's own health endpoint — and asks for a completion only once it does.
// A completion sent into that gap comes back "connection refused" from a server
// that is loading perfectly well, which is the whole failure this prevents.
func TestAWarmWaitsForTheServerToAnswer(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, mlxEntry(), 4242)

	var (
		probes    int
		probed    string
		atWarm    int
		completed int
	)
	manager.probe = func(url string) Health {
		probes, probed = probes+1, url
		if probes < 4 {
			return Health{URL: url, Detail: "connection refused"}
		}
		return Health{URL: url, Green: true, Status: 200, Detail: "200 OK"}
	}
	manager.complete = func(string, string, time.Duration) error {
		atWarm, completed = probes, completed+1
		return nil
	}

	if err := manager.Warm(record); err != nil {
		t.Fatalf("warming a server that came up: %v", err)
	}
	if completed != 1 {
		t.Fatalf("the warm sent %d completions, want the one that loads the weights", completed)
	}
	if atWarm != 4 {
		t.Errorf("the completion went after %d probe(s), want it held until the server answered (4)", atWarm)
	}
	if probed != probeURL(record) {
		t.Errorf("the wait asked %q, want the backend's own health endpoint (%q)", probed, probeURL(record))
	}
}

// A server whose port never comes up ends the warm at its budget, naming where
// cria was asking: the process is alive, so this is a load that never got
// anywhere rather than a server that is gone.
func TestAWarmOfAPortThatNeverComesUpEndsAtItsBudget(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, mlxEntry(), 4242)
	manager.completionWithin = 20 * time.Millisecond
	manager.probe = func(url string) Health { return Health{URL: url, Detail: "connection refused"} }
	manager.complete = func(string, string, time.Duration) error {
		t.Error("a completion was sent to a server that never answered")
		return nil
	}

	err := manager.Warm(record)
	if err == nil {
		t.Fatal("a server that never answered was reported as loaded")
	}
	if !strings.Contains(err.Error(), "did not answer within 20ms") || !strings.Contains(err.Error(), mlxHealthPath) {
		t.Errorf("the failure reads %q, want the endpoint it waited on and the budget it waited for", err)
	}
	if errors.Is(err, ErrServerGone) {
		t.Error("a server that is still running was reported as gone")
	}
}

// A server that dies while the warm is waiting for it ends the wait there: it
// has nothing left to answer, and sitting out a budget measured in minutes to
// say so would only delay the truth. The answer names it as gone, so a caller
// already showing that the server exited has nothing to add
// (docs/specs/TUI.md).
func TestAWarmEndsWhenTheServerDiesWhileItWaits(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, mlxEntry(), 4242)

	probes := 0
	manager.probe = func(url string) Health {
		probes++
		if probes == 2 {
			delete(host.alive, record.PID)
		}
		return Health{URL: url, Detail: "connection refused"}
	}
	manager.complete = func(string, string, time.Duration) error {
		t.Error("a completion was sent to a server that had died")
		return nil
	}

	err := manager.Warm(record)
	if err == nil {
		t.Fatal("a server that died before it answered was reported as loaded")
	}
	if !errors.Is(err, ErrServerGone) {
		t.Errorf("the failure reads %q, want it to name the server as gone", err)
	}
	if !strings.Contains(err.Error(), "qwen-mlx did not load its weights") {
		t.Errorf("the failure reads %q, want it to name the entry it was warming", err)
	}
	if probes > 3 {
		t.Errorf("the wait probed %d times after the process died, want it to stop at the death", probes)
	}
}

// recordAt is the record of an entry served where a test server is listening.
func recordAt(t *testing.T, entry config.Entry, address string) Record {
	t.Helper()
	served := servedAt(t, entry, address)
	return Record{
		EntryID: served.ID,
		Backend: served.Backend,
		Repo:    served.Repo,
		Quant:   served.Quant,
		Host:    served.Host,
		Port:    served.Port,
	}
}
