package engine

import (
	"slices"
	"testing"

	"cria/internal/config"
)

// llama-server is launched by Hub reference, and the quantization is how that
// reference is qualified: with one, the repo carries it after a colon; without
// one, the bare repo lets the server pick the repo's default
// (docs/specs/CONFIG.md).
func TestALlamaLaunchNamesItsQuantizationOnTheHubReference(t *testing.T) {
	tests := []struct {
		name   string
		launch config.Launch
		want   []string
	}{
		{
			name:   "a quantized launch qualifies the repo",
			launch: config.Launch{Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
			want:   []string{"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL"},
		},
		{
			name:   "without a quantization the bare repo is handed over",
			launch: config.Launch{Repo: "unsloth/Qwen3-30B-A3B-GGUF"},
			want:   []string{"-hf", "unsloth/Qwen3-30B-A3B-GGUF"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := (llama{}).ModelArgs(test.launch); !slices.Equal(got, test.want) {
				t.Errorf("the launch names its model as %v, want %v", got, test.want)
			}
		})
	}
	if !(llama{}).TakesQuant() {
		t.Error("the llama engine does not take a quantization, though its reference carries one")
	}
}

// What cria asks a llama-server, spelled out rather than composed: /health is
// the endpoint a phase is read from, and /slots is where the server says what it
// is working on (docs/specs/SERVE.md). A green /health is a model already in
// memory, so there is nothing left to warm.
func TestALlamaServerIsReadyWhenItAnswers(t *testing.T) {
	engine := llama{}
	if got := engine.HealthPath(); got != "/health" {
		t.Errorf("cria probes %q, want llama-server's documented health endpoint /health", got)
	}
	if engine.LoadsLazily() {
		t.Error("a green llama server is reported as still having weights to load")
	}
	path, published := engine.SlotsPath()
	if !published || path != "/slots" {
		t.Errorf("the slot signal reads (%q, %v), want llama-server's documented /slots", path, published)
	}
}
