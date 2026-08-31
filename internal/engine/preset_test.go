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
			preset, err := RouterPreset(test.defaults)
			if err != nil {
				t.Fatalf("composing %v: %v", test.defaults, err)
			}
			if preset != test.want {
				t.Errorf("the preset is\n%q\nwant\n%q", preset, test.want)
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
			preset, err := RouterPreset(test.defaults)
			if err == nil {
				t.Fatalf("%v composed as\n%s", test.defaults, preset)
			}
			for _, want := range append(test.want, "router.toml") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal reads %v, want it to name %q", err, want)
				}
			}
		})
	}
}
