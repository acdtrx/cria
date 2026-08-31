package engine

import (
	"cria/internal/config"
	"cria/internal/tools"
)

// llama serves entries with llama.cpp's llama-server: one process per entry,
// launched by Hub reference so the server fetches what it needs into the
// Hugging Face cache itself (docs/cria.md, principle 2).
type llama struct{}

// The endpoints llama-server publishes that cria asks about
// (docs/specs/SERVE.md).
const (
	// llamaHealthPath is llama-server's own: 200 once the model is loaded, 503
	// while it still is.
	llamaHealthPath = "/health"

	// llamaSlotsPath is its documented per-slot endpoint, where a server says
	// what it is working on right now.
	llamaSlotsPath = "/slots"
)

func (llama) ID() config.Backend { return config.BackendLlama }

func (llama) Program() tools.Name { return tools.LlamaServer }

func (llama) Tool(report tools.Report) (tools.Tool, bool) { return report.LlamaServer, true }

// ModelArgs launches by Hub reference: llama-server fetches what it needs into
// the Hugging Face cache itself, which is why the tool check refuses a build old
// enough to keep a private one (docs/specs/TOOLS.md).
func (llama) ModelArgs(launch config.Launch) []string {
	return []string{"-hf", hubReference(launch)}
}

func (llama) TakesQuant() bool { return true }

func (llama) HealthPath() string { return llamaHealthPath }

// LoadsLazily is false: llama-server answers 503 at /health until its model is
// in memory, so a green llama server has nothing left to load.
func (llama) LoadsLazily() bool { return false }

func (llama) SlotsPath() (string, bool) { return llamaSlotsPath, true }

// hubReference spells the model a llama launch serves the way llama-server takes
// it: the repo, qualified by the quantization when there is one. Without a quant
// the server picks the repo's default (docs/specs/CONFIG.md).
func hubReference(launch config.Launch) string {
	if launch.Quant == "" {
		return launch.Repo
	}
	return launch.Repo + ":" + launch.Quant
}
