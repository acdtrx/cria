package engine

import (
	"cria/internal/config"
	"cria/internal/tools"
)

// router serves many models behind one llama-server. The program is the same one
// the llama engine runs, started in router mode: it supervises a child server per
// model it holds and proxies all of them on one port (verified live 2026-08-31).
// cria starts, watches and stops that one process and never touches its children
// — which models it holds is asked through its documented API
// (docs/specs/SERVE.md).
//
// No entry declares the router: an entry is a model, and a model reaches the
// router by being included in it (docs/specs/CONFIG.md). So this engine's server
// is configured by engines/router.toml alone, and the models it serves are
// ordinary entries the llama engine could serve just as well.
type router struct{}

// What llama-server in router mode publishes that cria asks about
// (docs/specs/SERVE.md).
const (
	// routerHealthPath is the supervisor's own: green once it is routing, with no
	// model loaded and none required (verified live 2026-08-31).
	routerHealthPath = "/health"

	// routerSlotsPath is the documented per-slot endpoint of the child serving
	// one model, which the router answers for the model a `?model=` names
	// (verified live 2026-08-31).
	routerSlotsPath = "/slots"

	// presetFlag is how llama-server is told to be a router at all: the ini file
	// listing the models it may serve. config declares the same flag as the one
	// an args list may not restate, and engine_test holds the two together.
	presetFlag = "--models-preset"
)

func (router) ID() config.Backend { return config.BackendRouter }

func (router) Program() tools.Name { return tools.LlamaServer }

// Tool is the llama-server finding read for a second requirement: a build that
// takes the router flags. A binary cria may otherwise use, but that predates
// router mode, would fail at the spawn with an unknown flag; the report answers
// for it up front instead (docs/specs/TOOLS.md).
func (router) Tool(report tools.Report) (tools.Tool, bool) { return report.RouterMode(), true }

// ModelArgs names nothing. One router process serves every model included in it,
// so there is no entry on its command line: what it serves is the composed
// preset, passed under PresetArgs when the process itself is launched.
func (router) ModelArgs(config.Launch) []string { return nil }

// TakesQuant is true: the router serves GGUF repos, and which quantization of a
// repo it is asked for is part of the reference — the same rule the llama engine
// answers, for the same files.
func (router) TakesQuant() bool { return true }

func (router) HealthPath() string { return routerHealthPath }

// LoadsLazily is false. The router holds no weights of its own: when it answers
// it is routing, which is the whole of what it does. A model's load happens on
// the request that needs it rather than as part of this server coming up, so
// there is nothing here for a warm to pay in advance (docs/specs/SERVE.md).
func (router) LoadsLazily() bool { return false }

func (router) SlotsPath() (string, bool) { return routerSlotsPath, true }

// PresetArgs names the preset one router process serves from, the way
// llama-server takes it — the router's answer to what the other engines spell as
// a model reference, and the flag that puts llama-server in router mode at all.
//
// It is a function of this package rather than a method on the interface: the
// preset belongs to the router process, which is started as itself, while
// ModelArgs answers for one entry's server.
func PresetArgs(path string) []string { return []string{presetFlag, path} }
