package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/tools"
)

// The composed command line is the contract between a config entry and the
// server it launches (docs/specs/CONFIG.md): cria's four flags in a fixed order,
// then the args the launch composed — the entry's own, then the picked options',
// in the entry's choice order.
func TestComposedCommand(t *testing.T) {
	report := usableReport()
	cases := []struct {
		name      string
		entry     config.Entry
		selection config.Selection
		want      []string
	}{
		{
			name:  "llama names its quantization on the hub reference",
			entry: llamaEntry(),
			want: []string{
				"/opt/homebrew/bin/llama-server",
				"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
				"--host", "0.0.0.0",
				"--port", "8080",
				"--ctx-size", "16384",
			},
		},
		{
			name: "llama without a quantization hands the bare repo over",
			entry: config.Entry{
				ID: "qwen", Backend: config.BackendLlama,
				Repo: "unsloth/Qwen3-30B-A3B-GGUF", Host: "127.0.0.1", Port: 9000,
			},
			want: []string{
				"/opt/homebrew/bin/llama-server",
				"-hf", "unsloth/Qwen3-30B-A3B-GGUF",
				"--host", "127.0.0.1",
				"--port", "9000",
			},
		},
		{
			name: "mlx serves a repo, which is already the quantization",
			entry: config.Entry{
				ID: "qwen-mlx", Backend: config.BackendMLX,
				Repo: "mlx-community/Qwen3-30B-A3B-4bit", Host: "0.0.0.0", Port: 8080,
				Args: []string{"--max-tokens", "4096"},
			},
			want: []string{
				"/opt/homebrew/bin/mlx_lm.server",
				"--model", "mlx-community/Qwen3-30B-A3B-4bit",
				"--host", "0.0.0.0",
				"--port", "8080",
				"--max-tokens", "4096",
			},
		},
		{
			name:      "an entry with axes composes the picked options after its own args",
			entry:     choicesEntry(),
			selection: config.Selection{"quant": "q6", "context": "long"},
			want: []string{
				"/opt/homebrew/bin/llama-server",
				"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL",
				"--host", "0.0.0.0",
				"--port", "8080",
				"--ctx-size", "16384",
				"--n-cpu-moe", "12",
				"--cache-type-k", "f16",
			},
		},
		{
			name:  "the config defaults are the first option of each axis",
			entry: choicesEntry(),
			want: []string{
				"/opt/homebrew/bin/llama-server",
				"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
				"--host", "0.0.0.0",
				"--port", "8080",
				"--ctx-size", "16384",
				"--cache-type-k", "q8_0",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			selection := test.selection
			if selection == nil {
				selection = config.DefaultSelection(test.entry)
			}
			launch, err := config.Resolve(test.entry, selection)
			if err != nil {
				t.Fatalf("resolving: %v", err)
			}
			got, err := ComposedCommand(test.entry, launch, report)
			if err != nil {
				t.Fatalf("composing: %v", err)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("composed\n  %v\nwant\n  %v", got, test.want)
			}
		})
	}
}

