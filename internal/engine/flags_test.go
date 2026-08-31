package engine

import (
	"reflect"
	"strings"
	"testing"

	"cria/internal/config"
)

// A key becomes a flag mechanically: nothing here knows what any of these
// options mean, and the value is handed on exactly as the file wrote it.
func TestKeysBecomeTheFlagsAServerTakes(t *testing.T) {
	tests := []struct {
		name string
		args []config.Arg
		want []string
	}{
		{
			name: "a launch that sets nothing spells nothing",
			want: []string{},
		},
		{
			name: "a long key takes two dashes and its value follows",
			args: []config.Arg{{Key: "ctx-size", Value: "16384"}},
			want: []string{"--ctx-size", "16384"},
		},
		{
			name: "a one-letter key takes one dash",
			args: []config.Arg{{Key: "c", Value: "262144"}},
			want: []string{"-c", "262144"},
		},
		{
			name: "true is a flag that takes no value",
			args: []config.Arg{{Key: "jinja", Value: "true"}},
			want: []string{"--jinja"},
		},
		{
			name: "every other value is passed as written, the server's own words included",
			args: []config.Arg{{Key: "flash-attn", Value: "off"}, {Key: "cache-type-k", Value: "q8_0"}},
			want: []string{"--flash-attn", "off", "--cache-type-k", "q8_0"},
		},
		{
			name: "a value stays one argument, spaces and all",
			args: []config.Arg{{Key: "chat-template-kwargs", Value: `{"enable_thinking": false}`}},
			want: []string{"--chat-template-kwargs", `{"enable_thinking": false}`},
		},
		{
			name: "the keys keep the order the merge put them in",
			args: []config.Arg{
				{Key: "gpu-layers", Value: "99"},
				{Key: "jinja", Value: "true"},
				{Key: "parallel", Value: "4"},
			},
			want: []string{"--gpu-layers", "99", "--jinja", "--parallel", "4"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Flags(test.args); !reflect.DeepEqual(got, test.want) {
				t.Errorf("the args spell\n  %q\nwant\n  %q", got, test.want)
			}
		})
	}
}

// The key an args list may not restate is the flag its engine actually
// composes, without its dashes. config declares the key — it is what a file is
// refused against — and each engine spells the flag; this is the one place both
// are in view, so the two cannot drift into an args key that silently overrides
// the composed model reference.
func TestEveryEnginesModelFlagIsTheKeyTheTreeRefuses(t *testing.T) {
	launch := config.Launch{Repo: "org/repo", Quant: "Q4"}

	for _, engine := range All() {
		t.Run(string(engine.ID()), func(t *testing.T) {
			args := engine.ModelArgs(launch)
			if len(args) == 0 {
				t.Fatal("the engine names no model on the command line it composes")
			}
			flag := args[0]
			if !strings.HasPrefix(flag, "-") {
				t.Fatalf("the model reference opens with %q, want the flag it is passed under", flag)
			}
			if key, want := config.ModelKey(engine.ID()), strings.TrimLeft(flag, "-"); key != want {
				t.Errorf("the engine composes %q while the tree refuses the key %q; args may set %q and win the command line",
					flag, key, want)
			}
		})
	}
}
