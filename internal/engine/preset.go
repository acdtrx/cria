package engine

import (
	"fmt"
	"strings"

	"cria/internal/config"
)

// The router reads the models it may serve from an ini preset — llama-server's
// own format, not a shape cria invented. cria composes that file at every start
// from what the config tree declares; the tree keeps writing the server's own
// flags as argv tokens, and the translation happens here (docs/specs/CONFIG.md,
// OVERVIEW ruling 1).
//
// The translation is only possible because upstream canonicalizes its own key
// names (`ngl` → `--n-gpu-layers`, `fa` → `--flash-attn`, verified live
// 2026-08-31): a preset line is a flag group with its dashes stripped, and cria
// owns no mapping between the two spellings. It also refuses loudly rather than
// guessing — an unknown key fails the router's startup naming key and section,
// so a preset cria could not write honestly must not be written at all.

const (
	// presetDefaults is the section every model the router serves starts from.
	// The model sections a launch composes override it, which is the same
	// precedence the engine file and an entry already have.
	presetDefaults = "*"

	// presetTrue is what a flag with no value becomes. A command line says a
	// switch by naming it; an ini file has to give it a value.
	presetTrue = "true"
)

// RouterPreset composes the preset one router process serves from: the defaults
// section, from what engines/router.toml says every model it serves starts from.
//
// The file is regenerated at every start and never edited (docs/specs/SERVE.md),
// and it carries no comments of its own: it is upstream's format, read by
// upstream's parser, and a line cria added for a reader is a line that parser
// has to accept.
func RouterPreset(defaults []string) (string, error) {
	lines, err := presetSection(defaults)
	if err != nil {
		return "", fmt.Errorf("engines/%s.toml args: %w", config.BackendRouter, err)
	}

	var preset strings.Builder
	preset.WriteString("[" + presetDefaults + "]\n")
	for _, line := range lines {
		preset.WriteString(line + "\n")
	}
	return preset.String(), nil
}

// presetSection turns one args list into the lines of one preset section: each
// flag group as the key upstream canonicalizes, with the value written after it.
//
// Three lists cannot be written as keys, and each is refused by name rather than
// silently narrowed — the router would otherwise be started with less than its
// file asked for:
//
//   - a flag written twice, because a key holds one value;
//   - a flag carrying more than one value, for the same reason;
//   - tokens written before any flag, which have no key to belong to.
//
// This is the derivation every section is composed with: the defaults here, and
// each included model's own args where those land (STEP-8).
func presetSection(args []string) ([]string, error) {
	lines := make([]string, 0, len(args))
	written := make(map[string]bool, len(args))

	for _, group := range config.FlagGroups(args) {
		if group.Flag == "" {
			return nil, fmt.Errorf("%q is written before any flag, so it has no preset key to belong to", group.String())
		}

		key := strings.TrimLeft(group.Flag, "-")
		if written[key] {
			return nil, fmt.Errorf("%s is written more than once, and a preset key holds one value; write it once", group.Flag)
		}
		written[key] = true

		value, err := presetValue(group)
		if err != nil {
			return nil, err
		}
		lines = append(lines, key+" = "+value)
	}
	return lines, nil
}

// presetValue is what one flag group says on the right of the '='. The value is
// the token the file wrote, passed on as written: cria reads none of it, here or
// on a command line.
func presetValue(group config.FlagGroup) (string, error) {
	switch len(group.Tokens) {
	case 1:
		// One token is either a switch or the --flag=value spelling, which the
		// pairing leaves whole because a command line takes it whole.
		_, value, joined := strings.Cut(group.Tokens[0], "=")
		if !joined {
			return presetTrue, nil
		}
		if value == "" {
			return "", fmt.Errorf("%s is written with nothing after the '='; give it a value or write the flag alone", group.Flag)
		}
		return value, nil
	case 2:
		return group.Tokens[1], nil
	default:
		return "", fmt.Errorf("%s carries %d values, and a preset key holds one; the router cannot be given this flag",
			group.Flag, len(group.Tokens)-1)
	}
}
