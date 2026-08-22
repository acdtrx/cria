package serve

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"cria/internal/config"
	"cria/internal/hubapi"
	"cria/internal/procs"
	"cria/internal/tools"
)

// launch is one spawn request: the composed argv, the environment the server
// gets, and the open log file that takes everything it prints.
type launch struct {
	Command []string
	Env     []string
	Log     *os.File
}

// spawner starts a server and reports its pid. It is the one call in this
// package that creates a process, which makes it the seam the component tests
// replace: every rule around a launch — the gate, the argv, the environment, the
// log, the record — is then exercised with no server on the host.
type spawner func(launch) (int, error)

// Start launches one entry under one selection and records it. It returns once
// the record is written: the server is spawned, not yet answering — deciding when
// it is actually serving is a separate observation (docs/specs/SERVE.md).
//
// The selection is one pick per choice, already settled by whoever holds the
// explicit, the stored and the default picks. Resolving it is the first thing a
// start does (docs/specs/SERVE.md, Start 1): a pick naming nothing is answered
// with the valid names, before the tool gate and before anything on the host is
// asked a question. Resolution lives here rather than in each frontend so the CLI,
// the TUI and anything later refuse identically.
//
// An entry runs once at a time. A live record refuses the start; a record whose
// process is gone is replaced by this launch.
func (m *Manager) Start(entry config.Entry, selection config.Selection, report tools.Report) (Record, error) {
	// The refusal already names the entry, the choice and what could have been
	// picked instead — everything a caller would add.
	resolved, err := config.Resolve(entry, selection)
	if err != nil {
		return Record{}, err
	}
	command, err := ComposedCommand(entry, resolved, report)
	if err != nil {
		return Record{}, fmt.Errorf("cannot start %s: %w", entry.ID, err)
	}
	if err := m.refuseIfRunning(entry.ID); err != nil {
		return Record{}, err
	}

	// Pruning first, to one short of the retention, leaves room for the log this
	// launch is about to create: a log directory cria cannot tidy then fails the
	// start instead of a server that is already running.
	if err := m.pruneLogs(entry.ID, logsKept-1); err != nil {
		return Record{}, err
	}

	launchedAt := time.Now()
	logPath := m.logPath(entry.ID, launchedAt)
	log, err := m.createLog(logPath)
	if err != nil {
		return Record{}, err
	}
	// The child gets its own copy of the descriptor at spawn, so cria's copy is
	// closed the moment this returns — a detached server holds no handle of
	// cria's (docs/specs/SERVE.md). Nothing here ever writes to it, which is why
	// the close has nothing to report.
	defer log.Close()

	pid, err := m.spawn(launch{Command: command, Env: launchEnv(os.Environ(), hubapi.Token()), Log: log})
	if err != nil {
		// Nothing ran, so the log file is evidence of nothing — and leaving it
		// would push a real crash log out of the three kept. A removal that fails
		// changes nothing about the failed start being reported, which is why its
		// error is dropped here.
		_ = os.Remove(logPath)
		return Record{}, fmt.Errorf("cannot start %s: %w", entry.ID, err)
	}

	identity, captureErr := m.captureIdentity(pid, command[0])
	record := Record{
		EntryID: entry.ID,
		Backend: entry.Backend,
		// The resolved model, not the entry's: what runs is the combination, and
		// the record is what every later observation reads it from
		// (docs/specs/SERVE.md).
		Repo:       resolved.Repo,
		Quant:      resolved.Quant,
		Selection:  picksOf(selection),
		Host:       entry.Host,
		Port:       entry.Port,
		PID:        pid,
		Identity:   identity,
		Command:    command,
		LogPath:    logPath,
		LaunchedAt: launchedAt,
	}
	if err := m.writeRecord(record); err != nil {
		return record, fmt.Errorf("%s was started as pid %d (log: %s), but cria could not record it: %w",
			entry.ID, pid, logPath, err)
	}
	if captureErr != nil {
		return record, fmt.Errorf("%s was started as pid %d (log: %s), but cria could not read the process table to identify it: %w",
			entry.ID, pid, logPath, captureErr)
	}
	return record, nil
}

