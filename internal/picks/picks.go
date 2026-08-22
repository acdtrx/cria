// Package picks is cria's memory of which option is picked on each of an entry's
// choices: ~/.local/state/cria/choices.json, one pick per entry per choice
// (docs/specs/CONFIG.md, Choices). Picks are state, not config — the config tree
// is human-owned and cria never writes into it — so they live next to the state
// records, in a file cria writes without being asked.
//
// The store is its own package because both frontends need it and neither owns
// it: the TUI picker is the only writer, while the CLI reads picks and records
// nothing (a `cria start <id> choice=option` pick overrides one launch and is
// gone after it, so an agent's experiment never changes what a bare start
// launches next).
//
// A stored pick is a default, and defaults go stale: the entry file it names is
// edited by hand, and cria only finds out on the next read. That is what Merge
// and Prune are for — a stale pick falls back to the config default and is
// dropped by the next write, and neither is ever an error.
package picks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"cria/internal/config"
)

// picksFile is the store's name under the state root.
const picksFile = "choices.json"

// Picks is the whole file: entry id → the picks stored for that entry. An entry
// with no key stored has no picks of its own, which is exactly what an entry
// whose picks are all the config defaults looks like — nothing distinguishes the
// two, and nothing needs to.
type Picks map[string]config.Selection

// picksPath is where one state root keeps the file. Resolving that root stays
// the caller's problem (serve.Root), like every other path under the state tree,
// so tests can point this elsewhere.
func picksPath(root string) string { return filepath.Join(root, picksFile) }

// Load reads the store, and always answers with usable picks.
//
// A file that is not there means nothing has been picked yet, which is not a
// problem to report: every entry runs its config defaults. A file cria cannot
// read or cannot parse is reported and the config defaults are used anyway —
// this is cria's own state, so a broken one is never worth refusing to launch
// over, and the next pick writes a good file over it (CLAUDE.md, feature-building
// mode).
func Load(root string) (Picks, error) {
	path := picksPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Picks{}, nil
	}
	if err != nil {
		return Picks{}, fmt.Errorf("cannot read the stored picks at %s: %w; using the config defaults", path, err)
	}

	stored, err := decodePicks(data)
	if err != nil {
		return Picks{}, fmt.Errorf("the stored picks at %s are unreadable: %w; using the config defaults, which the next pick writes over them", path, err)
	}
	return stored, nil
}

// decodePicks parses one store file, strictly. Strict cannot mean what it means
// for a record or for the UI preferences: every key here is data — an entry id,
// a choice name — so there is no unknown field to reject and
// DisallowUnknownFields would police nothing. What is left is the shape: the
// file must be exactly one JSON object of objects of strings, and every name in
// it must be a name. Anything else was hand-edited or written by a cria that no
// longer exists.
//
// Names are not checked against the config tree: the store knows nothing about
// it, and a pick naming an entry, a choice or an option that is gone is stale
// state — skipped by Merge and dropped by Prune, never a reason to call the
// whole file broken. An empty name is different: it can never match anything in
// any tree, so it is junk rather than staleness.
func decodePicks(data []byte) (Picks, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))

	var stored Picks
	if err := decoder.Decode(&stored); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, errors.New("the file holds more than one JSON document")
	}
	if stored == nil {
		return nil, errors.New("the file holds no picks object")
	}

	// Sorted, so a file with more than one thing wrong with it always reports
	// the same one.
	for _, id := range slices.Sorted(maps.Keys(stored)) {
		if id == "" {
			return nil, errors.New("a pick is filed under an empty entry id")
		}
		for _, choice := range slices.Sorted(maps.Keys(stored[id])) {
			if choice == "" {
				return nil, fmt.Errorf("entry %q has a pick for an unnamed choice", id)
			}
			if stored[id][choice] == "" {
				return nil, fmt.Errorf("entry %q picks nothing for choice %q", id, choice)
			}
		}
	}
	return stored, nil
}

