package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The entry contract, rule by rule (docs/specs/CONFIG.md). Each case is a whole
// entry file so the rules are exercised the way a real tree hits them.

func TestEntryRulesAccept(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		entry    string
		want     Entry
	}{
		{
			name:  "minimal llama entry",
			entry: "backend = \"llama\"\nrepo = \"unsloth/Qwen3-30B-A3B-GGUF\"\nport = 8080\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
			},
		},
		{
			name:  "minimal mlx entry",
			entry: "backend = \"mlx\"\nrepo = \"mlx-community/Qwen3-30B-A3B-4bit\"\nport = 8080\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendMLX,
				Repo:    "mlx-community/Qwen3-30B-A3B-4bit",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
			},
		},
		{
			name:  "quant is allowed on llama",
			entry: "backend = \"llama\"\nrepo = \"unsloth/Qwen3-30B-A3B-GGUF\"\nquant = \"Q4_K_M\"\nport = 8080\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
				Quant:   "Q4_K_M",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
			},
		},
		{
			name:     "every key set at once",
			settings: "default_port = 9000\ndefault_host = \"127.0.0.1\"\n",
			entry: "backend = \"llama\"\nrepo = \"unsloth/Qwen3-30B-A3B-GGUF\"\nquant = \"Q4_K_M\"\n" +
				"port = 8080\nhost = \"192.168.1.10\"\nname = \"Qwen3 30B\"\nargs = [\"--ctx-size\", \"16384\"]\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
				Quant:   "Q4_K_M",
				Port:    8080,
				Host:    "192.168.1.10",
				Name:    "Qwen3 30B",
				Args:    []string{"--ctx-size", "16384"},
			},
		},
		{
			name:     "port falls back to default_port",
			settings: "default_port = 9000\n",
			entry:    "backend = \"llama\"\nrepo = \"org/name\"\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "org/name",
				Port:    9000,
				Host:    "0.0.0.0",
				Name:    "demo",
			},
		},
		{
			name:     "host falls back to default_host",
			settings: "default_port = 9000\ndefault_host = \"127.0.0.1\"\n",
			entry:    "backend = \"mlx\"\nrepo = \"org/name\"\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendMLX,
				Repo:    "org/name",
				Port:    9000,
				Host:    "127.0.0.1",
				Name:    "demo",
			},
		},
		{
			name:     "an entry overrides both defaults",
			settings: "default_port = 9000\ndefault_host = \"127.0.0.1\"\n",
			entry:    "backend = \"mlx\"\nrepo = \"org/name\"\nport = 8080\nhost = \"0.0.0.0\"\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendMLX,
				Repo:    "org/name",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
			},
		},
		{
			name:  "args that name no composed flag pass through verbatim",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--ctx-size\", \"16384\", \"--flash-attn\", \"-ngl\", \"99\"]\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "org/name",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Args:    []string{"--ctx-size", "16384", "--flash-attn", "-ngl", "99"},
			},
		},
		{
			name:  "an empty args list is not an omission",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = []\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "org/name",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Args:    []string{},
			},
		},
		{
			name:  "repo names may hold dots, dashes and underscores",
			entry: "backend = \"llama\"\nrepo = \"My-Org_1/Model.v2-GGUF\"\nport = 8080\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "My-Org_1/Model.v2-GGUF",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
			},
		},
		{
			name: "an entry that varies on two axes",
			entry: "backend = \"llama\"\nrepo = \"unsloth/Qwen3-30B-A3B-GGUF\"\nquant = \"UD-Q4_K_XL\"\nport = 8080\n" +
				"args = [\"--jinja\"]\n" +
				"[[choice]]\nname = \"quant\"\n" +
				"  [[choice.option]]\n  name = \"q4\"\n  quant = \"UD-Q4_K_XL\"\n  args = [\"--ctx-size\", \"32768\"]\n" +
				"  [[choice.option]]\n  name = \"q8\"\n  quant = \"Q8_0\"\n  args = [\"--ctx-size\", \"16384\"]\n" +
				"[[choice]]\nname = \"slots\"\n" +
				"  [[choice.option]]\n  name = \"one\"\n" +
				"  [[choice.option]]\n  name = \"four\"\n  args = [\"--parallel\", \"4\"]\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "unsloth/Qwen3-30B-A3B-GGUF",
				Quant:   "UD-Q4_K_XL",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Args:    []string{"--jinja"},
				Choices: []Choice{
					{
						Name: "quant",
						Options: []ChoiceOption{
							{Name: "q4", Quant: "UD-Q4_K_XL", Args: []string{"--ctx-size", "32768"}},
							{Name: "q8", Quant: "Q8_0", Args: []string{"--ctx-size", "16384"}},
						},
					},
					{
						Name: "slots",
						Options: []ChoiceOption{
							{Name: "one"},
							{Name: "four", Args: []string{"--parallel", "4"}},
						},
					},
				},
			},
		},
		{
			name: "a one-option choice is a named block of args",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n" +
				"[[choice]]\nname = \"debug\"\n  [[choice.option]]\n  name = \"verbose\"\n  args = [\"--verbose\"]\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "org/name",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Choices: []Choice{{
					Name:    "debug",
					Options: []ChoiceOption{{Name: "verbose", Args: []string{"--verbose"}}},
				}},
			},
		},
		{
			name: "an mlx entry's options replace the repo, since a quantization is one",
			entry: "backend = \"mlx\"\nrepo = \"mlx-community/Qwen3-30B-A3B-4bit\"\nport = 8080\n" +
				"[[choice]]\nname = \"quant\"\n" +
				"  [[choice.option]]\n  name = \"4bit\"\n  repo = \"mlx-community/Qwen3-30B-A3B-4bit\"\n" +
				"  [[choice.option]]\n  name = \"8bit\"\n  repo = \"mlx-community/Qwen3-30B-A3B-8bit\"\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendMLX,
				Repo:    "mlx-community/Qwen3-30B-A3B-4bit",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Choices: []Choice{{
					Name: "quant",
					Options: []ChoiceOption{
						{Name: "4bit", Repo: "mlx-community/Qwen3-30B-A3B-4bit"},
						{Name: "8bit", Repo: "mlx-community/Qwen3-30B-A3B-8bit"},
					},
				}},
			},
		},
		{
			name: "options of one choice share flags freely — they are alternatives",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n" +
				"[[choice]]\nname = \"ctx\"\n" +
				"  [[choice.option]]\n  name = \"short\"\n  args = [\"--ctx-size\", \"8192\"]\n" +
				"  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "org/name",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Choices: []Choice{{
					Name: "ctx",
					Options: []ChoiceOption{
						{Name: "short", Args: []string{"--ctx-size", "8192"}},
						{Name: "long", Args: []string{"--ctx-size", "65536"}},
					},
				}},
			},
		},
		{
			name: "the same value in two axes is not a collision; only flags are compared",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--seed\", \"-1\"]\n" +
				"[[choice]]\nname = \"sampling\"\n  [[choice.option]]\n  name = \"greedy\"\n  args = [\"--top-k\", \"-1\"]\n",
			want: Entry{
				ID:      "demo",
				Backend: BackendLlama,
				Repo:    "org/name",
				Port:    8080,
				Host:    "0.0.0.0",
				Name:    "demo",
				Args:    []string{"--seed", "-1"},
				Choices: []Choice{{
					Name:    "sampling",
					Options: []ChoiceOption{{Name: "greedy", Args: []string{"--top-k", "-1"}}},
				}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := loadOne(t, test.settings, test.entry)
			if err != nil {
				t.Fatalf("entry was rejected: %v", err)
			}
			got.Path = "" // the temp root differs per run; TestLoadRecordsEntryPath covers it
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("entry resolved to\n  %+v\nwant\n  %+v", got, test.want)
			}
		})
	}
}

