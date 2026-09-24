package engine

import (
	"time"

	"cria/internal/config"
	"cria/internal/tools"
)

// vllm serves entries with vLLM's OpenAI-compatible server, `vllm serve`: one
// process per entry, launched by repo — a vLLM quantization (FP8, NVFP4, AWQ…)
// is its own repo, so there is nothing to qualify the reference with
// (docs/cria.md, principle 2).
//
// The server is two processes: the API server cria spawns and an engine core
// it starts in the same process group, which is why a stop signals the group
// (docs/specs/SERVE.md).
type vllm struct{}

// vllmHealthPath is vLLM's documented health endpoint.
const vllmHealthPath = "/health"

func (vllm) ID() config.Backend { return config.BackendVLLM }

func (vllm) Program() tools.Name { return tools.VLLM }

func (vllm) Tool(report tools.Report) (tools.Tool, bool) { return report.VLLM, true }

// ModelArgs names the repo, which is already the quantization, after the serve
// subcommand. `vllm serve` takes the model positionally or as --model; cria
// composes the flag, so the backend has a model flag an args list may not
// restate like every other (internal/config).
func (vllm) ModelArgs(launch config.Launch) []string {
	return []string{"serve", "--model", launch.Repo}
}

func (vllm) TakesQuant() bool { return false }

func (vllm) HealthPath() string { return vllmHealthPath }

// LoadsLazily is false: vLLM binds its port only once the engine has loaded the
// weights and captured its graphs, so a server that answers has nothing left to
// load and there is nothing to warm.
func (vllm) LoadsLazily() bool { return false }

// SlotsPath publishes nothing cria reads: vLLM has no per-slot endpoint, and its
// Prometheus counters are a different signal, not read today.
func (vllm) SlotsPath() (string, bool) { return "", false }

// StartWithin is fifteen minutes. vLLM binds its port only after the load,
// torch.compile, CUDA-graph capture and memory profiling: measured on a DGX
// Spark with a 27B NVFP4 model at 256K context, green took about five minutes
// with warm compile caches and six to eight and a half with cold ones. A larger
// model compiles longer, so the budget is well above the measured worst and
// still bounded.
func (vllm) StartWithin() time.Duration { return 15 * time.Minute }
