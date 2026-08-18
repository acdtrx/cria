package serve

import (
	"strings"
	"testing"

	"cria/internal/procs"
)

// A port a server cria started is holding is answered from the records: the
// entry naming it is the fix, and the operating system is never asked
// (docs/specs/SERVE.md).
func TestPortUseNamesTheManagedServer(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	use, err := manager.PortUse(record.Port)
	if err != nil {
		t.Fatalf("asking who holds port %d: %v", record.Port, err)
	}
	if use.Free() {
		t.Fatal("the port of a running server reads as free")
	}
	if use.Managed == nil {
		t.Fatalf("the holder is %+v, want the record of %s", use, record.EntryID)
	}
	if use.Managed.EntryID != record.EntryID || use.Managed.PID != record.PID {
		t.Errorf("the holder is %s (pid %d), want %s (pid %d)",
			use.Managed.EntryID, use.Managed.PID, record.EntryID, record.PID)
	}
	if len(use.Holders) != 0 {
		t.Errorf("cria asked the operating system as well: %+v", use.Holders)
	}
}

// A record whose process is gone holds nothing: the port is free, whatever the
// record still says about it.
func TestPortUseIgnoresAnExitedRecord(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)
	delete(host.alive, 4242)

	use, err := manager.PortUse(record.Port)
	if err != nil {
		t.Fatalf("asking who holds port %d: %v", record.Port, err)
	}
	if !use.Free() {
		t.Errorf("the port of a crashed server reads as held: %+v", use)
	}
}

// A port nothing of cria's holds is attributed to whoever the operating system
// says holds it, with everything a refusal has to name: pid, command line and
// working directory (docs/specs/SERVE.md).
func TestPortUseDescribesAForeignHolder(t *testing.T) {
	host := &fakeHost{
		alive:     map[int]procs.Identity{99: identityOf("/opt/homebrew/bin/llama-server -m gemma.gguf --port 8080")},
		dirs:      map[int]string{99: "/Users/someone/models"},
		listening: map[int][]int{8080: {99}},
	}
	manager := newManager(t, host)

	use, err := manager.PortUse(8080)
	if err != nil {
		t.Fatalf("asking who holds port 8080: %v", err)
	}
	if use.Managed != nil {
		t.Fatalf("a foreign process was reported as a managed server: %+v", use.Managed)
	}
	if len(use.Holders) != 1 {
		t.Fatalf("the holders are %+v, want the one pid the port has", use.Holders)
	}
	holder := use.Holders[0]
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

// The kill the TUI offers on a foreign holder ends that process and nothing
// else (docs/specs/SERVE.md).
func TestKillHolderEndsAForeignProcess(t *testing.T) {
	host := &fakeHost{
		alive:     map[int]procs.Identity{99: identityOf("/opt/homebrew/bin/llama-server -m gemma.gguf --port 8080")},
		listening: map[int][]int{8080: {99}},
		dieOnKill: true,
	}
	manager := newManager(t, host)

	if err := manager.KillHolder(Holder{PID: 99}); err != nil {
		t.Fatalf("killing the process holding the port: %v", err)
	}
	if len(host.sent) != 1 || host.sent[0] != "KILL 99" {
		t.Errorf("cria sent %v, want one SIGKILL to the holder", host.sent)
	}
	if _, alive := host.alive[99]; alive {
		t.Error("the holder outlived the kill")
	}
}

// A pid that belongs to a live record is refused: that is a server cria started,
// and stopping it by its entry is what removes its record too. Killing it here
// would leave a crash report for something the user asked for.
func TestKillHolderRefusesAManagedServer(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	err := manager.KillHolder(Holder{PID: record.PID})
	if err == nil {
		t.Fatal("cria killed one of its own servers through the foreign-holder kill")
	}
	if !strings.Contains(err.Error(), record.EntryID) {
		t.Errorf("the refusal reads %q, want it to name the entry to stop instead", err)
	}
	if len(host.sent) != 0 {
		t.Errorf("cria signalled %v anyway", host.sent)
	}
}

// A holder the process table cannot describe is still a holder: the pid alone
// refuses the start, rather than the refusal turning into a lookup failure.
func TestPortUseReportsAHolderItCannotDescribe(t *testing.T) {
	host := &fakeHost{listening: map[int][]int{8080: {99}}}
	manager := newManager(t, host)

	use, err := manager.PortUse(8080)
	if err != nil {
		t.Fatalf("asking who holds port 8080: %v", err)
	}
	if len(use.Holders) != 1 || use.Holders[0].PID != 99 {
		t.Fatalf("the holders are %+v, want pid 99 named on its own", use.Holders)
	}
	if use.Holders[0].Command != "" || use.Holders[0].WorkingDir != "" {
		t.Errorf("the holder reads %+v, want the two unknown fields left empty", use.Holders[0])
	}
}
