package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/hubapi"
	"cria/internal/tools"
)

// The router is one process per host, and it is not an entry: no models/<id>.toml
// declares it, so it is started as itself from engines/router.toml, and the state
// it leaves behind is the engine's rather than any entry's (docs/specs/SERVE.md).
//
// Everything else about its life is the life every managed server has. It is
// spawned detached, recorded, judged live by pid and identity, observed by one
// probe of its own health endpoint and stopped by the same escalation — so this
// file holds what is different about it and nothing that is not: where its state
// lives, the preset it serves from, and the command line that puts llama-server
// in router mode.

const (
	// engineStateDir holds one subfolder per engine, for state that belongs to an
	// engine rather than to an entry: the router's composed preset, its record
	// and its logs (OVERVIEW ruling 2). Entry records and logs stay where they
	// are — an entry's server is still an entry's.
	engineStateDir = "engines"

	// presetFile is the composed preset the router serves from, and recordFile
	// the record of the process serving it. Both are regenerated at every start
	// and neither is ever edited (docs/specs/SERVE.md).
	presetFile = "preset.ini"
	recordFile = "server" + recordExt

	// engineLogsDir holds that engine's own launch logs, under the same
	// retention entries have. They live here rather than beside the entries' logs
	// so an engine named like an entry cannot prune the entry's logs, or be
	// pruned by it.
	engineLogsDir = "logs"
)

// RouterPort is the port the router serves on, or the refusal that says the tree
// declares none.
//
// The router takes no port from default_port: that is the port the entries share
// (docs/specs/CONFIG.md), and a router bound to it would collide with every one
// of them at the first start. So a tree with no engines/router.toml has no
// router, and says so by name.
func RouterPort(router config.RouterConfig) (int, error) {
	if router.Port == 0 {
		return 0, fmt.Errorf("no router port: %s sets none, and the router serves beside the entries rather than on their shared port; add `port = <port>` to that file", router.Path)
	}
	return router.Port, nil
}

// RouterServer reads the record of this host's router: what cria started, and
// whether that process is still the one it started. found is false when the
// router has never been started here, or its record was removed by a stop.
func (m *Manager) RouterServer() (Server, bool, error) {
	record, err := readRecord(m.routerRecordPath(), string(config.BackendRouter))
	if errors.Is(err, fs.ErrNotExist) {
		return Server{}, false, nil
	}
	if err != nil {
		return Server{}, false, fmt.Errorf("%s: %w; delete that file once the pid it names is gone", m.routerRecordPath(), err)
	}
	live, err := m.Live(record)
	if err != nil {
		return Server{}, false, err
	}
	return Server{Record: record, Live: live}, true, nil
}

// StartRouter launches this host's router and records it. Like every start it
// returns once the record is written: the process is spawned, not yet answering
// (docs/specs/SERVE.md).
//
// The preset is composed first, because it is the one part of the launch the
// config tree can make impossible: an engine file whose args cannot be written
// as preset keys refuses here, before anything on the host has changed
// (internal/engine). The models it composed come back alongside the record —
// including the ones it could not carry, which the caller reports rather than
// discovering from a router that serves fewer models than the store lists.
func (m *Manager) StartRouter(tree *config.Tree, report tools.Report) (Record, RouterModels, error) {
	router := tree.Router
	port, err := RouterPort(router)
	if err != nil {
		return Record{}, RouterModels{}, err
	}
	models, err := m.RouterModels(tree)
	if err != nil {
		return Record{}, RouterModels{}, fmt.Errorf("cannot start the router: %w", err)
	}
	command, err := m.routerCommand(router, port, report)
	if err != nil {
		return Record{}, RouterModels{}, fmt.Errorf("cannot start the router: %w", err)
	}
	if err := m.refuseIfRouterRunning(); err != nil {
		return Record{}, RouterModels{}, err
	}

	// Pruning first, to one short of the retention, leaves room for the log this
	// launch is about to create — the same order an entry's start takes.
	if err := pruneLogsIn(m.routerLogsRoot(), string(config.BackendRouter), logsKept-1); err != nil {
		return Record{}, RouterModels{}, err
	}

	launchedAt := time.Now()
	logPath := launchLogPath(m.routerLogsRoot(), string(config.BackendRouter), launchedAt)
	log, err := m.createLog(logPath)
	if err != nil {
		return Record{}, RouterModels{}, err
	}
	defer log.Close()

	if err := m.writeRouterPreset(models.Preset); err != nil {
		return Record{}, RouterModels{}, err
	}

	pid, err := m.spawn(launch{Command: command, Env: launchEnv(os.Environ(), hubapi.Token()), Log: log})
	if err != nil {
		// Nothing ran, so this launch's log is evidence of nothing, and leaving it
		// would push a real crash log out of the three kept.
		_ = os.Remove(logPath)
		return Record{}, RouterModels{}, fmt.Errorf("cannot start the router: %w", err)
	}

	identity, captureErr := m.captureIdentity(pid, command[0])
	record := Record{
		EntryID:    string(config.BackendRouter),
		Backend:    config.BackendRouter,
		Preset:     m.routerPresetPath(),
		Host:       router.Host,
		Port:       port,
		PID:        pid,
		Identity:   identity,
		Command:    command,
		LogPath:    logPath,
		LaunchedAt: launchedAt,
	}
	if err := writeRecordAt(m.routerRecordPath(), record); err != nil {
		return record, models, fmt.Errorf("the router was started as pid %d (log: %s), but cria could not record it: %w",
			pid, logPath, err)
	}
	if captureErr != nil {
		return record, models, fmt.Errorf("the router was started as pid %d (log: %s), but cria could not read the process table to identify it: %w",
			pid, logPath, captureErr)
	}
	return record, models, nil
}

