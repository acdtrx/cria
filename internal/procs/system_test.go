package procs

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperEnv marks a re-exec of this test binary as a process a test wants to
// find rather than another run of the suite.
const helperEnv = "CRIA_PROCS_HELPER"

// helperLifetime is how long a helper process lives if nothing kills it. Every
// test kills its own; this is the ceiling on a helper that outlived a crashed
// run.
const helperLifetime = 30 * time.Second

// TestMain turns a re-exec of this binary into a process that only sleeps. The
// tests below need real processes with argv of their own choosing, and the test
// binary is the one program guaranteed to be present and runnable — as itself,
// through a symlink named after a server, or from a script. Exiting before
// m.Run() also means the marker arguments a test passes are never parsed as
// flags, and a helper can never run the suite again.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		time.Sleep(helperLifetime)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startHelper runs one child process for a test to look at, in dir, carrying the
// given arguments. os/exec returns from Start only once the exec has succeeded,
// so the child already carries this argv by the time this returns — there is
// nothing to wait for.
//
// The child leads its own process group and the group is killed at the end of
// the test: a helper run from a shell script has a child of its own, and it must
// not outlive the run either.
func startHelper(t *testing.T, program, dir string, args ...string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the helper process %s: %v", program, err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	return cmd
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

// A real process, asked about through a real `ps`: what it runs, when it
// started, what it costs, where it is — and, once it has gone, that all of that
// comes back as a clean absence rather than an error.
func TestSystemIdentifiesARealProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real process")
	}
	host := System{}
	dir := t.TempDir()

	// 60 kB of argument, because the whole identity comparison rests on `ps`
	// handing back a command line it did not shorten. macOS does not truncate
	// `-o command=` output (verified on macOS 26.6); this is the check that says
	// so on every run.
	marker := "cria-procs-" + strings.Repeat("a", 60000)
	helper := startHelper(t, testBinary(t), dir, marker, "--port", "18999")
	pid := helper.Process.Pid

	identity, found, err := host.Identify(pid)
	if err != nil {
		t.Fatalf("identifying pid %d: %v", pid, err)
	}
	if !found {
		t.Fatalf("pid %d was not found while it was running", pid)
	}
	if !strings.Contains(identity.Command, marker) {
		t.Errorf("the command line came back %d bytes and without the %d-byte marker: %q",
			len(identity.Command), len(marker), identity.Command)
	}
	if !strings.HasSuffix(identity.Command, "--port 18999") {
		t.Errorf("the command line lost its trailing arguments: %q", lastBytes(identity.Command))
	}
	if words := strings.Fields(identity.StartedAt); len(words) != lstartWords {
		t.Errorf("the start time is %q, want %d words", identity.StartedAt, lstartWords)
	}

	again, _, err := host.Identify(pid)
	if err != nil {
		t.Fatalf("identifying pid %d a second time: %v", pid, err)
	}
	if !identity.SameProcess(again) {
		t.Error("one process gave two identities on two reads")
	}

	stats, found, err := host.Stats(pid)
	if err != nil {
		t.Fatalf("reading the stats of pid %d: %v", pid, err)
	}
	if !found {
		t.Fatalf("pid %d had no stats while it was running", pid)
	}
	if stats.RSSBytes <= 0 {
		t.Errorf("a running process reported %d bytes resident", stats.RSSBytes)
	}
	if stats.CPUPercent < 0 {
		t.Errorf("a running process reported %v%% cpu", stats.CPUPercent)
	}

	// lsof answers with the path the filesystem actually has, and a temporary
	// directory on macOS is reached through a symlink.
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolving %s: %v", dir, err)
	}
	gotDir, found, err := host.WorkingDir(pid)
	if err != nil {
		t.Fatalf("reading the working directory of pid %d: %v", pid, err)
	}
	if !found || gotDir != wantDir {
		t.Errorf("working directory is %q (found %v), want %q", gotDir, found, wantDir)
	}

	if err := host.Terminate(pid); err != nil {
		t.Fatalf("terminating pid %d: %v", pid, err)
	}
	// Reaped, not merely signalled: a child that has exited but not been waited
	// for is still a row in `ps`.
	_ = helper.Wait()

	if _, found, err := host.Identify(pid); err != nil || found {
		t.Errorf("a terminated pid identified as found=%v, err=%v; want a clean absence", found, err)
	}
	if _, found, err := host.Stats(pid); err != nil || found {
		t.Errorf("a terminated pid had stats: found=%v, err=%v; want a clean absence", found, err)
	}
	if _, found, err := host.WorkingDir(pid); err != nil || found {
		t.Errorf("a terminated pid had a working directory: found=%v, err=%v; want a clean absence", found, err)
	}
}

