package serve

import (
	"fmt"
	"strconv"
	"strings"

	"cria/internal/config"
	"cria/internal/tools"
)

// hfTokenVar is how both servers — and the huggingface_hub library underneath
// them — read the Hugging Face credential. It travels in the environment and
// never on a command line, where the process table would publish it to every
// user on the host (CODING-RULES §9).
const hfTokenVar = "HF_TOKEN"

// ComposedCommand builds the argv that serves one entry, program first. cria owns
// exactly four flags — the model reference, the host and the port — and hands
// everything else through verbatim in the order the entry wrote it
// (docs/specs/CONFIG.md); the entry loader has already refused an args list that
// restates one of them.
//
// What varies between launches of one entry arrives resolved, in the launch: the
// repo, the quant and the args a selection composed (config.Resolve). This
// composition knows nothing about choices — it spells a command line out of facts
// that are already settled, so an entry with axes and a flat one reach it the same
// way.
//
// It is exported because the composed line is the entry's documentation: the TUI's
// detail pane shows exactly what a start would run (docs/specs/CONFIG.md), and it
// shows it by asking for the same composition Start spawns, so the two can never
// drift apart.
func ComposedCommand(entry config.Entry, launch config.Launch, report tools.Report) ([]string, error) {
	tool, err := LaunchTool(entry.Backend, report)
	if err != nil {
		return nil, err
	}

	var command []string
	switch entry.Backend {
	case config.BackendLlama:
		// Launch by Hub reference: llama-server fetches what it needs into the
		// Hugging Face cache itself, which is why the tool check refuses a build
		// old enough to keep a private one (docs/specs/TOOLS.md).
		command = []string{tool.Path, "-hf", hubReference(launch)}
	case config.BackendMLX:
		// An mlx quantization is its own repo, so there is nothing to qualify the
		// reference with.
		command = []string{tool.Path, "--model", launch.Repo}
	default:
		return nil, fmt.Errorf("entry %s names backend %q, which cria cannot launch", entry.ID, entry.Backend)
	}

	command = append(command, "--host", entry.Host, "--port", strconv.Itoa(entry.Port))
	return append(command, launch.Args...), nil
}

// hubReference spells the model a llama launch serves the way llama-server takes
// it: the repo, qualified by the quantization when there is one. Without a quant
// the server picks the repo's default (docs/specs/CONFIG.md).
func hubReference(launch config.Launch) string {
	if launch.Quant == "" {
		return launch.Repo
	}
	return launch.Repo + ":" + launch.Quant
}

// LaunchTool is the start gate: an entry can only be launched by a tool the host
// has and cria may use (docs/specs/TOOLS.md). The refusal carries the tool's own
// verdict — what its state disables and the one action that clears it — because
// the tool check already phrased both.
//
// It is exported because the gate comes before the port check in a start
// (docs/specs/SERVE.md), and both callers of that sequence — the CLI and, later,
// the TUI — have to ask it in that order rather than discover the missing tool
// from a refused spawn.
func LaunchTool(backend config.Backend, report tools.Report) (tools.Tool, error) {
	var tool tools.Tool
	switch backend {
	case config.BackendLlama:
		tool = report.LlamaServer
	case config.BackendMLX:
		tool = report.MLXLMServer
	default:
		return tools.Tool{}, fmt.Errorf("backend %q has no server program", backend)
	}
	if !tool.Usable() {
		return tools.Tool{}, fmt.Errorf("%s is %s, which disables %s; %s", tool.Name, tool.Status, tool.Disables, tool.Fix)
	}
	return tool, nil
}

// launchEnv is the environment a server is spawned with: cria's own — the server
// needs the host's PATH, HF_HOME and the rest — carrying the Hugging Face
// credential when this host holds one, so gated repos download
// (docs/specs/SERVE.md).
//
// Any inherited HF_TOKEN is dropped first: the resolved token already accounts
// for it (hubapi.Token reads the environment before the token file), so this way
// the variable is present exactly when cria has a credential to pass.
func launchEnv(environ []string, token string) []string {
	env := make([]string, 0, len(environ)+1)
	for _, variable := range environ {
		if name, _, ok := strings.Cut(variable, "="); ok && name == hfTokenVar {
			continue
		}
		env = append(env, variable)
	}
	if token != "" {
		env = append(env, hfTokenVar+"="+token)
	}
	return env
}
