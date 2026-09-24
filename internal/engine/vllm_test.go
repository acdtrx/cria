package engine

import (
	"slices"
	"testing"

	"cria/internal/config"
)

// A vLLM quantization is its own repo (FP8, NVFP4, AWQ…), so the model
// reference is the repo alone, named under --model after the serve subcommand
// — and a quant named beside it is a mistake, not a preference
// (docs/specs/CONFIG.md).
func TestAVLLMLaunchServesTheRepoThatIsAlreadyTheQuantization(t *testing.T) {
	engine := vllm{}
	launch := config.Launch{Repo: "Qwen/Qwen3-30B-A3B-FP8"}
	want := []string{"serve", "--model", "Qwen/Qwen3-30B-A3B-FP8"}

	if got := engine.ModelArgs(launch); !slices.Equal(got, want) {
		t.Errorf("the launch names its model as %v, want %v", got, want)
	}
	if engine.TakesQuant() {
		t.Error("the vllm engine takes a quantization, though its repo is already one")
	}
}

// What cria asks a vLLM server: its documented /health, which it answers only
// once the engine has loaded — vLLM binds its port after the weights are in, so
// a green server owes nobody a load and there is nothing to warm. It publishes
// no per-slot endpoint (docs/specs/SERVE.md).
func TestAVLLMServerIsLoadedWhenItAnswers(t *testing.T) {
	engine := vllm{}
	if engine.ID() != config.BackendVLLM {
		t.Errorf("the engine claims backend %q, want %q", engine.ID(), config.BackendVLLM)
	}
	if engine.Program() != "vllm" {
		t.Errorf("the engine runs %q, want vllm", engine.Program())
	}
	if got := engine.HealthPath(); got != "/health" {
		t.Errorf("cria probes %q, want vLLM's /health", got)
	}
	if engine.LoadsLazily() {
		t.Error("a green vllm server is reported as still owing its load, though vLLM binds its port only once loaded")
	}
	if path, published := engine.SlotsPath(); published {
		t.Errorf("the engine claims a slot signal at %q, and vLLM publishes no per-slot endpoint", path)
	}
}
