package engine

import (
	"slices"
	"strings"
	"testing"

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

// The two registries are one set. config declares which backends a file may
// name (it cannot read them from here — this package imports config, so the
// dependency only runs one way), and this package declares which of them cria
// can actually serve. A backend on one side only is a config the parser accepts
// and nothing serves, or an engine no entry can reach; either way it is silent
// unless something checks, and this is that check.
func TestTheEnginesAreExactlyTheBackendsTheTreeMayDeclare(t *testing.T) {
	declared := config.Backends()
	for _, engine := range All() {
		if !slices.Contains(declared, engine.ID()) {
			t.Errorf("the %q engine serves a backend no entry may declare; add it to config.Backends", engine.ID())
		}
	}
	for _, backend := range declared {
		if _, err := For(backend); err != nil {
			t.Errorf("an entry may declare backend %q, which no engine serves: %v", backend, err)
		}
	}
}

// The flag an args list may not restate is the flag its engine actually
// composes. config declares it — it is what a file is refused against — and each
// engine passes the model under it; this is the one place both are in view, so
// the two cannot drift into an args flag that silently overrides the composed
// model reference.
func TestEveryEnginesModelFlagIsTheFlagTheTreeRefuses(t *testing.T) {
	launch := config.Launch{Repo: "org/repo", Quant: "Q4"}

	for _, engine := range All() {
		t.Run(string(engine.ID()), func(t *testing.T) {
			args := engine.ModelArgs(launch)
			if len(args) == 0 {
				t.Fatal("the engine names no model on the command line it composes")
			}
			if flag, refused := args[0], config.ModelFlag(engine.ID()); flag != refused {
				t.Errorf("the engine composes %q while the tree refuses %q; args may set %q and win the command line",
					flag, refused, flag)
			}
		})
	}
}

// A backend nothing claims is refused, and the refusal names the ones that
// exist. The default it must never take is another engine's answers: a stranger
// inheriting llama's endpoints and warm rule would show up as a server that
// simply never comes up, with nothing on screen to say why.
func TestABackendWithNoEngineIsRefused(t *testing.T) {
	for _, backend := range []config.Backend{"vllm", "", "LLAMA"} {
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
			if args := engine.ModelArgs(launch); len(args) == 0 {
				t.Error("the engine names no model on the command line it composes")
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
// (internal/procs).
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
