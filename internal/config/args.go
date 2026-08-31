package config

import "strings"

// An args list is the command line a server takes, token for token: cria reads
// none of it and passes every token through as written (docs/specs/CONFIG.md).
// The one thing it does read is where each flag's tokens begin and end — that is
// what makes a more specific level able to replace a flag the level under it
// set, and it is the only grouping rule in the project.

// FlagGroup is one flag and the tokens written after it. The flag is what two
// levels of a launch compare when one overrides the other; the tokens are the
// group as the file wrote it, and cria never looks inside them.
//
// A group whose Flag is empty holds tokens written before any flag — values
// with nothing to belong to. Nothing overrides them and they override nothing;
// they stay where the file put them.
type FlagGroup struct {
	Flag   string
	Tokens []string
}

// String spells a group as one line: the flag and its values, separated by the
// spaces a command line would show. It is what the TUI draws per line, and the
// router engine's config-format preset is the same grouping read differently —
// the flag without its dashes is the key upstream canonicalizes, the tokens
// after it are the value. One pairing, both spellings.
func (g FlagGroup) String() string { return strings.Join(g.Tokens, " ") }

// FlagGroups pairs an args list into the groups it is made of: each flag with
// the values that follow it, in the list's own order. Nothing is reformatted or
// dropped — concatenating the groups' tokens yields the list back.
func FlagGroups(args []string) []FlagGroup {
	var groups []FlagGroup
	for _, token := range args {
		flag, isFlag := flagToken(token)
		if isFlag || len(groups) == 0 {
			groups = append(groups, FlagGroup{Flag: flag, Tokens: []string{token}})
			continue
		}
		last := &groups[len(groups)-1]
		last.Tokens = append(last.Tokens, token)
	}
	return groups
}

// flagToken reads an args token as a flag: the flag it names, without any
// --flag=value payload, and whether it names one at all. A leading '-' followed
// by a letter is a flag; "-1" and "-0.5" are values a flag takes, and two levels
// of one launch passing the same number fight over nothing.
func flagToken(token string) (string, bool) {
	name, _, _ := strings.Cut(token, "=")
	rest := strings.TrimLeft(name, "-")
	if rest == name || rest == "" {
		return "", false
	}
	switch first := rest[0]; {
	case first >= 'a' && first <= 'z', first >= 'A' && first <= 'Z':
		return name, true
	default:
		return "", false
	}
}

// flagTokens lists the flags an args list sets. Values are left out, so two
// options setting the same flag collide whatever they set it to
// (docs/specs/CONFIG.md).
func flagTokens(args []string) []string {
	var flags []string
	for _, group := range FlagGroups(args) {
		if group.Flag != "" {
			flags = append(flags, group.Flag)
		}
	}
	return flags
}

// mergeArgs composes the levels of one launch, least specific first: what the
// engine serves everything with, what the entry declares, what the picks change
// (docs/specs/CONFIG.md).
//
// It builds a new list rather than appending into any level's own slice:
// appending would write one launch's composition into the loaded tree, and the
// next launch would read it back.
func mergeArgs(levels ...[]string) []string {
	var merged []FlagGroup
	for _, level := range levels {
		merged = override(merged, FlagGroups(level))
	}

	var args []string
	for _, group := range merged {
		args = append(args, group.Tokens...)
	}
	return args
}

// override lays one level over the levels beneath it. A flag the higher level
// names replaces every group the lower ones wrote under it, at the place the
// first of them stood: an override changes what is passed, not the order the
// files read in. A flag the higher level introduces follows at the end.
//
// Repetition inside one level is left alone. A list passing one flag twice is
// the author's own business — it is a command line, and cria hands it on as
// written; only two levels meeting are a question this has to answer.
func override(lower, higher []FlagGroup) []FlagGroup {
	replacements := map[string][]FlagGroup{}
	for _, group := range higher {
		if group.Flag != "" {
			replacements[group.Flag] = append(replacements[group.Flag], group)
		}
	}

	var merged []FlagGroup
	placed := map[string]bool{}
	for _, group := range lower {
		replacement, replaced := replacements[group.Flag]
		if !replaced {
			merged = append(merged, group)
			continue
		}
		if placed[group.Flag] {
			continue
		}
		placed[group.Flag] = true
		merged = append(merged, replacement...)
	}
	for _, group := range higher {
		if !placed[group.Flag] {
			merged = append(merged, group)
		}
	}
	return merged
}
