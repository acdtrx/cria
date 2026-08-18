package serve

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/tools"
)

// fakeHost is the process table a component test drives: which pids exist, what
// each of them is running, and every signal cria asked it to deliver, in order.
// It is the whole seam between this package and the host — liveness, the stop
// escalation and the once-at-a-time rule are all exercised through it with no
// process on the machine.
type fakeHost struct {
	alive     map[int]procs.Identity
	sent      []string // "TERM 42", "KILL 42", in the order they were sent
	dieOnTerm bool     // the process exits when it is asked to
	dieOnKill bool     // ... and when it is killed
	failWith  error    // Identify answers with this instead of the table
}

func (h *fakeHost) Identify(pid int) (procs.Identity, bool, error) {
	if h.failWith != nil {
		return procs.Identity{}, false, h.failWith
	}
	identity, found := h.alive[pid]
	return identity, found, nil
}

func (h *fakeHost) Terminate(pid int) error {
	h.sent = append(h.sent, fmt.Sprintf("TERM %d", pid))
	if h.dieOnTerm {
		delete(h.alive, pid)
	}
	return nil
}

func (h *fakeHost) Kill(pid int) error {
	h.sent = append(h.sent, fmt.Sprintf("KILL %d", pid))
	if h.dieOnKill {
		delete(h.alive, pid)
	}
	return nil
}

// serve asks the process table three questions. The rest of the interface panics
// rather than answering zero, so a later step that starts asking one of them
// fails here instead of quietly reading an empty answer as the truth.
func (h *fakeHost) Stats(int) (procs.Stats, bool, error) { panic("serve does not ask for stats") }
func (h *fakeHost) Servers() ([]procs.Process, error)    { panic("serve does not scan for servers") }
func (h *fakeHost) WorkingDir(int) (string, bool, error) {
	panic("serve does not ask for working directories")
}
func (h *fakeHost) Listeners(int) ([]int, error) { panic("serve does not attribute ports") }

// fakeSpawner stands in for the one call that creates a process. It records what
// it was asked to launch — argv, environment, and the log file the output would
// have gone to — and hands back a pid of the test's choosing.
type fakeSpawner struct {
	pid      int
	output   string // written to the launch's log, standing in for what a server prints
	failWith error
	launches []launch
}

func (s *fakeSpawner) launch(l launch) (int, error) {
	s.launches = append(s.launches, l)
	if s.failWith != nil {
		return 0, s.failWith
	}
	if s.output != "" {
		if _, err := l.Log.WriteString(s.output); err != nil {
			return 0, err
		}
	}
	return s.pid, nil
}

// last is the launch the test just triggered.
func (s *fakeSpawner) last() launch { return s.launches[len(s.launches)-1] }

// newManager builds a manager over a fresh state root with the stop windows
// wound down: the escalation is about ordering, and a real ten-second grace
// would only make the suite slow.
func newManager(t *testing.T, host procs.Host) *Manager {
	t.Helper()
	manager := New(t.TempDir(), host)
	manager.grace = 50 * time.Millisecond
	manager.confirm = 50 * time.Millisecond
	manager.poll = time.Millisecond
	return manager
}

// identityOf is what the process table would say about a process running this
// command line.
func identityOf(command string) procs.Identity {
	return procs.Identity{Command: command, StartedAt: "Tue Aug 18 14:57:30 2026"}
}

// usableReport is a tool check that found both servers, so the start gate opens.
func usableReport() tools.Report {
	return tools.Report{
		LlamaServer: tools.Tool{Name: tools.LlamaServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/llama-server", Build: 9000},
		MLXLMServer: tools.Tool{Name: tools.MLXLMServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/mlx_lm.server"},
		HF:          tools.Tool{Name: tools.HF, Status: tools.StatusFound, Path: "/opt/homebrew/bin/hf"},
	}
}

// llamaEntry is a resolved llama entry, the way config.Load hands one over.
func llamaEntry() config.Entry {
	return config.Entry{
		ID:      "qwen",
		Path:    "/home/u/.config/cria/models/qwen.toml",
		Backend: config.BackendLlama,
		Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
		Quant:   "UD-Q4_K_XL",
		Host:    "0.0.0.0",
		Port:    8080,
		Name:    "Qwen3 30B",
		Args:    []string{"--ctx-size", "16384"},
	}
}

// startOne starts an entry through a fake spawner that reports pid, and leaves
// the process table holding it. It returns the record and the spawner, which is
// what the tests inspect.
func startOne(t *testing.T, manager *Manager, host *fakeHost, entry config.Entry, pid int) (Record, *fakeSpawner) {
	t.Helper()
	spawner := &fakeSpawner{pid: pid}
	manager.spawn = spawner.launch
	command, err := composeCommand(entry, usableReport())
	if err != nil {
		t.Fatalf("composing the command of %s: %v", entry.ID, err)
	}
	if host.alive == nil {
		host.alive = map[int]procs.Identity{}
	}
	host.alive[pid] = identityOf(strings.Join(command, " "))

	record, err := manager.Start(entry, usableReport())
	if err != nil {
		t.Fatalf("starting %s: %v", entry.ID, err)
	}
	return record, spawner
}

// records lists the record files under a manager's state root.
func records(t *testing.T, manager *Manager) []string {
	t.Helper()
	entries, err := os.ReadDir(manager.recordsRoot())
	if err != nil {
		t.Fatalf("reading %s: %v", manager.recordsRoot(), err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// logs lists the log files under a manager's state root.
func logs(t *testing.T, manager *Manager) []string {
	t.Helper()
	entries, err := os.ReadDir(manager.logsRoot())
	if err != nil {
		t.Fatalf("reading %s: %v", manager.logsRoot(), err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
