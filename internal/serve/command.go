package serve

import (
	"fmt"
	"strconv"
	"strings"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/tools"
)

// hfTokenVar is how both servers — and the huggingface_hub library underneath
// them — read the Hugging Face credential. It travels in the environment and
// never on a command line, where the process table would publish it to every
// user on the host (CODING-RULES §9).
const hfTokenVar = "HF_TOKEN"

// ComposedCommand builds the argv that serves one entry, program first. cria owns
// exactly four flags — the model reference, the host and the port — and spells
// the launch's own args out of the keys the tree merged for it
// (docs/specs/CONFIG.md); the entry loader has already refused an args list that
// restates one of the four.
//
// How the model is named is the engine's knowledge (internal/engine) and the
// rest of the line is not: the flags cria owns and the args the launch composed
// are one tail for every engine, which is what stops each new way of serving
// from respelling them.
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
	served, err := engine.For(entry.Backend)
	if err != nil {
		return nil, fmt.Errorf("entry %s: %w", entry.ID, err)
	}
	tool, err := engine.LaunchTool(served, report)
	if err != nil {
		return nil, err
	}

	command := append([]string{tool.Path}, served.ModelArgs(launch)...)
	command = append(command, "--host", entry.Host, "--port", strconv.Itoa(entry.Port))
	return append(command, engine.Flags(launch.Args)...), nil
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
