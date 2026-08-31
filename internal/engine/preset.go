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
	// precedence the engine file and an entry already have — upstream's own
	// precedence rule, so cria merges nothing into a section that the router
	// would merge again.
	presetDefaults = "*"

	// presetTrue is what a flag with no value becomes. A command line says a
	// switch by naming it; an ini file has to give it a value.
	presetTrue = "true"

	// presetAlias is the key that gives one section the name its clients address
	// it by. cria writes it itself, from the entry id (STEP-4's ruling): section
	// names are model references and upstream normalizes their quant tags, while
	// an alias is passed through as written.
	presetAlias = "alias"
)

// RouterModel is one entry as the router is asked to serve it: the id clients
// address it by, the model reference its section is named after, and the args
// that section carries — the entry's own, merged with the picked options', with
// no engine level in them (config.ResolveUnder).
type RouterModel struct {
	ID    string
	Repo  string
	Quant string
	Args  []string
}

// Composition is a composed preset and what became of every model it was asked
// to carry: the ids that got a section, and the ids that got none with the
// reason each was refused.
//
// A model that cannot be written is skipped rather than fatal. One entry whose
// args are not expressible as preset keys would otherwise take the whole router
// down with it, and a router that serves the rest and says which one it dropped
// is the answer the host can act on (docs/specs/SERVE.md).
type Composition struct {
	Preset  string   // the file's whole text
	Served  []string // the ids that got a section, in the order they were written
	Skipped []Skipped
}

// Skipped is one model the preset could not carry, and why — phrased for
// whoever included it, since the fix is always in that entry's file or in the
// inclusion itself.
type Skipped struct {
	ID     string
	Reason string
}

// RouterPreset composes the preset one router process serves from: the defaults
// section, from what engines/router.toml says every model it serves starts from,
// and one section per model included in the router.
//
// The file is regenerated at every start and never edited (docs/specs/SERVE.md),
// and it carries no comments of its own: it is upstream's format, read by
// upstream's parser, and a line cria added for a reader is a line that parser
// has to accept.
//
// The error is the defaults' alone. What the engine file says is the router's
// own configuration — a router started with less than it asked for is not the
// router that was configured — while a model that cannot be written is one
// model, answered per model.
func RouterPreset(defaults []string, models []RouterModel) (Composition, error) {
	keys, err := presetSection(defaults)
	if err != nil {
		return Composition{}, fmt.Errorf("engines/%s.toml args: %w", config.BackendRouter, err)
	}

	var preset strings.Builder
	preset.WriteString("[" + presetDefaults + "]\n")
	for _, key := range keys {
		preset.WriteString(key.line() + "\n")
	}

	composed := Composition{}
	sections := map[string]string{} // section name → the id that wrote it
	for _, model := range models {
		section := hubReference(config.Launch{Repo: model.Repo, Quant: model.Quant})

		if held, taken := sections[section]; taken {
			composed.Skipped = append(composed.Skipped, Skipped{ID: model.ID, Reason: fmt.Sprintf(
				"it serves %s, which %s is already the router's section for; one section per model reference, so include one of the two or point them at different quantizations",
				section, held)})
			continue
		}
		keys, err := presetSection(model.Args)
		if err != nil {
			composed.Skipped = append(composed.Skipped, Skipped{ID: model.ID, Reason: err.Error()})
			continue
		}
		if named := namesAlias(keys); named != "" {
			composed.Skipped = append(composed.Skipped, Skipped{ID: model.ID, Reason: fmt.Sprintf(
				"its args set %s, and the router's section for it is named by its entry id; drop that flag from the entry's args", named)})
			continue
		}

		sections[section] = model.ID
		preset.WriteString("\n[" + section + "]\n")
		preset.WriteString(presetAlias + " = " + model.ID + "\n")
		for _, key := range keys {
			preset.WriteString(key.line() + "\n")
		}
		composed.Served = append(composed.Served, model.ID)
	}

	composed.Preset = preset.String()
	return composed, nil
}

// presetKey is one line of a preset section: the key upstream canonicalizes and
// the value written after it. It stays a pair rather than a formatted line so
// composition can see what a section sets before it writes it.
type presetKey struct {
	Key   string
	Flag  string // the flag the key was written as, for a refusal to name
	Value string
}

func (k presetKey) line() string { return k.Key + " = " + k.Value }

// presetSection turns one args list into the keys of one preset section: each
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
// This is the derivation every section is composed with: the defaults, and each
// included model's own args.
func presetSection(args []string) ([]presetKey, error) {
	keys := make([]presetKey, 0, len(args))
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
		keys = append(keys, presetKey{Key: key, Flag: group.Flag, Value: value})
	}
	return keys, nil
}

// namesAlias reports the flag a section's own args set the alias with, or the
// empty string when none does.
//
// cria writes that key itself, from the entry id, so a section carrying a second
// one would hand upstream two answers to the same question. Only the key cria
// writes is refused: cria owns no table of upstream's short spellings, so a
// list writing `-a` is upstream's to canonicalize and refuse.
func namesAlias(keys []presetKey) string {
	for _, key := range keys {
		if key.Key == presetAlias {
			return key.Flag
		}
	}
	return ""
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
