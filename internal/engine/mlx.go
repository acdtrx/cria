package engine

import (
	"time"

	"cria/internal/config"
	"cria/internal/tools"
)

// mlx serves entries with mlx-lm's mlx_lm.server on Apple silicon: one process
// per entry, launched by repo — an mlx quantization is its own repo, so there is
// nothing to qualify the reference with (docs/cria.md, principle 2).
type mlx struct{}

// mlxHealthPath is mlx_lm.server's model listing. It publishes no health
// endpoint, and the listing is the documented proof of life
// (docs/specs/SERVE.md).
const mlxHealthPath = "/v1/models"

func (mlx) ID() config.Backend { return config.BackendMLX }

func (mlx) Program() tools.Name { return tools.MLXLMServer }

func (mlx) Tool(report tools.Report) (tools.Tool, bool) { return report.MLXLMServer, true }

// ModelArgs names the repo, which is already the quantization.
func (mlx) ModelArgs(launch config.Launch) []string {
	return []string{"--model", launch.Repo}
}

func (mlx) TakesQuant() bool { return false }

func (mlx) HealthPath() string { return mlxHealthPath }

// LoadsLazily is true: mlx_lm.server lists its models the moment its HTTP
// listener is up and reads the weights on the first completion instead, so a
// green mlx server still owes its first caller the whole load.
func (mlx) LoadsLazily() bool { return true }

// SlotsPath publishes nothing: mlx_lm.server documents no per-slot signal, and
// deriving one from something adjacent would hand back a guess spelled like a
// measurement.
func (mlx) SlotsPath() (string, bool) { return "", false }

// StartWithin is two minutes: mlx_lm.server binds its port before it reads any
// weights, so its green costs only the process coming up — the load itself is
// the warm's, under that request's own budget.
func (mlx) StartWithin() time.Duration { return 2 * time.Minute }
