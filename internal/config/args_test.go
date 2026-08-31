package config

import (
	"reflect"
	"slices"
	"testing"
)

// The one rule cria reads an args list by: where each flag's tokens begin and
// end. Everything that compares or replaces args — the levels of a launch, the
// collision refusal, the detail pane's lines — is this pairing read one way or
// another, so it is pinned here rather than at each of them
// (docs/specs/CONFIG.md).
func TestFlagGroupsPairEachFlagWithItsValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []FlagGroup
	}{
		{
			name: "an empty list groups into nothing",
		},
		{
			name: "a flag takes the value written after it",
			args: []string{"--ctx-size", "16384"},
			want: []FlagGroup{{Flag: "--ctx-size", Tokens: []string{"--ctx-size", "16384"}}},
		},
		{
			name: "a flag that takes no value is a group of its own",
			args: []string{"--jinja", "--ctx-size", "16384"},
			want: []FlagGroup{
				{Flag: "--jinja", Tokens: []string{"--jinja"}},
				{Flag: "--ctx-size", Tokens: []string{"--ctx-size", "16384"}},
			},
		},
		{
			name: "a one-dash flag is a flag",
			args: []string{"-ngl", "99", "-fa", "on"},
			want: []FlagGroup{
				{Flag: "-ngl", Tokens: []string{"-ngl", "99"}},
				{Flag: "-fa", Tokens: []string{"-fa", "on"}},
			},
		},
		{
			name: "every value after a flag belongs to it",
			args: []string{"--lora", "one.gguf", "two.gguf", "--jinja"},
			want: []FlagGroup{
				{Flag: "--lora", Tokens: []string{"--lora", "one.gguf", "two.gguf"}},
				{Flag: "--jinja", Tokens: []string{"--jinja"}},
			},
		},
		{
			// The 2026-08-22 amendment, still load-bearing: a bare number is a
			// value flags commonly take, so it never opens a group of its own.
			name: "a negative number is a value, not a flag",
			args: []string{"--seed", "-1", "--temp", "-0.5"},
			want: []FlagGroup{
				{Flag: "--seed", Tokens: []string{"--seed", "-1"}},
				{Flag: "--temp", Tokens: []string{"--temp", "-0.5"}},
			},
		},
		{
			name: "a --flag=value token is that flag, payload and all",
			args: []string{"--ctx-size=16384", "--jinja"},
			want: []FlagGroup{
				{Flag: "--ctx-size", Tokens: []string{"--ctx-size=16384"}},
				{Flag: "--jinja", Tokens: []string{"--jinja"}},
			},
		},
		{
			name: "one flag written twice is two groups; the list is passed as written",
			args: []string{"--override-kv", "a=b", "--override-kv", "c=d"},
			want: []FlagGroup{
				{Flag: "--override-kv", Tokens: []string{"--override-kv", "a=b"}},
				{Flag: "--override-kv", Tokens: []string{"--override-kv", "c=d"}},
			},
		},
		{
			// Values before any flag belong to nothing. They are kept where they
			// stand rather than swallowed, so a list nobody can pair is still the
			// list the server receives.
			name: "tokens written before any flag name no flag",
			args: []string{"stray", "--jinja"},
			want: []FlagGroup{
				{Flag: "", Tokens: []string{"stray"}},
				{Flag: "--jinja", Tokens: []string{"--jinja"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FlagGroups(test.args)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("the list groups into\n  %+v\nwant\n  %+v", got, test.want)
			}

			// Nothing is reformatted or dropped: the groups hold the list back.
			var paired []string
			for _, group := range got {
				paired = append(paired, group.Tokens...)
			}
			if !slices.Equal(paired, test.args) {
				t.Errorf("the groups hold %q, want the list they came from %q", paired, test.args)
			}
		})
	}
}

// A group draws as the line it was written as — the flag and its values, spaced
// the way a command line spaces them.
func TestAFlagGroupSpellsItsOwnLine(t *testing.T) {
	tests := []struct {
		group FlagGroup
		want  string
	}{
		{group: FlagGroup{Flag: "--jinja", Tokens: []string{"--jinja"}}, want: "--jinja"},
		{group: FlagGroup{Flag: "-c", Tokens: []string{"-c", "262144"}}, want: "-c 262144"},
		{
			group: FlagGroup{Flag: "--chat-template-kwargs", Tokens: []string{"--chat-template-kwargs", `{"a": 1}`}},
			want:  `--chat-template-kwargs {"a": 1}`,
		},
	}

	for _, test := range tests {
		if got := test.group.String(); got != test.want {
			t.Errorf("the group spells %q, want %q", got, test.want)
		}
	}
}

// The merge is aliasing-free in both directions: it reads the levels and writes
// a list of its own, so a launch can never grow into the tree it was composed
// from (mergeArgs builds new groups; the levels keep their own).
func TestMergingLevelsWritesIntoNeitherOfThem(t *testing.T) {
	engine := make([]string, 0, 8)
	engine = append(engine, "-ngl", "99")
	entry := []string{"--ctx-size", "16384"}

	merged := mergeArgs(engine, entry)
	if want := []string{"-ngl", "99", "--ctx-size", "16384"}; !slices.Equal(merged, want) {
		t.Fatalf("the levels merge to %q, want %q", merged, want)
	}

	merged[0] = "--gutted"
	if want := []string{"-ngl", "99"}; !slices.Equal(engine, want) {
		t.Errorf("the engine's level became %q, want %q", engine, want)
	}
	if want := []string{"--ctx-size", "16384"}; !slices.Equal(entry, want) {
		t.Errorf("the entry's level became %q, want %q", entry, want)
	}
}
