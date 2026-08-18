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
