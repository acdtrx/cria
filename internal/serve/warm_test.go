package serve

import (
	"encoding/json"
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
			if got := warmURL(test.record); got != test.want {
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
		sent                      warmRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	manager.warm = newHTTPWarm()
	record := recordAt(t, mlxEntry(), server.URL)

	if err := manager.Warm(record); err != nil {
		t.Fatalf("warming a server that answered: %v", err)
	}
	if method != http.MethodPost || path != warmPath {
		t.Errorf("the warm was %s %s, want POST %s", method, path, warmPath)
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
	manager.warm = newHTTPWarm()

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error": "Repository Not Found for url: .../mlx-community/Qwen3-30B-A3B-4bit"}`)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	manager.warm = newHTTPWarm()

	err := manager.Warm(recordAt(t, mlxEntry(), server.URL))
	if err == nil {
		t.Fatal("a refused completion came back as a warm that worked")
	}
	for _, want := range []string{"qwen-mlx did not load its weights", warmPath, "404 Not Found", "Repository Not Found"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure reads %q, want it to carry %q", err, want)
		}
	}
}

// A server that never answers ends the warm at its budget, saying so — the
// failure is that no completion came back, not that the server is gone.
func TestAWarmThatIsNeverAnsweredEndsAtItsBudget(t *testing.T) {
	held := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-held
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(held)

	manager := newManager(t, &fakeHost{})
	manager.warm = newHTTPWarm()
	manager.warmWithin = 50 * time.Millisecond

	err := manager.Warm(recordAt(t, mlxEntry(), server.URL))
	if err == nil {
		t.Fatal("a completion that never came back was reported as loaded")
	}
	if !strings.Contains(err.Error(), "no answer within 50ms") {
		t.Errorf("the failure reads %q, want the budget it was bound by", err)
	}
}

// A server that is not listening at all fails the warm with what the connection
// said, not with a paragraph wrapping the whole request.
func TestAWarmOfADeadPortSaysWhatHappened(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	address := server.URL
	server.Close()

	manager := newManager(t, &fakeHost{})
	manager.warm = newHTTPWarm()

	err := manager.Warm(recordAt(t, mlxEntry(), address))
	if err == nil {
		t.Fatal("warming a port with nothing on it came back as a load")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("the failure reads %q, want it to say the connection was refused", err)
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
