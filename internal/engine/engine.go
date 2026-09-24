// Package engine holds what cria knows about each way of serving a model: which
// program serves it, how a model is named on that program's command line, where
// its server publishes health and per-slot signals, and whether a server that
// answers has already loaded its weights.
//
// One engine per backend an entry can declare (config.Backend). The knowledge
// lives here rather than spread through the lifecycle, so a new way of serving
// is a new implementation rather than a new branch in every place that asks a
// per-backend question.
//
// Engines know; they do nothing. Spawning, signalling, probing and every request
// cria sends a server belong to internal/serve, which asks an engine what to
// compose and where to ask. That split is what keeps an engine implementable by
// something that is not a process at all.
//
// The lookup is total (For): a backend no engine claims is refused, never
// answered with another engine's endpoints, warm rule or model reference.
package engine

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/tools"
)

// Engine is one way of serving an entry. Every method answers a question about
// this engine alone — nothing here reads the config tree, the state directory or
// the host.
type Engine interface {
	// ID is the value an entry's backend key carries for this engine.
	ID() config.Backend

	// Program names the server program this engine runs, as PATH and the process
	// table spell it. Empty exactly for an engine that runs no program of its own.
	Program() tools.Name

	// Tool picks this engine's program out of a tool check. needed is false for
	// an engine that runs no program: its zero Tool is then not a missing tool
	// but nothing to look for, and the start gate opens on it (LaunchTool).
	Tool(report tools.Report) (tool tools.Tool, needed bool)

	// ModelArgs spells the model one launch serves the way this engine's server
	// takes it. It is the head of a command line: the flags cria owns and the
	// args the launch composed are appended by the caller, which is what keeps
	// that tail one composition for every engine. The head ends with the model
	// under the engine's model flag (config.ModelFlag); a program that serves
	// through a subcommand opens it with that (vllm serve).
	//
	// An engine whose one server serves every model included in it names nothing
	// here: no entry is on its command line, and no entry may declare it
	// (config.Backends). Its own launch is composed where its process is started
	// from, under the flag the tree refuses all the same (router.go, PresetArgs).
	ModelArgs(launch config.Launch) []string

	// TakesQuant reports whether a model reference under this engine is qualified
	// by a quantization. Where it is false the repo is already the quantization,
	// and a quant named alongside it is a mistake to refuse rather than a
	// preference to ignore.
	TakesQuant() bool

	// HealthPath is the documented endpoint cria asks whether this engine's
	// server is serving. It is part of that server's published API, which is the
	// whole reason a phase can be read from it rather than mined out of a log
	// (docs/cria.md, principle 6).
	HealthPath() string

	// LoadsLazily reports whether this engine's server goes green before it has
	// loaded any weights — the difference between a server that is ready when it
	// answers and one that still owes its first caller the whole load
	// (docs/specs/SERVE.md).
	LoadsLazily() bool

	// SlotsPath is where this engine's server publishes what each of its slots is
	// doing; published is false for an engine that publishes no such signal. A
	// missing signal is an answer of its own — cria reports that it cannot tell,
	// never idleness derived from silence.
	SlotsPath() (path string, published bool)

	// StartWithin is how long a server of this engine may take, from its spawn,
	// to serve a model already in the cache — the budget a wait for green is
	// bound by before it reports the start as stuck (docs/specs/SERVE.md). How
	// long a cached model takes to come up is what the engine does between
	// spawn and green, so it is the engine's to answer: bounded, because a
	// wedged start must be reported rather than waited out, and generous enough
	// that a slow but healthy start never reads as a failure. A model still
	// being fetched is bound by the network instead, and waited for under the
	// download budget, not this one.
	StartWithin() time.Duration
}

// engines is every engine cria has, in the order docs/specs/TOOLS.md presents
// the backends.
var engines = []Engine{llama{}, mlx{}, vllm{}, router{}}

// For answers which engine serves one backend.
//
// A backend no engine claims is refused rather than defaulted. A default would
// hand a stranger llama's health endpoint, llama's warm rule and llama's model
// reference with nothing on screen to say so, and every one of those is wrong in
// a way that only shows up as a server that never comes up.
func For(backend config.Backend) (Engine, error) {
	for _, engine := range engines {
		if engine.ID() == backend {
			return engine, nil
		}
	}
	return nil, fmt.Errorf("backend %q is not an engine cria has; cria serves %s", backend, strings.Join(IDs(), ", "))
}

// All lists every engine cria has. Callers that render or enumerate engines read
// this rather than spelling the set again, so a new engine reaches them by
// existing.
func All() []Engine { return slices.Clone(engines) }

// IDs names every backend an entry may declare, in the same order — the set a
// refusal lists and a schema documents.
func IDs() []string {
	ids := make([]string, 0, len(engines))
	for _, engine := range engines {
		ids = append(ids, string(engine.ID()))
	}
	return ids
}

// Programs names the server programs the engines run: what the process table is
// scanned for when cria looks for servers it did not start (internal/procs). An
// engine that runs no program of its own contributes nothing to the scan, and a
// program two engines run is named once — the scan looks for programs, and the
// llama engine and the router run the same one in two modes.
func Programs() []tools.Name {
	programs := make([]tools.Name, 0, len(engines))
	for _, engine := range engines {
		program := engine.Program()
		if program == "" || slices.Contains(programs, program) {
			continue
		}
		programs = append(programs, program)
	}
	return programs
}

// LaunchTool is the start gate: an entry can only be launched by a tool the host
// has and cria may use (docs/specs/TOOLS.md). The refusal carries the tool's own
// verdict — what its state disables and the one action that clears it — because
// the tool check already phrased both.
//
// The gate comes before the port check in a start (docs/specs/SERVE.md), so
// every caller of that sequence asks it in that order rather than discovering a
// missing tool from a refused spawn.
//
// An engine that runs no program of its own has nothing to gate on: it answers
// with the zero Tool and no refusal, which is not the same as a tool that is
// missing.
func LaunchTool(engine Engine, report tools.Report) (tools.Tool, error) {
	tool, needed := engine.Tool(report)
	if !needed {
		return tools.Tool{}, nil
	}
	if !tool.Usable() {
		return tools.Tool{}, fmt.Errorf("%s is %s, which disables %s; %s", tool.Name, tool.Status, tool.Disables, tool.Fix)
	}
	return tool, nil
}
