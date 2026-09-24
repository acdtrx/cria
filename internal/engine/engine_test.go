package engine

import (
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/tools"
)

// Every backend an entry may declare resolves to the engine that claims it, and
// no two engines claim the same id.
func TestEveryBackendResolvesToItsOwnEngine(t *testing.T) {
	seen := map[config.Backend]bool{}
	for _, engine := range All() {
		if seen[engine.ID()] {
			t.Fatalf("two engines claim backend %q", engine.ID())
		}
		seen[engine.ID()] = true

		found, err := For(engine.ID())
		if err != nil {
			t.Fatalf("looking up %q: %v", engine.ID(), err)
		}
		if found.ID() != engine.ID() {
			t.Errorf("backend %q resolved to the engine of %q", engine.ID(), found.ID())
		}
	}
	if len(seen) == 0 {
		t.Fatal("cria has no engines at all")
	}
}

// The two registries are one set. config declares which engines the tree knows
// (it cannot read them from here — this package imports config, so the
// dependency only runs one way), and this package declares which of them cria
// can actually serve. An engine on one side only is a config file nothing reads,
// or an engine the tree cannot configure; either way it is silent unless
// something checks, and this is that check.
func TestTheEnginesAreExactlyTheOnesTheTreeKnows(t *testing.T) {
	known := config.Engines()
	for _, engine := range All() {
		if !slices.Contains(known, engine.ID()) {
			t.Errorf("cria has a %q engine the tree does not know; add it to config.Engines", engine.ID())
		}
	}
	for _, id := range known {
		if _, err := For(id); err != nil {
			t.Errorf("the tree knows engine %q, which cria cannot serve: %v", id, err)
		}
	}

	// An entry names the engine that serves it, so every backend a file may
	// declare has to be one of them too — a narrower set, since an engine whose
	// one server serves many entries is started as itself.
	for _, backend := range config.Backends() {
		if !slices.Contains(known, backend) {
			t.Errorf("an entry may declare backend %q, which is not an engine the tree knows", backend)
		}
	}
}

// The flag an args list may not restate is the flag its engine actually
// composes. config declares it — it is what a file is refused against — and each
// engine passes its models under it; this is the one place both are in view, so
// the two cannot drift into an args flag that silently overrides the composed
// model reference.
//
// The claim is bidirectional, because the two shapes of engine are told apart by
// exactly this: an engine that serves one entry per server names it under its
// model flag, and an engine that serves many names none and is not a backend an
// entry may declare.
func TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses(t *testing.T) {
	launch := config.Launch{Repo: "org/repo", Quant: "Q4"}

	for _, engine := range All() {
		t.Run(string(engine.ID()), func(t *testing.T) {
			args := engine.ModelArgs(launch)
			perEntry := slices.Contains(config.Backends(), engine.ID())

			if len(args) == 0 {
				if perEntry {
					t.Fatal("an entry may declare this engine, but it names no model on the command line it composes")
				}
				return
			}
			if !perEntry {
				t.Fatalf("the engine composes %v for one entry, but no entry may declare it", args)
			}
			// The model reference closes the head, under the flag right before it;
			// anything ahead of that flag is the program's own subcommand (vllm
			// serve), which names no model.
			if len(args) < 2 || args[len(args)-1] != launch.Repo && !strings.HasPrefix(args[len(args)-1], launch.Repo+":") {
				t.Fatalf("the engine composes %v, want the head to end with the model reference", args)
			}
			if flag, refused := args[len(args)-2], config.ModelFlag(engine.ID()); flag != refused {
				t.Errorf("the engine composes %q while the tree refuses %q; args may set %q and win the command line",
					flag, refused, flag)
			}
		})
	}
}

// The router's model flag is the preset it serves from, held to the tree the
// same way: the flag that turns llama-server into a router is the flag no args
// list may restate, since a second one would replace the file cria composed.
func TestTheRouterServesFromThePresetTheTreeRefuses(t *testing.T) {
	args := PresetArgs("/state/engines/router/preset.ini")
	if len(args) != 2 {
		t.Fatalf("the router is launched with %v, want a flag and the preset it names", args)
	}
	if flag, refused := args[0], config.ModelFlag(config.BackendRouter); flag != refused {
		t.Errorf("the router serves from %q while the tree refuses %q; args may set %q and win the command line",
			flag, refused, flag)
	}
	if args[1] != "/state/engines/router/preset.ini" {
		t.Errorf("the router is pointed at %q, want the preset it was given", args[1])
	}
}

