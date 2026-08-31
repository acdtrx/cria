package engine

import (
	"strings"
	"testing"
)

// The composed preset is what the router actually reads, so these are byte
// comparisons: a stray blank line or a quoted value is a file upstream's parser
// judges, not a formatting preference.

func TestThePresetIsTheEngineFilesArgsWithTheirDashesStripped(t *testing.T) {
	tests := []struct {
		name     string
		defaults []string
		want     string
	}{
		{
			name:     "the short aliases the tree already uses, canonicalized upstream",
			defaults: []string{"-ngl", "99", "-fa", "on"},
			want:     "[*]\nngl = 99\nfa = on\n",
		},
		{
			name:     "a flag with no value is the switch it is",
			defaults: []string{"--jinja", "-c", "262144"},
			want:     "[*]\njinja = true\nc = 262144\n",
		},
		{
			name:     "the --flag=value spelling names the same key",
			defaults: []string{"--ctx-size=8192"},
			want:     "[*]\nctx-size = 8192\n",
		},
		{
			name:     "a value cria does not read is written as it stands",
			defaults: []string{"--spec-type", "draft", "--temp", "0.7"},
			want:     "[*]\nspec-type = draft\ntemp = 0.7\n",
		},
		{
			name:     "an engine file with no args is a router with no defaults",
			defaults: nil,
			want:     "[*]\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			composed, err := RouterPreset(test.defaults, nil)
			if err != nil {
				t.Fatalf("composing %v: %v", test.defaults, err)
			}
			if composed.Preset != test.want {
				t.Errorf("the preset is\n%q\nwant\n%q", composed.Preset, test.want)
			}
		})
	}
}

// An args list that cannot be written as preset keys is refused by name at
// composition, not narrowed silently: the router would otherwise be started
// serving models with less than its file asked for.
func TestThePresetRefusesWhatItCannotWrite(t *testing.T) {
	tests := []struct {
		name     string
		defaults []string
		want     []string
	}{
		{
			name:     "a flag written twice has no single value",
			defaults: []string{"--override-kv", "a=int:1", "--override-kv", "b=int:2"},
			want:     []string{"--override-kv", "more than once"},
		},
		{
			name:     "a flag written twice in two spellings is still twice",
			defaults: []string{"-c", "8192", "-c=16384"},
			want:     []string{"-c", "more than once"},
		},
		{
			name:     "a flag carrying two values has no single value either",
			defaults: []string{"--override-tensor", "exps=CPU", "attn=GPU"},
			want:     []string{"--override-tensor", "2 values"},
		},
		{
			name:     "tokens before any flag belong to no key",
			defaults: []string{"99", "-fa", "on"},
			want:     []string{"99", "no preset key"},
		},
		{
			name:     "a flag written with nothing after the '=' sets nothing",
			defaults: []string{"--ctx-size="},
			want:     []string{"--ctx-size", "nothing after"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			composed, err := RouterPreset(test.defaults, nil)
			if err == nil {
				t.Fatalf("%v composed as\n%s", test.defaults, composed.Preset)
			}
			for _, want := range append(test.want, "router.toml") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal reads %v, want it to name %q", err, want)
				}
			}
		})
	}
}