// StopRouter ends the router the way every managed server is ended: SIGTERM, a
// grace period, then SIGKILL, with the record removed once the process is
// confirmed gone (docs/specs/SERVE.md). The composed preset is left where it is —
// it is regenerated at the next start, and it is what the stopped router was
// serving from.
func (m *Manager) StopRouter(record Record) error {
	return m.end(record, m.grace, m.routerRecordPath())
}

// RouterSnapshot observes the router: whether it is still the process cria
// launched, and whether its port answers.
//
// It reads no cache and asks the Hub nothing, because the router downloads
// nothing when it starts: it holds no model of its own, and the models it serves
// are fetched by the child it spawns for one, when a request asks for it. So the
// downloading phase an entry's server can be in has no counterpart here — the
// router is starting, running, unhealthy or exited (docs/specs/SERVE.md).
func (m *Manager) RouterSnapshot(record Record) (Status, error) {
	live, err := m.Live(record)
	if err != nil {
		return Status{}, err
	}
	status := Status{Record: record, Phase: PhaseExited}
	if !live {
		return status, nil
	}

	status.Uptime = time.Since(record.LaunchedAt)
	stats, found, err := m.host.Stats(record.PID)
	if err != nil {
		return Status{}, err
	}
	if found {
		status.Stats = stats
	}

	served, err := engine.For(record.Backend)
	if err != nil {
		return Status{}, err
	}
	status.Health = m.probe(probeURL(served, record))
	status.Phase = derivePhase(observation{
		live:     true,
		green:    status.Health.Green,
		wasGreen: m.rememberGreen(record.EntryID, record.PID, status.Health.Green),
		// Nothing to download: the phase rule reads this as a model that is all
		// there, which for the router is the truth — it has none.
		cached: true,
	})
	return status, nil
}

// routerCommand composes the argv that runs llama-server as this host's router:
// the preset it serves from, the address cria owns, and the router's own flags
// verbatim — the same four-flag shape an entry's command line has
// (docs/specs/CONFIG.md).
func (m *Manager) routerCommand(router config.RouterConfig, port int, report tools.Report) ([]string, error) {
	served, err := engine.For(config.BackendRouter)
	if err != nil {
		return nil, err
	}
	tool, err := engine.LaunchTool(served, report)
	if err != nil {
		return nil, err
	}

	command := append([]string{tool.Path}, engine.PresetArgs(m.routerPresetPath())...)
	command = append(command, "--host", router.Host, "--port", strconv.Itoa(port))
	return append(command, router.RouterArgs...), nil
}

// refuseIfRouterRunning holds the one-router-per-host rule. A record cria cannot
// read refuses the start too: it names a pid cria started, and starting a second
// router while that one may still hold the port is what this rule prevents.
func (m *Manager) refuseIfRouterRunning() error {
	server, found, err := m.RouterServer()
	if err != nil {
		return fmt.Errorf("cannot tell whether the router is already running: %w", err)
	}
	if found && server.Live {
		return fmt.Errorf("the router is already running as pid %d on port %d; stop it first", server.PID, server.Port)
	}
	return nil
}

// writeRouterPreset lands the composed preset where the router will read it. It
// is written whole at every start rather than edited: what the tree says now is
// what the router serves, and a preset left over from a previous start would
// serve models the tree no longer declares.
func (m *Manager) writeRouterPreset(preset string) error {
	path := m.routerPresetPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create the router's state directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(preset), 0o644); err != nil {
		return fmt.Errorf("cannot write the router's preset %s: %w", path, err)
	}
	return nil
}

// engineRoot is where one engine's own state lives: everything that belongs to
// the engine rather than to an entry it serves.
func (m *Manager) engineRoot(id config.Backend) string {
	return engineStateRoot(m.root, id)
}

func engineStateRoot(root string, id config.Backend) string {
	return filepath.Join(root, engineStateDir, string(id))
}

// RouterStateDir is the router's own folder under one state root: its composed
// preset, its record, its logs, and the store of which models it holds
// (internal/picks). Callers outside this package resolve it through here rather
// than spelling the layout a second time.
func RouterStateDir(root string) string { return engineStateRoot(root, config.BackendRouter) }

// routerStateRoot is that folder under this manager's own root.
func (m *Manager) routerStateRoot() string { return m.engineRoot(config.BackendRouter) }

// routerPresetPath is the preset the router serves from, routerRecordPath the
// record of the process serving it, and routerLogsRoot the directory its launch
// logs are kept in.
func (m *Manager) routerPresetPath() string {
	return filepath.Join(m.engineRoot(config.BackendRouter), presetFile)
}

func (m *Manager) routerRecordPath() string {
	return filepath.Join(m.engineRoot(config.BackendRouter), recordFile)
}

func (m *Manager) routerLogsRoot() string {
	return filepath.Join(m.engineRoot(config.BackendRouter), engineLogsDir)
}