// A backend nothing claims is refused, and the refusal names the ones that
// exist. The default it must never take is another engine's answers: a stranger
// inheriting llama's endpoints and warm rule would show up as a server that
// simply never comes up, with nothing on screen to say why.
func TestABackendWithNoEngineIsRefused(t *testing.T) {
	for _, backend := range []config.Backend{"sglang", "", "LLAMA"} {
		engine, err := For(backend)
		if err == nil {
			t.Fatalf("backend %q was answered by the %q engine", backend, engine.ID())
		}
		if !strings.Contains(err.Error(), "backend") {
			t.Errorf("the refusal of %q reads %v, want it to name the key that is wrong", backend, err)
		}
		for _, id := range IDs() {
			if !strings.Contains(err.Error(), id) {
				t.Errorf("the refusal of %q reads %v, want it to list %q", backend, err, id)
			}
		}
	}
}

// Every engine answers every question the lifecycle asks. A knowledge gap here
// is not a compile error — an engine can return an empty string from any of
// these — so it is a test: an engine with no health endpoint would be probed at
// the server's root and read as unhealthy forever.
func TestEveryEngineAnswersEveryQuestion(t *testing.T) {
	launch := config.Launch{Repo: "org/repo", Quant: "Q4"}
	report := tools.Report{
		LlamaServer: tools.Tool{Name: tools.LlamaServer, Status: tools.StatusFound, Path: "/bin/llama-server"},
		MLXLMServer: tools.Tool{Name: tools.MLXLMServer, Status: tools.StatusFound, Path: "/bin/mlx_lm.server"},
		VLLM:        tools.Tool{Name: tools.VLLM, Status: tools.StatusFound, Path: "/bin/vllm"},
		HF:          tools.Tool{Name: tools.HF, Status: tools.StatusFound, Path: "/bin/hf"},
	}

	for _, engine := range All() {
		t.Run(string(engine.ID()), func(t *testing.T) {
			if engine.ID() == "" {
				t.Error("the engine claims no backend id")
			}
			if path := engine.HealthPath(); !strings.HasPrefix(path, "/") {
				t.Errorf("the health endpoint is %q, want a path a server answers on", path)
			}
			// What an engine names on a per-entry command line is the one answer
			// that may legitimately be nothing; which engines those are is held by
			// TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses.
			if args := engine.ModelArgs(launch); len(args) == 1 {
				t.Errorf("the engine composes %v, which names a flag with no model after it", args)
			}

			// A program and a tool are two spellings of one fact: an engine that
			// runs a program has a finding in the tool check, and that finding is
			// its own program's.
			tool, needed := engine.Tool(report)
			if needed != (engine.Program() != "") {
				t.Errorf("the engine runs %q but reports needed=%v", engine.Program(), needed)
			}
			if needed && tool.Name != engine.Program() {
				t.Errorf("the tool check answer is %q, want the finding of %q", tool.Name, engine.Program())
			}

			if path, published := engine.SlotsPath(); published && !strings.HasPrefix(path, "/") {
				t.Errorf("the slot endpoint is %q, want a path a server answers on", path)
			}
		})
	}
}

