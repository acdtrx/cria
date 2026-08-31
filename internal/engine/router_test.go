package engine

import (
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/tools"
)

// fitLlamaServer is a host whose llama-server cria may use, with router mode
// present or not — the one fact the router engine reads differently from the
// llama engine.
func fitLlamaServer(router bool) tools.Report {
	return tools.Report{LlamaServer: tools.Tool{
		Name:   tools.LlamaServer,
		Status: tools.StatusFound,
		Path:   "/opt/homebrew/bin/llama-server",
		Build:  10450,
		Router: router,
	}}
}

// The start gate refuses a llama-server that cannot be a router, with what the
// binary lacks — rather than letting the spawn fail on an unknown flag, which
// reads as a server that died on its first breath (docs/specs/TOOLS.md).
func TestTheGateRefusesALlamaServerWithoutRouterMode(t *testing.T) {
	served, err := For(config.BackendRouter)
	if err != nil {
		t.Fatalf("looking up the router engine: %v", err)
	}

	if _, err := LaunchTool(served, fitLlamaServer(false)); err == nil {
		t.Fatal("the gate opened on a build that takes no router flags")
	} else {
		for _, want := range []string{"llama-server", tools.RouterFlag, "upgrade llama.cpp"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not carry %q: %v", want, err)
			}
		}
	}

	tool, err := LaunchTool(served, fitLlamaServer(true))
	if err != nil {
		t.Fatalf("the gate refused a build that takes the router flags: %v", err)
	}
	if tool.Path != "/opt/homebrew/bin/llama-server" {
		t.Errorf("the gate handed back %q, want the program it will run", tool.Path)
	}
}

// The router runs the same program as the llama engine, in another mode. That is
// why one tool check answers for both, and why the process scan looks for one
// program rather than two.
func TestTheRouterRunsLlamaServerInAnotherMode(t *testing.T) {
	served, err := For(config.BackendRouter)
	if err != nil {
		t.Fatalf("looking up the router engine: %v", err)
	}
	llama, err := For(config.BackendLlama)
	if err != nil {
		t.Fatalf("looking up the llama engine: %v", err)
	}

	if served.Program() != llama.Program() {
		t.Errorf("the router runs %q and the llama engine %q, want one program", served.Program(), llama.Program())
	}
	if args := served.ModelArgs(config.Launch{Repo: "org/repo", Quant: "Q4"}); args != nil {
		t.Errorf("the router composes %v for one entry; it serves the models included in it, and no entry declares it", args)
	}
	if !served.TakesQuant() {
		t.Error("the router serves GGUF repos, whose references name a quantization")
	}
	if served.LoadsLazily() {
		t.Error("the router holds no weights of its own, so a green router has nothing left to load")
	}
	if path, published := served.SlotsPath(); !published || path != llamaSlotsPath {
		t.Errorf("the router publishes slots at %q (%v), want the endpoint its children answer", path, published)
	}
}
