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

// The router serves llama entries and only llama entries: it is llama-server in
// router mode, so what it can hold is what that program serves. The refusal
// names both programs, which is the fact behind the rule.
func TestOnlyTheEntriesTheRoutersProgramServesCanBeIncluded(t *testing.T) {
	if err := RouterServes(config.BackendLlama); err != nil {
		t.Errorf("a llama entry cannot be included in the router: %v", err)
	}

	err := RouterServes(config.BackendMLX)
	if err == nil {
		t.Fatal("an mlx entry was accepted into the router, which runs llama-server")
	}
	for _, want := range []string{string(tools.MLXLMServer), string(tools.LlamaServer), string(config.BackendLlama)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %v, want it to name %q", err, want)
		}
	}

	// The router itself is not a model to include: no entry declares it.
	if err := RouterServes(config.BackendRouter); err == nil {
		t.Error("the router was accepted as one of its own models")
	}
	if err := RouterServes("sglang"); err == nil {
		t.Error("a backend cria has no engine for was accepted into the router")
	}
}

// One of the router's models is addressed by the alias its section carries — the
// entry id — on the endpoints a child answers for.
func TestARoutersModelIsAddressedByItsAlias(t *testing.T) {
	if got, want := RouterModelQuery("/slots", "qwen-q6"), "/slots?model=qwen-q6"; got != want {
		t.Errorf("cria addresses a child at %q, want %q", got, want)
	}
	if got, want := RouterModelQuery("/props", "lfm 2.5"), "/props?model=lfm+2.5"; got != want {
		t.Errorf("a name with a space is addressed as %q, want it escaped: %q", got, want)
	}
}
