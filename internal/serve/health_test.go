package serve

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"cria/internal/config"
)

// Where a probe goes, for every shape a bind address takes: a wildcard is
// probed on loopback, anything else exactly where the server listens
// (docs/specs/CONFIG.md), and each backend is asked at its own documented
// endpoint (docs/specs/SERVE.md).
func TestProbeURL(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   string
	}{
		{
			name:   "the wildcard bind is probed on loopback",
			record: Record{Backend: config.BackendLlama, Host: "0.0.0.0", Port: 8080},
			want:   "http://127.0.0.1:8080/health",
		},
		{
			name:   "a private bind is probed where it listens",
			record: Record{Backend: config.BackendLlama, Host: "127.0.0.1", Port: 8080},
			want:   "http://127.0.0.1:8080/health",
		},
		{
			name:   "an address on the LAN is probed at that address",
			record: Record{Backend: config.BackendLlama, Host: "192.168.1.10", Port: 9090},
			want:   "http://192.168.1.10:9090/health",
		},
		{
			name:   "the IPv6 wildcard is probed on the IPv6 loopback",
			record: Record{Backend: config.BackendLlama, Host: "::", Port: 8080},
			want:   "http://[::1]:8080/health",
		},
		{
			name:   "the IPv6 wildcard as some tools spell it",
			record: Record{Backend: config.BackendLlama, Host: "[::]", Port: 8080},
			want:   "http://[::1]:8080/health",
		},
		{
			name:   "a bound IPv6 address keeps its brackets",
			record: Record{Backend: config.BackendLlama, Host: "fd00::1", Port: 8080},
			want:   "http://[fd00::1]:8080/health",
		},
		{
			name:   "a hostname is probed as it was bound",
			record: Record{Backend: config.BackendLlama, Host: "mac-studio.local", Port: 8080},
			want:   "http://mac-studio.local:8080/health",
		},
		{
			// mlx_lm.server publishes no health endpoint, so its model listing
			// is what cria asks for.
			name:   "an mlx server is asked for its model listing",
			record: Record{Backend: config.BackendMLX, Host: "0.0.0.0", Port: 8080},
			want:   "http://127.0.0.1:8080/v1/models",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := probeURL(test.record); got != test.want {
				t.Errorf("the probe goes to %q, want %q", got, test.want)
			}
		})
	}
}

// The real probe against a real listener: each backend's endpoint, and the two
// answers llama-server gives there — 200 once the model is loaded, 503 while it
// still is. A server answering anything but the endpoint cria asked for would
// come back 404, so the phase itself proves the path.
func TestProbingARealServer(t *testing.T) {
	tests := []struct {
		name    string
		entry   config.Entry
		path    string
		status  int
		want    Phase
		details string
	}{
		{
			name:    "llama-server with its model loaded",
			entry:   llamaEntry(),
			path:    "/health",
			status:  http.StatusOK,
			want:    PhaseRunning,
			details: "200 OK",
		},
		{
			name:    "llama-server still loading its model",
			entry:   llamaEntry(),
			path:    "/health",
			status:  http.StatusServiceUnavailable,
			want:    PhaseStarting,
			details: "503 Service Unavailable",
		},
		{
			name:    "mlx_lm.server listing its model",
			entry:   mlxEntry(),
			path:    "/v1/models",
			status:  http.StatusOK,
			want:    PhaseRunning,
			details: "200 OK",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(test.status)
			}))
			defer server.Close()

			o := newObserved(t)
			o.manager.probe = newHTTPProbe()
			record := o.start(t, servedAt(t, test.entry, server.URL), 4242)

			status := o.snapshot(t, record)

			if status.Phase != test.want {
				t.Errorf("phase is %q, want %q", status.Phase, test.want)
			}
			if status.Health.Status != test.status || status.Health.Detail != test.details {
				t.Errorf("health is %+v, want %d %q", status.Health, test.status, test.details)
			}
			if status.Health.Green != (test.want == PhaseRunning) {
				t.Errorf("health reads green=%v for a %d answer", status.Health.Green, test.status)
			}
		})
	}
}

// A server that answered and then stopped: the probe gets no connection at all,
// and because this cria has seen it serve, that is unhealthy rather than a
// start still in progress. The detail is the two words that say so, not the
// paragraph net/http wraps them in.
func TestAServerThatStopsAnswering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	o := newObserved(t)
	o.manager.probe = newHTTPProbe()
	record := o.start(t, servedAt(t, llamaEntry(), server.URL), 4242)

	if status := o.snapshot(t, record); status.Phase != PhaseRunning {
		t.Fatalf("a server answering 200 is %q, want %q", status.Phase, PhaseRunning)
	}

	server.Close()
	status := o.snapshot(t, record)

	if status.Phase != PhaseUnhealthy {
		t.Errorf("a server that stopped answering is %q, want %q", status.Phase, PhaseUnhealthy)
	}
	if status.Health.Green || status.Health.Status != 0 {
		t.Errorf("health is %+v, want no answer at all", status.Health)
	}
	if !strings.Contains(status.Health.Detail, "connection refused") {
		t.Errorf("the detail is %q, want it to say the connection was refused", status.Health.Detail)
	}
}

// servedAt is the entry, bound where a test server is listening.
func servedAt(t *testing.T, entry config.Entry, address string) config.Entry {
	t.Helper()
	listening, err := url.Parse(address)
	if err != nil {
		t.Fatalf("parsing the test server address %q: %v", address, err)
	}
	port, err := strconv.Atoi(listening.Port())
	if err != nil {
		t.Fatalf("reading the port of %q: %v", address, err)
	}
	entry.Host = listening.Hostname()
	entry.Port = port
	return entry
}

// mlxEntry is a resolved mlx entry: an MLX quantization is its own repo, so it
// names no quant.
func mlxEntry() config.Entry {
	return config.Entry{
		ID:      "qwen-mlx",
		Path:    "/home/u/.config/cria/models/qwen-mlx.toml",
		Backend: config.BackendMLX,
		Repo:    "mlx-community/Qwen3-30B-A3B-4bit",
		Host:    "0.0.0.0",
		Port:    8080,
		Name:    "Qwen3 30B (MLX 4bit)",
	}
}