// A real profile's flags reach the server exactly as its files wrote them. The
// argv below is one the user's own tree serves today, token for token, aliases
// and all — nothing on it is a spelling cria chose (docs/plans/engines).
//
// The second tree is that same profile with the machine's own flags lifted into
// engines/llama.toml, one of them (-c) overridden by the entry: extraction moves
// where a flag is written, never what the server receives. Both trees compose
// the identical line, which is what makes an extraction safe to do by hand.
//
// It goes through the real loader rather than a built entry: the files, the
// merge and the composition are all part of what has to add up.
func TestAProfilesArgsReachTheServerVerbatim(t *testing.T) {
	// llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0
	//   --port 8080 -ngl 99 -fa on -c 262144 --parallel 1 --jinja --n-cpu-moe 24
	served := []string{
		"/opt/homebrew/bin/llama-server",
		"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"--host", "0.0.0.0",
		"--port", "8080",
		"-ngl", "99",
		"-fa", "on",
		"-c", "262144",
		"--parallel", "1",
		"--jinja",
		"--n-cpu-moe", "24",
	}

	tests := []struct {
		name  string
		files map[string]string
	}{
		{
			// One file, every flag where the author typed it.
			name: "a profile that carries all its own flags",
			files: map[string]string{
				"models/qwen.toml": `backend = "llama"
repo = "unsloth/Qwen3-30B-A3B-GGUF"
quant = "UD-Q4_K_XL"
port = 8080
args = [
  "-ngl", "99",
  "-fa", "on",
  # 262144 tokens, the whole window for a single slot
  "-c", "262144",
  "--parallel", "1",
  "--jinja",
]

[[choice]]
name = "offload"
  [[choice.option]]
  name = "cpu"
  args = ["--n-cpu-moe", "24"]
`,
			},
		},
		{
			// The machine's flags in the engine file, including a -c this model
			// overrides: the entry's value lands where the engine's stood, so the
			// line reads the same as the flat profile's.
			name: "a profile whose machine-wide flags moved to the engine file",
			files: map[string]string{
				"engines/llama.toml": "args = [\"-ngl\", \"99\", \"-fa\", \"on\", \"-c\", \"8192\"]\n",
				"models/qwen.toml": `backend = "llama"
repo = "unsloth/Qwen3-30B-A3B-GGUF"
quant = "UD-Q4_K_XL"
port = 8080
args = ["-c", "262144", "--parallel", "1", "--jinja"]

[[choice]]
name = "offload"
  [[choice.option]]
  name = "cpu"
  args = ["--n-cpu-moe", "24"]
`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range test.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatalf("cannot write %s: %v", path, err)
				}
			}

			tree, err := config.Load(root)
			if err != nil {
				t.Fatalf("loading the tree: %v", err)
			}
			if len(tree.Broken) != 0 {
				t.Fatalf("the profile was refused: %v", tree.Broken[0].Err)
			}
			entry, found := tree.Entry("qwen")
			if !found {
				t.Fatalf("the tree holds %+v, want the qwen entry", tree.Entries)
			}

			launch, err := config.Resolve(entry, config.DefaultSelection(entry))
			if err != nil {
				t.Fatalf("resolving: %v", err)
			}
			got, err := ComposedCommand(entry, launch, usableReport())
			if err != nil {
				t.Fatalf("composing: %v", err)
			}
			if !slices.Equal(got, served) {
				t.Errorf("the profile composes\n  %v\nwant the line its files wrote\n  %v", got, served)
			}
		})
	}
}

// The argv a start spawned is what its record file holds: the file is where a
// later invocation reads back what is running (docs/specs/SERVE.md), so the
// composition reaches the outside as bytes on disk rather than as a return
// value. Both backends launch by Hub reference and differ only in how that
// reference is spelled — llama qualifies the repo with the quantization behind
// -hf, an mlx quantization is its own repo behind --model.
func TestTheRecordFileHoldsTheArgvThatWasSpawned(t *testing.T) {
	cases := []struct {
		name  string
		entry config.Entry
		pid   int
		want  []string
	}{
		{
			name:  "a llama server carries the quantization on its hub reference",
			entry: llamaEntry(),
			pid:   4242,
			want: []string{
				"/opt/homebrew/bin/llama-server",
				"-hf", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
				"--host", "0.0.0.0",
				"--port", "8080",
				"--ctx-size", "16384",
			},
		},
		{
			name:  "an mlx server is launched by the repo that is already the quantization",
			entry: mlxEntry(),
			pid:   4243,
			want: []string{
				"/opt/homebrew/bin/mlx_lm.server",
				"--model", "mlx-community/Qwen3-30B-A3B-4bit",
				"--host", "0.0.0.0",
				"--port", "8080",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			host := &fakeHost{}
			manager := newManager(t, host)
			_, spawner := startOne(t, manager, host, test.entry, test.pid)

			if !slices.Equal(spawner.last().Command, test.want) {
				t.Errorf("the launch ran\n  %v\nwant\n  %v", spawner.last().Command, test.want)
			}

			written, err := os.ReadFile(manager.recordPath(test.entry.ID))
			if err != nil {
				t.Fatalf("reading the record file: %v", err)
			}
			var file struct {
				Command []string `json:"command"`
			}
			if err := json.Unmarshal(written, &file); err != nil {
				t.Fatalf("the record file is not the JSON a later invocation reads: %v", err)
			}
			if !slices.Equal(file.Command, test.want) {
				t.Errorf("the record file holds\n  %v\nwant\n  %v", file.Command, test.want)
			}
		})
	}
}