// Save records the picks. The write lands through a temporary file and a rename,
// like a state record: a half-written store would be read back as a corrupt one
// on the next launch.
//
// Go writes a map's keys in sorted order, so the file stays stable across writes
// that change nothing — it is machine-owned, but it is read by hand exactly when
// somebody is wondering why a start launched what it launched.
func Save(root string, stored Picks) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("cannot create the state directory %s: %w", root, err)
	}

	// An empty store is an empty object, never a null: the file cria writes is
	// the file cria can read back.
	if stored == nil {
		stored = Picks{}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the stored picks: %w", err)
	}
	data = append(data, '\n')

	path := picksPath(root)
	temp := path + ".writing"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("cannot write the stored picks: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("cannot write the stored picks: %w", err)
	}
	return nil
}

// Merge is the total selection config.Resolve asks for, from the three layers a
// launch is decided by: the entry's config defaults, overlaid by the picks this
// store holds, overlaid by the explicit picks the caller was handed.
//
// The asymmetry between the two overlays is the point of the function. A stored
// pick naming a choice or an option the entry no longer has is skipped, and the
// config default stands in for it: the file is cria's own state and the entry
// file is the authority that moved, so stale picks are a thing to outgrow, not a
// thing to refuse over. An explicit pick naming nothing is refused: it was typed
// on a command line or handed over by an agent just now, and answering a
// misspelled `qunt=q4` by quietly launching something else is how a wrong model
// ends up serving.
//
// The refusal is config.Resolve's own, earned by resolving what the layers came
// to rather than by pre-checking the explicit picks: one wording reaches whoever
// typed the pick, naming the entry's real choices and options. Only an explicit
// pick can reach it — the stored ones are dropped before it looks — and the
// defaults make the selection total, so nothing here can be refused for being
// incomplete.
func Merge(entry config.Entry, stored, explicit config.Selection) (config.Selection, error) {
	selection := config.DefaultSelection(entry)
	for choice, option := range stored {
		if holdsOption(entry, choice, option) {
			selection[choice] = option
		}
	}
	maps.Copy(selection, explicit)

	if _, err := config.Resolve(entry, selection); err != nil {
		return nil, err
	}
	return selection, nil
}

// Prune is the store as the file should record it now: a pick whose entry,
// choice or option the tree no longer holds is dropped, and an entry left with
// no picks goes with them — an absent entry and an entry picking nothing say the
// same thing, so the file only ever spells it one way.
//
// A broken entry file keeps every pick it had. cria refused to read its choices,
// which is knowledge about the file's syntax and none at all about its axes; a
// typo must not be what erases what was picked (docs/specs/TUI.md settles the
// same rule for group membership).
//
// Nothing of the argument is kept: picks travel by reference, so the caller's map
// must not change under it when this result is the one being written. The result
// is always a store, empty when nothing survived — the shape Load answers with
// too, so no reader has to tell "no picks" apart from "no file".
func Prune(stored Picks, tree *config.Tree) Picks {
	pruned := make(Picks, len(stored))

	// A tree cria has not read yet holds no file to match against, and dropping
	// every pick is not what "not read yet" means.
	if tree == nil {
		for id, selection := range stored {
			pruned[id] = maps.Clone(selection)
		}
		return pruned
	}

	entries := make(map[string]config.Entry, len(tree.Entries))
	for _, entry := range tree.Entries {
		entries[entry.ID] = entry
	}
	broken := make(map[string]bool, len(tree.Broken))
	for _, entry := range tree.Broken {
		broken[entry.ID] = true
	}

	for id, selection := range stored {
		if broken[id] {
			pruned[id] = maps.Clone(selection)
			continue
		}
		entry, known := entries[id]
		if !known {
			continue
		}

		kept := make(config.Selection, len(selection))
		for choice, option := range selection {
			if holdsOption(entry, choice, option) {
				kept[choice] = option
			}
		}
		if len(kept) == 0 {
			continue
		}
		pruned[id] = kept
	}
	return pruned
}

// holdsOption answers whether one pick still names something the entry has —
// the question both stale-pick rules turn on.
func holdsOption(entry config.Entry, choice, option string) bool {
	for _, axis := range entry.Choices {
		if axis.Name != choice {
			continue
		}
		return slices.ContainsFunc(axis.Options, func(o config.ChoiceOption) bool { return o.Name == option })
	}
	return false
}
