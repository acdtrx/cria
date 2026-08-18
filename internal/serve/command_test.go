package serve

import (
	"slices"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/tools"
)

// The composed command line is the contract between a config entry and the
// server it launches (docs/specs/CONFIG.md): cria's four flags in a fixed order,
// then the entry's own args verbatim.
func TestComposedCommand(t *testing.T) {
	report := usableReport()
	cases := []struct {
		name  string
		entry config.Entry
		want  []string
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
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := composeCommand(test.entry, report)
			if err != nil {
				t.Fatalf("composing: %v", err)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("composed\n  %v\nwant\n  %v", got, test.want)
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
			_, err := composeCommand(test.entry, test.report)
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
			if _, err := manager.Start(test.entry, test.report); err == nil {
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
