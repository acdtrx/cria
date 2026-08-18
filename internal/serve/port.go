package serve

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
