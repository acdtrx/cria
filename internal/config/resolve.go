package config

import (
	"fmt"
	"slices"
	"strings"
)

// Selection is one pick per choice: choice name → option name. It is the whole
// input a launch varies on, and it is settled before it gets here — deciding
// which pick wins, an explicit one over a stored one over the config default,
// belongs to the caller that holds all three (docs/specs/SERVE.md).
type Selection map[string]string

// Launch is what an entry actually runs under one selection: the repo and quant
// after the picked options replaced them, and the args they composed. cria reads
// none of those args — it only puts them in the order the author wrote them, the
// entry's own first (docs/specs/CONFIG.md).
type Launch struct {
	Repo  string
	Quant string
	Args  []string
}

// DefaultSelection is the entry's config default: each choice's first option
// (docs/specs/CONFIG.md). A flat entry has nothing to pick, so its selection is
// empty — empty rather than nil, so a caller can layer stored and explicit picks
// straight over it.
//
// Every choice has an option to take: an axis with nothing to pick from never
// loads (schema.go, checkAtLeastOneOption).
func DefaultSelection(entry Entry) Selection {
	selection := make(Selection, len(entry.Choices))
	for _, choice := range entry.Choices {
		selection[choice.Name] = choice.Options[0].Name
	}
	return selection
}

// Resolve reads an entry through one selection and yields the facts a launch is
// composed from. The selection must name every one of the entry's choices and
// nothing else: by the time a start asks, the picks are settled, so a choice left
// unpicked is a caller that skipped its own merge, not a gap for this function to
// fill.
//
// Every refusal names what is valid, because it is printed to whoever typed the
// pick — on the command line or in the picker.
func Resolve(entry Entry, selection Selection) (Launch, error) {
	if err := refuseUnknownPicks(entry, selection); err != nil {
		return Launch{}, err
	}

	// Cloning keeps the composition off the entry's own args: appending to them
	// would write a pick into the loaded tree, and the next pick would read it.
	launch := Launch{Repo: entry.Repo, Quant: entry.Quant, Args: slices.Clone(entry.Args)}
	for _, choice := range entry.Choices {
		option, err := pickedOption(entry, choice, selection)
		if err != nil {
			return Launch{}, err
		}
		// Only one axis may replace the quant, and only one the repo (load.go,
		// resolveChoices), so the order these are applied in decides nothing.
		if option.Repo != "" {
			launch.Repo = option.Repo
		}
		if option.Quant != "" {
			launch.Quant = option.Quant
		}
		launch.Args = append(launch.Args, option.Args...)
	}
	return launch, nil
}

// refuseUnknownPicks answers a selection naming something the entry does not
// have. It runs before any pick is read so that a misspelled choice is reported
// as the misspelling it is: read in file order first, the same selection would
// come back as the real choice being unpicked, sending the author to look at the
// wrong half of the mistake.
func refuseUnknownPicks(entry Entry, selection Selection) error {
	var unknown []string
	for name := range selection {
		if !slices.ContainsFunc(entry.Choices, func(choice Choice) bool { return choice.Name == name }) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	// A map has no order, so a selection with more than one mistake in it always
	// reports the same one.
	slices.Sort(unknown)

	if len(entry.Choices) == 0 {
		return fmt.Errorf("entry %s has no choices, so there is nothing to pick; drop %s from the selection",
			entry.ID, strings.Join(unknown, ", "))
	}
	return fmt.Errorf("entry %s has no choice named %q; its choices are: %s",
		entry.ID, unknown[0], entry.choiceNames())
}

// pickedOption reads one choice's pick out of the selection.
func pickedOption(entry Entry, choice Choice, selection Selection) (ChoiceOption, error) {
	name, picked := selection[choice.Name]
	if !picked {
		return ChoiceOption{}, fmt.Errorf("entry %s: nothing picked for choice %q; its options are: %s",
			entry.ID, choice.Name, choice.optionNames())
	}
	for _, option := range choice.Options {
		if option.Name == name {
			return option, nil
		}
	}
	return ChoiceOption{}, fmt.Errorf("entry %s: choice %q has no option named %q; its options are: %s",
		entry.ID, choice.Name, name, choice.optionNames())
}

// choiceNames lists an entry's axes for a refusal, in file order — the order the
// author wrote them and the order everything else shows them in.
func (e Entry) choiceNames() string {
	names := make([]string, len(e.Choices))
	for i, choice := range e.Choices {
		names[i] = choice.Name
	}
	return strings.Join(names, ", ")
}

// optionNames lists a choice's picks for a refusal, in file order; the first one
// is the config default.
func (c Choice) optionNames() string {
	names := make([]string, len(c.Options))
	for i, option := range c.Options {
		names[i] = option.Name
	}
	return strings.Join(names, ", ")
}