// Composing an entry's command line is also the start gate: a backend whose tool
// the host does not have, or has in a build cria refuses, cannot be launched —
// and the refusal carries the tool check's own words (docs/specs/TOOLS.md).
func TestStartGateRefusesAnUnusableTool(t *testing.T) {
	cases := []struct {
		name   string
		entry  config.Entry
		report tools.Report
		want   []string
	}{
		{
			name:  "llama-server missing",
			entry: llamaEntry(),
			report: tools.Report{LlamaServer: tools.Tool{
				Name: tools.LlamaServer, Status: tools.StatusMissing,
				Disables: "starting llama entries; they stay listed, marked unstartable",
				Fix:      "install llama.cpp so llama-server is on PATH",
			}},
			want: []string{"llama-server", "missing", "install llama.cpp"},
		},
		{
			name:  "llama-server too old for the hub cache",
			entry: llamaEntry(),
			report: tools.Report{LlamaServer: tools.Tool{
				Name: tools.LlamaServer, Status: tools.StatusOutdated, Path: "/usr/local/bin/llama-server",
				Build:    7000,
				Disables: "starting llama entries; they stay listed, marked unstartable",
				Fix:      "upgrade llama.cpp to build 8498 or newer",
			}},
			want: []string{"llama-server", "outdated", "upgrade llama.cpp"},
		},
		{
			name:  "mlx_lm.server missing",
			entry: config.Entry{ID: "m", Backend: config.BackendMLX, Repo: "mlx-community/x", Host: "0.0.0.0", Port: 8080},
			report: tools.Report{MLXLMServer: tools.Tool{
				Name: tools.MLXLMServer, Status: tools.StatusMissing,
				Disables: "starting mlx entries; they stay listed, marked unstartable",
				Fix:      "install mlx-lm so mlx_lm.server is on PATH (Apple silicon only)",
			}},
			want: []string{"mlx_lm.server", "missing", "install mlx-lm"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			launch := config.Launch{Repo: test.entry.Repo, Quant: test.entry.Quant, Args: test.entry.Args}
			_, err := ComposedCommand(test.entry, launch, test.report)
			if err == nil {
				t.Fatal("an unusable tool composed a command line")
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q: %v", want, err)
				}
			}

			// And the same refusal has to stop a start, before anything is spawned.
			manager := newManager(t, &fakeHost{})
			spawner := &fakeSpawner{pid: 4242}
			manager.spawn = spawner.launch
			if _, err := manager.Start(test.entry, config.DefaultSelection(test.entry), test.report); err == nil {
				t.Fatal("an entry started on a tool cria refuses")
			}
			if len(spawner.launches) != 0 {
				t.Errorf("the refused start spawned %d processes", len(spawner.launches))
			}
		})
	}
}

// The Hugging Face credential reaches a server through its environment and
// nowhere else: an argument would publish it to every process listing on the
// host (CODING-RULES §9).
func TestTheTokenTravelsInTheEnvironment(t *testing.T) {
	t.Setenv("HF_TOKEN", "hf_a_secret_value")

	host := &fakeHost{}
	manager := newManager(t, host)
	record, spawner := startOne(t, manager, host, llamaEntry(), 4242)

	env := spawner.last().Env
	if count := countPrefix(env, "HF_TOKEN="); count != 1 {
		t.Fatalf("the environment carries HF_TOKEN %d times, want once: %v", count, redact(env))
	}
	if !slices.Contains(env, "HF_TOKEN=hf_a_secret_value") {
		t.Errorf("the environment does not carry the resolved token: %v", redact(env))
	}
	for _, argument := range record.Command {
		if strings.Contains(argument, "hf_a_secret_value") {
			t.Fatalf("the token reached the command line: %v", record.Command)
		}
	}
}

// No credential on the host is the normal case: the variable is then absent
// rather than empty, so a server never sees a token it cannot use.
func TestNoTokenLeavesTheVariableUnset(t *testing.T) {
	t.Setenv("HF_TOKEN", "")
	// hubapi falls back to the token file under the Hugging Face home; pointing
	// that at an empty directory is a host nobody has logged in on.
	t.Setenv("HF_HOME", t.TempDir())

	host := &fakeHost{}
	manager := newManager(t, host)
	_, spawner := startOne(t, manager, host, llamaEntry(), 4242)

	if count := countPrefix(spawner.last().Env, "HF_TOKEN="); count != 0 {
		t.Errorf("the environment carries HF_TOKEN %d times on a host with no token", count)
	}
}

// A server inherits the environment cria was started with — its PATH, its
// Hugging Face home, everything the tools resolve against.
func TestTheServerInheritsTheEnvironment(t *testing.T) {
	t.Setenv("CRIA_SERVE_INHERITED", "yes")

	host := &fakeHost{}
	manager := newManager(t, host)
	_, spawner := startOne(t, manager, host, llamaEntry(), 4242)

	if !slices.Contains(spawner.last().Env, "CRIA_SERVE_INHERITED=yes") {
		t.Error("the launch environment dropped a variable cria was started with")
	}
}

func countPrefix(env []string, prefix string) int {
	count := 0
	for _, variable := range env {
		if strings.HasPrefix(variable, prefix) {
			count++
		}
	}
	return count
}

// redact keeps a failing environment printable without spilling the credential
// the test just set.
func redact(env []string) []string {
	shown := make([]string, 0, len(env))
	for _, variable := range env {
		if name, _, ok := strings.Cut(variable, "="); ok && name == hfTokenVar {
			shown = append(shown, name+"=<set>")
			continue
		}
		shown = append(shown, variable)
	}
	return shown
}