// refuseIfRunning holds the once-at-a-time rule (docs/specs/SERVE.md). A record
// cria cannot read refuses the start too: it names a pid cria started, and
// starting a second server while that one may still hold the port is exactly
// what this rule exists to prevent.
func (m *Manager) refuseIfRunning(entryID string) error {
	record, found, err := m.loadRecord(entryID)
	if err != nil {
		return fmt.Errorf("cannot tell whether %s is already running: %w; delete the record file once the pid it names is gone", entryID, err)
	}
	if !found {
		return nil
	}
	live, err := m.Live(record)
	if err != nil {
		return err
	}
	if live {
		return fmt.Errorf("%s is already running as pid %d on port %d; stop it first", entryID, record.PID, record.Port)
	}
	return nil
}

// captureIdentity reads back what the process table says the new pid is running,
// so the record carries the identity that makes it verifiable later.
//
// The identity has to name the program cria just launched, and a fresh pid does
// not always name it yet: a server installed as a shim re-execs itself within
// tens of milliseconds of the spawn, and the argv caught in between names
// neither program. So the pid is asked again, at identityPoll, until it either
// names the program or is gone — the settle window bounds the whole thing
// (internal/serve, identitySettle).
//
// Two answers end the wait with no identity at all. A pid that has gone was a
// server that failed on its first breath; a pid that never names the program
// within the window is either that same failure lingering as an unreaped
// "<defunct>" row — recording it would produce a record matching itself forever,
// claiming to be live — or a spawn cria simply could not attribute. Neither is
// the process cria launched: the record takes no identity, matches nothing, and
// reads as exited with its log as the crash report.
//
// The check is containment rather than a prefix because a server installed as a
// script runs under its interpreter, which puts its path in argv[1] — the shape
// mlx-lm ships (internal/procs).
func (m *Manager) captureIdentity(pid int, program string) (procs.Identity, error) {
	deadline := time.Now().Add(m.settle)
	for {
		identity, found, err := m.host.Identify(pid)
		if err != nil {
			return procs.Identity{}, err
		}
		if !found {
			return procs.Identity{}, nil
		}
		if strings.Contains(identity.Command, program) {
			return identity, nil
		}
		if !time.Now().Before(deadline) {
			return procs.Identity{}, nil
		}
		time.Sleep(m.settlePoll)
	}
}

// spawnDetached is the real spawner: it starts a server that outlives cria.
//
// Setsid puts the server in a session of its own. That is what detachment means
// here — it leads its own session and process group, and it has no controlling
// terminal, so the SIGHUP a closing terminal sends to its session never reaches
// it (verified on macOS 26.6: `ps -o tty=` reads "??" and `stat` reads "Ss").
// cria may exit at any moment afterwards; the server reparents to launchd and
// keeps serving.
func spawnDetached(l launch) (int, error) {
	cmd := exec.Command(l.Command[0], l.Command[1:]...)
	cmd.Env = l.Env
	cmd.Stdout = l.Log
	cmd.Stderr = l.Log
	// No stdin, which os/exec wires to /dev/null: a server that read from the
	// terminal cria was started in would be reading from something that is not
	// there. Nothing sets Dir either — the server inherits cria's working
	// directory, because an entry's args are passed through verbatim and may
	// name files relative to where the user ran cria (docs/specs/CONFIG.md).
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// cria collects no exit status — a detached server reparents away and its log
	// is the crash evidence (docs/specs/SERVE.md) — so the handle is released
	// rather than waited on. Until cria exits the server is still its child, and
	// one that dies in the meantime lingers as a "<defunct>" row: that row's
	// command no longer names the server, so liveness reads it as exited, which
	// is what it is.
	_ = cmd.Process.Release()
	return pid, nil
}