// SIGKILL is the escalation, and it has to end a process that ignores SIGTERM.
// The helper does not ignore it, so this checks the delivery rather than the
// stubbornness.
func TestSystemKillsARealProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real process")
	}
	host := System{}
	helper := startHelper(t, testBinary(t), t.TempDir(), "cria-procs-kill")
	pid := helper.Process.Pid

	if err := host.Kill(pid); err != nil {
		t.Fatalf("killing pid %d: %v", pid, err)
	}
	if err := helper.Wait(); err == nil {
		t.Error("a killed process exited cleanly")
	}
	if _, found, err := host.Identify(pid); err != nil || found {
		t.Errorf("a killed pid identified as found=%v, err=%v; want a clean absence", found, err)
	}
}

// The foreign scan against the two shapes a managed server really takes: a
// compiled program, whose name is argv[0], and a program installed as a script,
// which the kernel runs through its interpreter so that the name lands in
// argv[1] — the shape mlx-lm ships (verified against a real mlx_lm.server on
// macOS 26.6).
func TestSystemFindsRealServers(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes")
	}
	host := System{}
	dir := t.TempDir()

	// A symlink rather than a copy: a copied Apple-signed binary is refused by
	// macOS, and the name a process is started under is what reaches argv[0]
	// either way.
	compiled := filepath.Join(dir, "llama-server")
	if err := os.Symlink(testBinary(t), compiled); err != nil {
		t.Fatalf("linking %s: %v", compiled, err)
	}
	script := filepath.Join(dir, "mlx_lm.server")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", script, err)
	}

	compiledPID := startHelper(t, compiled, dir, "cria-procs-llama", "--port", "18998").Process.Pid
	scriptPID := startHelper(t, script, dir).Process.Pid

	servers, err := host.Servers()
	if err != nil {
		t.Fatalf("scanning for servers: %v", err)
	}

	found := map[int]Process{}
	for _, server := range servers {
		found[server.PID] = server
	}
	compiledServer, ok := found[compiledPID]
	if !ok {
		t.Fatalf("the scan missed the llama-server at pid %d; it found %v", compiledPID, servers)
	}
	if !strings.HasPrefix(compiledServer.Command, compiled+" ") {
		t.Errorf("the llama-server's command line is %q, want it to start with %q", compiledServer.Command, compiled)
	}
	if compiledServer.StartedAt == "" {
		t.Error("the llama-server came back with no start time")
	}
	scriptServer, ok := found[scriptPID]
	if !ok {
		t.Fatalf("the scan missed the mlx_lm.server at pid %d; it found %v", scriptPID, servers)
	}
	if !strings.Contains(scriptServer.Command, script) {
		t.Errorf("the mlx_lm.server's command line is %q, want it to name %q", scriptServer.Command, script)
	}
}

// Port attribution against a socket this test holds itself: the pid `lsof` names
// has to be this one, and closing the socket has to free the port.
func TestSystemAttributesARealPort(t *testing.T) {
	if testing.Short() {
		t.Skip("binds a real port")
	}
	host := System{}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	pids, err := host.Listeners(port)
	if err != nil {
		t.Fatalf("finding what listens on port %d: %v", port, err)
	}
	if !slices.Contains(pids, os.Getpid()) {
		t.Errorf("port %d is held by %v, want it to include this test at pid %d", port, pids, os.Getpid())
	}
	if len(pids) != len(slices.Compact(slices.Clone(pids))) {
		t.Errorf("port %d reported a pid twice: %v", port, pids)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}
	pids, err = host.Listeners(port)
	if err != nil {
		t.Fatalf("finding what listens on the closed port %d: %v", port, err)
	}
	if len(pids) != 0 {
		t.Errorf("a closed port is held by %v, want nobody", pids)
	}
}

// kill(2) reads 0 as "my whole process group" and -1 as "everything this user
// can signal". A pid that arrived wrong must never become either.
func TestSignalRefusesWhatIsNotAProcess(t *testing.T) {
	host := System{}
	for _, pid := range []int{0, -1, -12345} {
		t.Run(strconv.Itoa(pid), func(t *testing.T) {
			if err := host.Terminate(pid); err == nil {
				t.Errorf("Terminate(%d) was allowed", pid)
			}
			if err := host.Kill(pid); err == nil {
				t.Errorf("Kill(%d) was allowed", pid)
			}
		})
	}
}

// lastBytes keeps a failure message about a 60 kB command line readable.
func lastBytes(command string) string {
	const tail = 80
	if len(command) <= tail {
		return command
	}
	return "..." + command[len(command)-tail:]
}