// entryWith is the smallest entry that loads, plus the lines a case is about: a
// choice case then reads as the choice it tests and nothing else.
func entryWith(lines string) string {
	return "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n" + lines
}

func TestEntryRulesReject(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		entry    string
		wantKey  string
	}{
		{
			name:    "backend is required",
			entry:   "repo = \"org/name\"\nport = 8080\n",
			wantKey: "backend",
		},
		{
			name:    "backend must be one of the two servers",
			entry:   "backend = \"vllm\"\nrepo = \"org/name\"\nport = 8080\n",
			wantKey: "backend",
		},
		{
			name:    "backend must be a string",
			entry:   "backend = 3\nrepo = \"org/name\"\nport = 8080\n",
			wantKey: "backend",
		},
		{
			name:    "repo is required",
			entry:   "backend = \"llama\"\nport = 8080\n",
			wantKey: "repo",
		},
		{
			name:    "repo must name an org",
			entry:   "backend = \"llama\"\nrepo = \"Qwen3-30B\"\nport = 8080\n",
			wantKey: "repo",
		},
		{
			name:    "repo may not hold spaces",
			entry:   "backend = \"llama\"\nrepo = \"org/na me\"\nport = 8080\n",
			wantKey: "repo",
		},
		{
			name:    "repo may not hold a third segment",
			entry:   "backend = \"llama\"\nrepo = \"org/name/extra\"\nport = 8080\n",
			wantKey: "repo",
		},
		{
			name:    "quant belongs to llama only",
			entry:   "backend = \"mlx\"\nrepo = \"org/name\"\nquant = \"Q4_K_M\"\nport = 8080\n",
			wantKey: "quant",
		},
		{
			name:    "quant may not be blank",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nquant = \"\"\nport = 8080\n",
			wantKey: "quant",
		},
		{
			name:    "port is required when no default_port exists",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\n",
			wantKey: "port",
		},
		{
			name:    "port must be an integer",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = \"8080\"\n",
			wantKey: "port",
		},
		{
			name:    "port must be a bindable port",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 70000\n",
			wantKey: "port",
		},
		{
			name:    "port may not be zero",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 0\n",
			wantKey: "port",
		},
		{
			name:    "host may not be blank",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nhost = \"\"\n",
			wantKey: "host",
		},
		{
			name:    "name may not be blank",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nname = \"  \"\n",
			wantKey: "name",
		},
		{
			name:    "args must be a list",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = \"--ctx-size 16384\"\n",
			wantKey: "args",
		},
		{
			name:    "args elements must be strings",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--ctx-size\", 16384]\n",
			wantKey: "args",
		},
		{
			name:    "args may not restate -hf",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"-hf\", \"other/repo\"]\n",
			wantKey: "args",
		},
		{
			name:    "args may not restate --model",
			entry:   "backend = \"mlx\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--model\", \"other/repo\"]\n",
			wantKey: "args",
		},
		{
			name:    "args may not restate --port",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--port\", \"9090\"]\n",
			wantKey: "args",
		},
		{
			name:    "args may not restate --host",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--host\", \"127.0.0.1\"]\n",
			wantKey: "args",
		},
		{
			name:    "args may not restate a composed flag in --flag=value form",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--port=9090\"]\n",
			wantKey: "args",
		},
		{
			name:    "args may not restate --model in --flag=value form",
			entry:   "backend = \"mlx\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--model=other/repo\"]\n",
			wantKey: "args",
		},
		{
			name:    "an unknown key is a typo, not an extension point",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nctx = 16384\n",
			wantKey: "ctx",
		},
		{
			name:    "a misspelled known key is reported by its own name",
			entry:   "bakend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
			wantKey: "bakend",
		},
		{
			name:    "an entry table is not a key",
			entry:   "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n[tools]\nhf = \"/usr/bin/hf\"\n",
			wantKey: "tools",
		},
		{
			name:    "choice must be a list of tables",
			entry:   entryWith("choice = \"quant\"\n"),
			wantKey: "choice",
		},
		{
			name:    "a choice needs a name",
			entry:   entryWith("[[choice]]\n  [[choice.option]]\n  name = \"q4\"\n"),
			wantKey: "choice.name",
		},
		{
			name:    "a choice name is spelled like an entry id",
			entry:   entryWith("[[choice]]\nname = \"context size\"\n  [[choice.option]]\n  name = \"q4\"\n"),
			wantKey: "choice.name",
		},
		{
			name: "two choices may not share a name",
			entry: entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4\"\n" +
				"[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q8\"\n"),
			wantKey: "choice.name",
		},
		{
			name:    "a choice with no options has nothing to pick",
			entry:   entryWith("[[choice]]\nname = \"quant\"\n"),
			wantKey: "choice.option",
		},
		{
			name:    "an empty option list has nothing to pick either",
			entry:   entryWith("[[choice]]\nname = \"quant\"\noption = []\n"),
			wantKey: "choice.option",
		},
		{
			name:    "an unknown key inside a choice is a typo",
			entry:   entryWith("[[choice]]\nname = \"quant\"\ndefault = \"q4\"\n  [[choice.option]]\n  name = \"q4\"\n"),
			wantKey: "choice.default",
		},
		{
			name:    "an option needs a name",
			entry:   entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  quant = \"Q8_0\"\n"),
			wantKey: "choice.option.name",
		},
		{
			name:    "an option name is spelled like an entry id",
			entry:   entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4:xl\"\n"),
			wantKey: "choice.option.name",
		},
		{
			name: "two options of one choice may not share a name",
			entry: entryWith("[[choice]]\nname = \"quant\"\n" +
				"  [[choice.option]]\n  name = \"q4\"\n  [[choice.option]]\n  name = \"q4\"\n"),
			wantKey: "choice.option.name",
		},
		{
			name:    "an unknown key inside an option is a typo",
			entry:   entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4\"\n  ctx = 16384\n"),
			wantKey: "choice.option.ctx",
		},
		{
			name: "an option's quant belongs to llama only",
			entry: "backend = \"mlx\"\nrepo = \"org/name\"\nport = 8080\n" +
				"[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q8\"\n  quant = \"Q8_0\"\n",
			wantKey: "choice.option.quant",
		},
		{
			name:    "an option's repo must name an org",
			entry:   entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4\"\n  repo = \"Qwen3-30B\"\n"),
			wantKey: "choice.option.repo",
		},
		{
			name: "only one choice may replace the quant",
			entry: entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4\"\n  quant = \"UD-Q4_K_XL\"\n" +
				"[[choice]]\nname = \"size\"\n  [[choice.option]]\n  name = \"big\"\n  quant = \"Q8_0\"\n"),
			wantKey: "choice.option.quant",
		},
		{
			name: "only one choice may replace the repo",
			entry: entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4\"\n  repo = \"org/q4\"\n" +
				"[[choice]]\nname = \"fork\"\n  [[choice.option]]\n  name = \"other\"\n  repo = \"org/other\"\n"),
			wantKey: "choice.option.repo",
		},
		{
			name:    "an option may not restate a flag cria composes",
			entry:   entryWith("[[choice]]\nname = \"quant\"\n  [[choice.option]]\n  name = \"q4\"\n  args = [\"--port\", \"9090\"]\n"),
			wantKey: "choice.option.args",
		},
		{
			name: "a flag in the entry's args may not also be set by an option",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--ctx-size\", \"16384\"]\n" +
				"[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n",
			wantKey: "choice.option.args",
		},
		{
			name: "the same flag with the same value is still two homes",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--ctx-size\", \"16384\"]\n" +
				"[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"same\"\n  args = [\"--ctx-size\", \"16384\"]\n",
			wantKey: "choice.option.args",
		},
		{
			name: "options of two different choices may not set the same flag",
			entry: entryWith("[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n" +
				"[[choice]]\nname = \"offload\"\n  [[choice.option]]\n  name = \"cpu\"\n  args = [\"--ctx-size=8192\"]\n"),
			wantKey: "choice.option.args",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, err := loadOne(t, test.settings, test.entry)
			if err == nil {
				t.Fatalf("entry was accepted as %+v, want a rejection naming key %q", entry, test.wantKey)
			}
			var keyErr *KeyError
			if !errors.As(err, &keyErr) {
				t.Fatalf("error is %T (%v), want a *KeyError naming %q", err, err, test.wantKey)
			}
			if keyErr.Key != test.wantKey {
				t.Errorf("error names key %q, want %q (%v)", keyErr.Key, test.wantKey, err)
			}
		})
	}
}

