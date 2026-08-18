package serve

import (
	"fmt"
	"slices"
)

// PortUse is who holds a port right now, answered in the order a start has to
// ask (docs/specs/SERVE.md): the server cria itself has running there, else
// whatever the operating system attributes the port to.
//
// The two answers are two different refusals. A managed server is cria's own, so
// the fix is to stop that entry. Anything else is foreign: cria reports its pid,
// its command line and where it runs, and the kill is the TUI's offer, never the
// CLI's (docs/specs/CLI.md).
type PortUse struct {
	Managed *Server  // the live record serving on the port; nil when no record does
	Holders []Holder // the processes listening there; empty when nothing is
}

// Holder is one process holding a port, carrying what a refusal has to say about
// it: which pid, what it is running, and where it thinks it is.
type Holder struct {
	PID        int
	Command    string // the full argv, as `ps` prints it; empty when the process table had nothing to say
	WorkingDir string // where the process runs; empty when it could not be read
}

// Free reports whether a server may be started on the port.
func (u PortUse) Free() bool { return u.Managed == nil && len(u.Holders) == 0 }

// PortUse answers who holds one port.
//
// The records are asked first: a port held by a server cria started is answered
// without a single exec, and it is the one answer that carries its own fix. Only
// when no live record claims the port does cria ask the operating system, which
// is also what turns a server started from someone's forgotten terminal into a
// named process rather than a mysteriously busy port.
func (m *Manager) PortUse(port int) (PortUse, error) {
	listing, err := m.List()
	if err != nil {
		return PortUse{}, err
	}
	for _, server := range listing.Servers {
		if server.Live && server.Port == port {
			managed := server
			return PortUse{Managed: &managed}, nil
		}
	}

	pids, err := m.host.Listeners(port)
	if err != nil {
		return PortUse{}, err
	}
	holders := make([]Holder, 0, len(pids))
	for _, pid := range pids {
		holders = append(holders, m.describe(pid))
	}
	if len(holders) == 0 {
		return PortUse{}, nil
	}
	return PortUse{Holders: holders}, nil
}

// ListensOn reports whether the pid a record names is among the processes
// listening on that record's port, and which pids are listening either way.
//
// It answers a different question from liveness and from the health probe, and
// only together do the three mean "this server is serving": the probe proves
// something answers on the port, liveness proves cria's process is alive, and
// this proves they are the same thing. A server started from a forgotten
// terminal answers a probe exactly as convincingly (docs/specs/SERVE.md).
//
// The empty answer is honest rather than absent: a port with no listener at the
// moment cria looked is reported as such, not as an error.
func (m *Manager) ListensOn(record Record) (bool, []int, error) {
	pids, err := m.host.Listeners(record.Port)
	if err != nil {
		return false, nil, fmt.Errorf("cannot tell which process listens on port %d: %w", record.Port, err)
	}
	return slices.Contains(pids, record.PID), pids, nil
}

// KillHolder ends a process that is holding a port cria did not start it on —
// the kill the TUI offers on the foreign-holder refusal, and the only place cria
// signals a process it has no record of (docs/specs/SERVE.md). The CLI never
// offers it (docs/specs/CLI.md).
//
// A pid that belongs to a live record is refused: that is a server cria started,
// and stopping it by its entry is what removes its record too. Killing it here
// would leave the record behind as a crash report for something the user asked
// for.
func (m *Manager) KillHolder(holder Holder) error {
	listing, err := m.List()
	if err != nil {
		return err
	}
	for _, server := range listing.Servers {
		if server.Live && server.PID == holder.PID {
			return fmt.Errorf("pid %d is %s, a server cria started; stop %s rather than killing its process",
				holder.PID, server.EntryID, server.EntryID)
		}
	}
	return m.host.Kill(holder.PID)
}

// describe fills in what one holder is running and where.
//
// Neither answer is required. The pid is already proof the port is taken, and a
// refusal that said nothing because `ps` could not describe one process would be
// less useful than one naming the pid alone — so a failed lookup leaves its
// field empty rather than travelling (CODING-RULES §5).
func (m *Manager) describe(pid int) Holder {
	holder := Holder{PID: pid}
	if identity, found, err := m.host.Identify(pid); err == nil && found {
		holder.Command = identity.Command
	}
	if dir, found, err := m.host.WorkingDir(pid); err == nil && found {
		holder.WorkingDir = dir
	}
	return holder
}