// The models included in the router become sections of the same file: one per
// entry, named by the model reference it resolves to, carrying the entry id as
// the alias clients address it by (STEP-4's ruling) and the args that entry
// contributes over the defaults.
func TestThePresetCarriesOneSectionPerIncludedModel(t *testing.T) {
	composed, err := RouterPreset([]string{"-ngl", "99", "-fa", "on"}, []RouterModel{
		{ID: "qwen", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL", Args: []string{"-c", "262144", "--jinja"}},
		{ID: "lfm", Repo: "LiquidAI/LFM2.5-2.6B-GGUF", Quant: "Q8_0"},
		{ID: "plain", Repo: "ggml-org/gemma-3-4b-it-GGUF"},
	})
	if err != nil {
		t.Fatalf("composing the preset: %v", err)
	}

	want := "[*]\nngl = 99\nfa = on\n" +
		"\n[unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL]\nalias = qwen\nc = 262144\njinja = true\n" +
		"\n[LiquidAI/LFM2.5-2.6B-GGUF:Q8_0]\nalias = lfm\n" +
		"\n[ggml-org/gemma-3-4b-it-GGUF]\nalias = plain\n"
	if composed.Preset != want {
		t.Errorf("the preset is\n%q\nwant\n%q", composed.Preset, want)
	}
	if got := strings.Join(composed.Served, ", "); got != "qwen, lfm, plain" {
		t.Errorf("the composition served %q, want every model in the order it was given", got)
	}
	if len(composed.Skipped) != 0 {
		t.Errorf("the composition skipped %+v, want nothing", composed.Skipped)
	}
}

// One section per model reference. Two entries resolving to the same repo and
// quantization would be one section written twice, so the second is refused —
// naming both entries, because either of them is the one to change.
func TestTwoModelsCannotShareOneSection(t *testing.T) {
	composed, err := RouterPreset(nil, []RouterModel{
		{ID: "qwen-q4", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
		{ID: "qwen-again", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
		{ID: "qwen-q6", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q6_K_XL"},
	})
	if err != nil {
		t.Fatalf("composing the preset: %v", err)
	}

	if got := strings.Join(composed.Served, ", "); got != "qwen-q4, qwen-q6" {
		t.Errorf("the composition served %q, want the first of the two and the one on another quantization", got)
	}
	if len(composed.Skipped) != 1 || composed.Skipped[0].ID != "qwen-again" {
		t.Fatalf("the composition skipped %+v, want the second entry on the taken reference", composed.Skipped)
	}
	for _, want := range []string{"qwen-q4", "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL"} {
		if !strings.Contains(composed.Skipped[0].Reason, want) {
			t.Errorf("the reason reads %q, want it to name %q", composed.Skipped[0].Reason, want)
		}
	}
	if strings.Count(composed.Preset, "[unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL]") != 1 {
		t.Errorf("the preset is\n%s\nwant the shared section written once", composed.Preset)
	}
}

// An entry whose args cannot be written as preset keys costs that entry its
// section and nothing else: the router still serves every other model included
// in it, and the reason travels with the id (docs/specs/SERVE.md).
func TestAModelThatCannotBeWrittenIsSkippedRatherThanFatal(t *testing.T) {
	composed, err := RouterPreset([]string{"-ngl", "99"}, []RouterModel{
		{ID: "twice", Repo: "org/twice", Quant: "Q4", Args: []string{"--override-kv", "a=int:1", "--override-kv", "b=int:2"}},
		{ID: "named", Repo: "org/named", Quant: "Q4", Args: []string{"--alias", "something-else"}},
		{ID: "loose", Repo: "org/loose", Quant: "Q4", Args: []string{"99", "-fa", "on"}},
		{ID: "fine", Repo: "org/fine", Quant: "Q4", Args: []string{"-c", "8192"}},
	})
	if err != nil {
		t.Fatalf("composing the preset: %v", err)
	}

	if got := strings.Join(composed.Served, ", "); got != "fine" {
		t.Errorf("the composition served %q, want the one model it could write", got)
	}
	reasons := map[string]string{}
	for _, skipped := range composed.Skipped {
		reasons[skipped.ID] = skipped.Reason
	}
	for id, want := range map[string]string{
		"twice": "more than once",
		"named": "entry id",
		"loose": "no preset key",
	} {
		if !strings.Contains(reasons[id], want) {
			t.Errorf("%s was skipped with %q, want a reason naming %q", id, reasons[id], want)
		}
	}
	if strings.Contains(composed.Preset, "org/twice") || strings.Contains(composed.Preset, "org/named") {
		t.Errorf("the preset is\n%s\nwant no section for a model that was refused", composed.Preset)
	}
	if !strings.Contains(composed.Preset, "[*]\nngl = 99\n") {
		t.Errorf("the preset is\n%s\nwant the engine file's defaults intact", composed.Preset)
	}
}

// The engine file's own args are the router's configuration rather than one of
// its models: args that cannot be written there refuse the whole composition,
// and no model's section is written from a defaults section cria could not
// honour.
func TestTheDefaultsRefuseTheWholeComposition(t *testing.T) {
	_, err := RouterPreset([]string{"-c", "8192", "-c", "16384"}, []RouterModel{
		{ID: "fine", Repo: "org/fine", Quant: "Q4"},
	})
	if err == nil {
		t.Fatal("a preset composed from defaults that cannot be written as keys")
	}
	for _, want := range []string{"router.toml", "-c", "more than once"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %v, want it to name %q", err, want)
		}
	}
}
