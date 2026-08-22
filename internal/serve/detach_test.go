package serve

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/tools"
)

// Detachment is the one property of this package that cannot be proved inside a
// single process: the parent has to die while the child keeps serving. So this
// test binary plays all three parts — the test, a cria that starts a server and
// exits, and the server itself.
const (
	launcherRole = "CRIA_SERVE_LAUNCHER"   // =1: start a server through the real spawn path, then exit
	sleeperRole  = "CRIA_SERVE_SLEEPER"    // =1: be the server
	stateRootEnv = "CRIA_SERVE_STATE_ROOT" // where the launcher keeps its records and logs
)

const (
	// sleeperSettle is how long the server waits before reporting a second time.
	// The launcher exits within milliseconds of the spawn, so by then the parent
	// the server names is whoever the operating system reparented it to.
	sleeperSettle = time.Second
	// sleeperLifetime is the ceiling on a server nothing stops. Every run stops
	// its own; this bounds one that outlived a crashed run.
	sleeperLifetime = 60 * time.Second
)

// TestMain gives a re-exec of this binary the role its environment names. The
// sleeper is checked first because it inherits the launcher's environment,
// marker and all — that order is what keeps a server from launching a server.
func TestMain(m *testing.M) {
	switch {
	case os.Getenv(sleeperRole) == "1":
		runSleeper()
	case os.Getenv(launcherRole) == "1":
		runLauncher()
	}
	os.Exit(m.Run())
}

// runSleeper stands in for a server: it reports itself to stdout — which the
// launch wired to the log file — twice, once at start and once after its parent
// has had time to exit, and then waits to be stopped.
func runSleeper() {
	report := func(label string) {
		fmt.Printf("%s pid=%d ppid=%d args=%s\n", label, os.Getpid(), os.Getppid(), strings.Join(os.Args[1:], " "))
	}
	report("started")
	time.Sleep(sleeperSettle)
	report("settled")
	time.Sleep(sleeperLifetime)
	os.Exit(0)
}

// runLauncher is a whole cria invocation: it starts one entry through the real
// spawn path and exits immediately, which is what `cria start` does.
func runLauncher() {
	program, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "finding this binary:", err)
		os.Exit(1)
	}
	// The server is another copy of this binary; the marker is what turns it
	// into one, and it travels through the environment the launch inherits.
	if err := os.Setenv(sleeperRole, "1"); err != nil {
		fmt.Fprintln(os.Stderr, "marking the server:", err)
		os.Exit(1)
	}

	manager := New(os.Getenv(stateRootEnv), procs.System{})
	entry := config.Entry{
		ID: "detached", Backend: config.BackendLlama,
		Repo: "cria/detachment", Quant: "Q4_K_M",
		Host: "127.0.0.1", Port: 18099,
	}
	report := tools.Report{LlamaServer: tools.Tool{Name: tools.LlamaServer, Status: tools.StatusFound, Path: program}}

	record, err := manager.Start(entry, nil, report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "starting:", err)
		os.Exit(1)
	}
	fmt.Println(record.PID)
	os.Exit(0)
}

// The mechanism this whole package rests on: a server cria spawns survives the
// cria that spawned it, in a session of its own, with its output in its log —
// and a later cria invocation finds it from the record alone (docs/specs/SERVE.md).
func TestAServerOutlivesTheCriaThatStartedIt(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real detached process")
	}
	root := t.TempDir()

	launcher := exec.Command(testBinary(t))
	launcher.Env = append(os.Environ(), launcherRole+"=1", stateRootEnv+"="+root)
	// This also proves the spawn holds no pipe of cria's: the launcher's output
	// is read to end-of-file, and a server still holding it would hang this call
	// for its whole lifetime.
	out, err := launcher.CombinedOutput()
	if err != nil {
		t.Fatalf("the launcher failed: %v\n%s", err, out)
	}

	// A fresh manager over the same state root: this is the re-attach path, with
	// nothing carried over from the process that did the starting.
	manager := New(root, procs.System{})
	listing, err := manager.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listing.Broken) != 0 {
		t.Fatalf("the launcher wrote a record cria refuses: %v", listing.Broken[0].Err)
	}
	if len(listing.Servers) != 1 {
		t.Fatalf("the state root holds %d servers, want the one that was started", len(listing.Servers))
	}
	server := listing.Servers[0]
	t.Cleanup(func() { _ = manager.Kill(server.Record) })

	if !server.Live {
		t.Fatalf("the server did not survive the launcher's exit; its log holds:\n%s", read(t, server.LogPath))
	}
	if strconv.Itoa(server.PID) != strings.TrimSpace(string(out)) {
		t.Errorf("the record names pid %d, the launcher reported %q", server.PID, out)
	}

	// The server's own report, in the log the launch created: the parent it has
	// now is not the launcher, which is gone.
	settled := waitForLog(t, server.LogPath, "settled")
	fields := strings.Fields(settled)
	if len(fields) < 3 || fields[1] != "pid="+strconv.Itoa(server.PID) {
		t.Fatalf("the log line %q does not come from pid %d", settled, server.PID)
	}
	if fields[2] != "ppid=1" {
		t.Errorf("the server's parent is %s, want ppid=1: it was not reparented away from the launcher", fields[2])
	}
	if !strings.Contains(settled, "--port 18099") {
		t.Errorf("the server did not receive the composed command line: %q", settled)
	}

	// A session of its own, with no controlling terminal: the SIGHUP a closing
	// terminal sends to its session cannot reach it. macOS spells the absent
	// terminal "??" and marks a session leader "s" in the state column; Linux
	// spells the terminal "?".
	terminal, state := psState(t, server.PID)
	if terminal != "??" && terminal != "?" {
		t.Errorf("the server's controlling terminal is %q, want none", terminal)
	}
	if !strings.Contains(state, "s") {
		t.Errorf("the server's state is %q, want a session leader", state)
	}

	// And it stops on demand, from an invocation that never started it.
	if err := manager.Stop(server.Record); err != nil {
		t.Fatalf("stopping: %v", err)
	}
	if _, found, err := (procs.System{}).Identify(server.PID); found || err != nil {
		t.Errorf("pid %d survived the stop: found=%v, err=%v", server.PID, found, err)
	}
	if names := records(t, manager); len(names) != 0 {
		t.Errorf("a deliberate stop left the records %v", names)
	}
}

// testBinary is the path to run to get another copy of this process.
func testBinary(t *testing.T) string {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatalf("finding this test binary: %v", err)
	}
	return program
}

// waitForLog waits for a line the server writes on its own schedule.
func waitForLog(t *testing.T, path, label string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, line := range strings.Split(read(t, path), "\n") {
			if strings.HasPrefix(line, label+" ") {
				return line
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the log %s never held a %q line; it holds:\n%s", path, label, read(t, path))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// psState asks the process table for the two columns that describe a process's
// attachment: its controlling terminal and its state flags.
func psState(t *testing.T, pid int) (terminal, state string) {
	t.Helper()
	out, err := exec.Command("ps", "-o", "tty=,stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("asking ps about pid %d: %v", pid, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("ps answered %q for pid %d", out, pid)
	}
	return fields[0], fields[1]
}