// A collision is about two places at once, so the refusal names both: the flag
// and each home that sets it, which is what the author has to go and edit.
func TestFlagCollisionNamesBothHomes(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  []string
	}{
		{
			name: "the entry's args against an option",
			entry: "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\nargs = [\"--ctx-size\", \"16384\"]\n" +
				"[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n",
			want: []string{"--ctx-size", "the entry's args", `option "long" of choice "ctx"`},
		},
		{
			name: "one axis against another",
			entry: entryWith("[[choice]]\nname = \"ctx\"\n  [[choice.option]]\n  name = \"long\"\n  args = [\"--ctx-size\", \"65536\"]\n" +
				"[[choice]]\nname = \"offload\"\n  [[choice.option]]\n  name = \"cpu\"\n  args = [\"--ctx-size=8192\"]\n"),
			want: []string{"--ctx-size", `option "long" of choice "ctx"`, `option "cpu" of choice "offload"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, err := loadOne(t, "", test.entry)
			if err == nil {
				t.Fatalf("entry was accepted as %+v, want the collision refused", entry)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal is %q, want it to name %s", err, want)
				}
			}
		})
	}
}

// config.toml, rule by rule. Its failures are tree-level: every entry resolves
// against it, so a broken one is not an isolatable entry problem.

func TestSettingsAccept(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     Settings
	}{
		{
			name: "an absent config.toml is a valid tree",
			want: Settings{},
		},
		{
			name:     "an empty config.toml is a valid tree",
			settings: "\n# nothing set yet\n",
			want:     Settings{},
		},
		{
			name:     "defaults only",
			settings: "default_port = 8080\ndefault_host = \"127.0.0.1\"\n",
			want:     Settings{DefaultPort: 8080, DefaultHost: "127.0.0.1"},
		},
		{
			name: "defaults and every tool override",
			settings: "default_port = 8080\n[tools]\nllama_server = \"/opt/homebrew/bin/llama-server\"\n" +
				"mlx_lm_server = \"/opt/homebrew/bin/mlx_lm.server\"\nhf = \"/opt/homebrew/bin/hf\"\n",
			want: Settings{
				DefaultPort: 8080,
				Tools: Tools{
					LlamaServer: "/opt/homebrew/bin/llama-server",
					MLXLMServer: "/opt/homebrew/bin/mlx_lm.server",
					HF:          "/opt/homebrew/bin/hf",
				},
			},
		},
		{
			name:     "a partial tools table leaves the rest on PATH",
			settings: "[tools]\nhf = \"/opt/homebrew/bin/hf\"\n",
			want:     Settings{Tools: Tools{HF: "/opt/homebrew/bin/hf"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := map[string]string{}
			if test.settings != "" {
				files[settingsFile] = test.settings
			}
			tree, err := Load(writeTree(t, files))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(tree.Settings, test.want) {
				t.Errorf("settings loaded as\n  %+v\nwant\n  %+v", tree.Settings, test.want)
			}
		})
	}
}

func TestSettingsReject(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		wantKey  string
	}{
		{
			name:     "an unknown top-level key is a typo",
			settings: "default_prot = 8080\n",
			wantKey:  "default_prot",
		},
		{
			name:     "default_port must be an integer",
			settings: "default_port = \"8080\"\n",
			wantKey:  "default_port",
		},
		{
			name:     "default_port must be a bindable port",
			settings: "default_port = 0\n",
			wantKey:  "default_port",
		},
		{
			name:     "default_host may not be blank",
			settings: "default_host = \"\"\n",
			wantKey:  "default_host",
		},
		{
			name:     "tools must be a table",
			settings: "tools = \"/opt/homebrew/bin\"\n",
			wantKey:  "tools",
		},
		{
			name:     "an unknown key inside tools is reported with its table",
			settings: "[tools]\nllama = \"/opt/homebrew/bin/llama-server\"\n",
			wantKey:  "tools.llama",
		},
		{
			name:     "a tool override must be an absolute path",
			settings: "[tools]\nhf = \"hf\"\n",
			wantKey:  "tools.hf",
		},
		{
			name:     "a tool override must be a string",
			settings: "[tools]\nhf = 7\n",
			wantKey:  "tools.hf",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				settingsFile:          test.settings,
				"models/healthy.toml": "backend = \"llama\"\nrepo = \"org/name\"\nport = 8080\n",
			})
			tree, err := Load(root)
			if err == nil {
				t.Fatalf("tree loaded with %d entries, want a rejection naming key %q", len(tree.Entries), test.wantKey)
			}
			if tree != nil {
				t.Errorf("Load returned a tree alongside its error; a tree-level failure yields none")
			}
			var keyErr *KeyError
			if !errors.As(err, &keyErr) {
				t.Fatalf("error is %T (%v), want a *KeyError naming %q", err, err, test.wantKey)
			}
			if keyErr.Key != test.wantKey {
				t.Errorf("error names key %q, want %q (%v)", keyErr.Key, test.wantKey, err)
			}
		})
	}
}

// TestSchemaDefinitionsCarryTheirDocs guards the one-source rule: `cria docs`
// renders these definitions, so a key added without its documentation would ship
// an undocumented key rather than a stale doc page.
func TestSchemaDefinitionsCarryTheirDocs(t *testing.T) {
	var walk func(t *testing.T, s schema, prefix string)
	walk = func(t *testing.T, s schema, prefix string) {
		if len(s) == 0 {
			t.Errorf("%sschema declares no keys", prefix)
		}
		seen := map[string]bool{}
		for _, k := range s {
			name := prefix + k.name
			if seen[k.name] {
				t.Errorf("key %q is declared twice", name)
			}
			seen[k.name] = true
			if k.name == "" {
				t.Errorf("a key in %q has no name", prefix)
			}
			if k.rules == "" {
				t.Errorf("key %q has no rules line to document it", name)
			}
			if k.kind.holdsKeys() {
				walk(t, k.keys, name+".")
				continue
			}
			if k.example == "" {
				t.Errorf("key %q has no example value", name)
			}
			if k.keys != nil {
				t.Errorf("key %q is not a table but declares sub-keys", name)
			}
		}
	}

	t.Run("entry", func(t *testing.T) { walk(t, entrySchema, "") })
	t.Run("config.toml", func(t *testing.T) { walk(t, treeSchema, "") })
}

func TestKindNames(t *testing.T) {
	tests := []struct {
		kind kind
		want string
	}{
		{kindString, "string"},
		{kindInteger, "integer"},
		{kindStringList, "string[]"},
		{kindTable, "table"},
		{kindTableArray, "table[]"},
	}
	for _, test := range tests {
		if got := test.kind.String(); got != test.want {
			t.Errorf("kind %d prints %q, want %q", int(test.kind), got, test.want)
		}
	}
}