// The gate refuses with the tool check's own words: what the tool's state
// disables, and the one action that clears it (docs/specs/TOOLS.md).
func TestTheStartGateCarriesTheToolsOwnVerdict(t *testing.T) {
	report := tools.Report{LlamaServer: tools.Tool{
		Name:     tools.LlamaServer,
		Status:   tools.StatusOutdated,
		Build:    7000,
		Disables: "starting llama entries; they stay listed, marked unstartable",
		Fix:      "upgrade llama.cpp to build 8498 or newer",
	}}

	engine, err := For(config.BackendLlama)
	if err != nil {
		t.Fatalf("looking up the llama engine: %v", err)
	}
	_, err = LaunchTool(engine, report)
	if err == nil {
		t.Fatal("the gate opened on a build cria refuses")
	}
	for _, want := range []string{"llama-server", "outdated", "marked unstartable", "upgrade llama.cpp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// A usable tool opens the gate and hands back the program a start would exec.
func TestTheStartGateHandsBackTheProgramItWillRun(t *testing.T) {
	report := tools.Report{MLXLMServer: tools.Tool{
		Name: tools.MLXLMServer, Status: tools.StatusFound, Path: "/opt/homebrew/bin/mlx_lm.server",
	}}

	engine, err := For(config.BackendMLX)
	if err != nil {
		t.Fatalf("looking up the mlx engine: %v", err)
	}
	tool, err := LaunchTool(engine, report)
	if err != nil {
		t.Fatalf("the gate refused a tool the host has: %v", err)
	}
	if tool.Path != "/opt/homebrew/bin/mlx_lm.server" {
		t.Errorf("the gate handed back %q, want the resolved program", tool.Path)
	}
}

// An engine that runs no program of its own has nothing to gate on, and an
// empty tool check does not refuse it: "there is no program to look for" is an
// answer, and it must not read as "the program is missing".
func TestTheStartGateOpensForAnEngineThatRunsNoProgram(t *testing.T) {
	tool, err := LaunchTool(programless{}, tools.Report{})
	if err != nil {
		t.Fatalf("the gate refused an engine that needs no tool: %v", err)
	}
	if tool != (tools.Tool{}) {
		t.Errorf("the gate handed back %+v, want nothing to exec", tool)
	}
}

// The process scan looks for every engine's program, so a way of serving that
// cria knows about cannot be a foreign server it fails to recognise
// (internal/procs). Two engines running one program name it once: the scan looks
// for programs, and a program listed twice would report every foreign server of
// it twice.
func TestTheProcessScanLooksForEveryEnginesProgram(t *testing.T) {
	programs := Programs()
	for _, engine := range All() {
		program := engine.Program()
		if program == "" {
			continue
		}
		if !slices.Contains(programs, program) {
			t.Errorf("the scan does not look for %q, which the %q engine runs", program, engine.ID())
		}
	}
	if slices.Contains(programs, "") {
		t.Error("the scan looks for a program with no name")
	}
	for i, program := range programs {
		if slices.Contains(programs[:i], program) {
			t.Errorf("the scan looks for %q twice", program)
		}
	}
}

// Each engine's start window is pinned: it is the budget a --wait gives a cached
// model before reporting the start as stuck (docs/specs/SERVE.md, Start 4), and
// vLLM's is the one measured against its compile and graph capture on a DGX
// Spark — a 27B NVFP4 model took up to eight and a half minutes from cold. Every
// engine answers, and no answer is zero, which would fail every start at once.
func TestEveryEngineAnswersItsStartWindow(t *testing.T) {
	want := map[config.Backend]time.Duration{
		config.BackendLlama:  2 * time.Minute,
		config.BackendMLX:    2 * time.Minute,
		config.BackendVLLM:   15 * time.Minute,
		config.BackendRouter: 2 * time.Minute,
	}
	for _, engine := range All() {
		window, pinned := want[engine.ID()]
		if !pinned {
			t.Errorf("engine %q has no pinned start window; add it here", engine.ID())
			continue
		}
		if got := engine.StartWithin(); got != window {
			t.Errorf("engine %q starts within %s, want %s", engine.ID(), got, window)
		}
	}
}

// programless is an engine that serves through no program of its own — the
// shape the gate has to answer without calling it a missing tool. It answers
// every other question the way an engine speaking to something already running
// would.
type programless struct{}

func (programless) ID() config.Backend                   { return "programless" }
func (programless) Program() tools.Name                  { return "" }
func (programless) Tool(tools.Report) (tools.Tool, bool) { return tools.Tool{}, false }
func (programless) ModelArgs(config.Launch) []string     { return nil }
func (programless) TakesQuant() bool                     { return false }
func (programless) HealthPath() string                   { return "/health" }
func (programless) LoadsLazily() bool                    { return false }
func (programless) SlotsPath() (string, bool)            { return "", false }
func (programless) StartWithin() time.Duration           { return time.Minute }
