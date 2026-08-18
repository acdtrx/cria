// Package serve owns the life of a managed server: composing its command line
// from a config entry, spawning it detached, recording what was spawned, judging
// later whether it is still the process cria launched, and stopping it
// (docs/specs/SERVE.md).
//
// There is no daemon (docs/cria.md, principle 3). A start writes one state
// record and cria is free to exit; any later invocation re-attaches by reading
// those records back. Records are self-contained — stop and status never consult
// the config tree — so editing or deleting an entry never confuses the server it
// launched.
//
// The two things this package touches are both injected: the state root as a
// path and the process table as a procs.Host. That is the whole test seam for
// records, liveness and stop; spawning has one of its own, so every rule here is
// exercised without a live server.
//
// cria never collects a server's exit status: a detached server reparents away,
// and its log file is the crash evidence — never parsed, only shown
// (docs/cria.md, principle 6).
package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cria/internal/procs"
)

const (
	// recordsDir holds one JSON record per server cria started, named after the
	// entry; logsDir holds one file per launch (docs/specs/SERVE.md).
	recordsDir = "servers"
	logsDir    = "logs"
	recordExt  = ".json"
	logExt     = ".log"

	// logStamp is what distinguishes one launch's log from the last: local time,
	// second resolution, spelled so the name sorts in the order the launches
	// happened and holds nothing a filesystem objects to.
	logStamp = "20060102-150405"

	// logsKept is how many launches of one entry keep their log. Retention by
	// count, no rotation machinery (docs/specs/SERVE.md).
	logsKept = 3

	// stopGrace is how long a server has to exit on its own after SIGTERM before
	// cria escalates to SIGKILL. Long enough for a loaded model to be released,
	// short enough that a person waiting on `cria stop` does not think it hung.
	stopGrace = 10 * time.Second

	// killConfirm bounds the wait after SIGKILL. Nothing survives it except a
	// process stuck in the kernel, and that has to become an error rather than a
	// removed record for a server that is still holding its port.
	killConfirm = 2 * time.Second

	// exitPoll is how often cria asks whether a stopping process has gone. The
	// answer lives in another process's lifetime, so there is no event to wait
	// on: this is observation, bounded by the two windows above.
	exitPoll = 100 * time.Millisecond
)

// Manager is the state directory plus the process table: everything cria needs
// to start a server, find it again in a later invocation, and stop it.
type Manager struct {
	root string     // the runtime state root; records and logs live under it
	host procs.Host // the process table: identity and signals

	// spawn is the one call that creates a process. Component tests replace it
	// to drive every rule around a launch — argv, environment, log wiring,
	// records — with no server on the host.
	spawn spawner

	// The stop windows, held rather than read from the constants so a test can
	// drive the whole escalation without waiting out a real grace period.
	grace   time.Duration
	confirm time.Duration
	poll    time.Duration
}

// New builds the manager cria uses: a state root and a process table.
func New(root string, host procs.Host) *Manager {
	return &Manager{
		root:    root,
		host:    host,
		spawn:   spawnDetached,
		grace:   stopGrace,
		confirm: killConfirm,
		poll:    exitPoll,
	}
}

// Root is where cria keeps its runtime state: the same path on macOS and Linux,
// like the config tree (docs/TECH-STACK.md). New takes its root as an argument
// so tests can point it elsewhere; production callers pass this.
//
// XDG_STATE_HOME is deliberately not consulted. hubcache honours XDG_CACHE_HOME
// because that tree belongs to huggingface_hub and cria has to read exactly
// where the library writes; this tree is cria's own, and config.Root already
// spells the config tree out. Half of cria's files moving with an environment
// variable while the other half stays put is worse than neither moving.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory that holds ~/.local/state/cria: %w", err)
	}
	return filepath.Join(home, ".local", "state", "cria"), nil
}

// recordsRoot is the directory holding one record per started server.
func (m *Manager) recordsRoot() string { return filepath.Join(m.root, recordsDir) }

// logsRoot is the directory holding every launch's log.
func (m *Manager) logsRoot() string { return filepath.Join(m.root, logsDir) }

// recordPath is where one entry's record lives. The file is named after the
// entry, which is what makes "an entry runs once at a time" a property of the
// filesystem rather than a rule cria has to remember (docs/specs/SERVE.md).
func (m *Manager) recordPath(entryID string) string {
	return filepath.Join(m.recordsRoot(), entryID+recordExt)
}
