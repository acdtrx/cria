package engine

import (
	"slices"
	"testing"

	"cria/internal/config"
)

// An mlx quantization is its own repo, so the model reference is the repo alone
// — and a quant named beside it is a mistake, not a preference
// (docs/specs/CONFIG.md).
func TestAnMLXLaunchNamesTheRepoThatIsAlreadyTheQuantization(t *testing.T) {
	engine := mlx{}
	launch := config.Launch{Repo: "mlx-community/Qwen3-30B-A3B-4bit"}
	want := []string{"--model", "mlx-community/Qwen3-30B-A3B-4bit"}

	if got := engine.ModelArgs(launch); !slices.Equal(got, want) {
		t.Errorf("the launch names its model as %v, want %v", got, want)
	}
	if engine.TakesQuant() {
		t.Error("the mlx engine takes a quantization, though its repo is already one")
	}
}

// What cria asks an mlx_lm.server: it publishes no health endpoint, so its model
// listing is the documented proof of life, and it publishes nothing at all about
// its slots (docs/specs/SERVE.md). It answers that listing before reading a
// single weight, which is what leaves a green server with a load still to pay.
func TestAnMLXServerLoadsOnItsFirstRequest(t *testing.T) {
	engine := mlx{}
	if got := engine.HealthPath(); got != "/v1/models" {
		t.Errorf("cria probes %q, want mlx_lm.server's model listing /v1/models", got)
	}
	if !engine.LoadsLazily() {
		t.Error("a green mlx server is reported as loaded, though it reads its weights on the first completion")
	}
	if path, published := engine.SlotsPath(); published {
		t.Errorf("the engine claims a slot signal at %q, and mlx_lm.server documents none", path)
	}
}
