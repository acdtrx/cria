package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"cria/internal/config"
)

// prefsFile holds the UI's own memory, next to the state records rather than in
// the config tree: which backend the lists are showing, which entry was started
// last, and how the entry list is grouped. All of it is machine-owned — cria
// writes it without being asked — and the config tree is human-owned, so none of
// it may ever be recorded there (docs/specs/TUI.md).
const prefsFile = "ui.json"

// prefs is what the TUI remembers between launches. Backend is a sticky choice:
// running llama or mlx is a decision, not a per-session question. LastStarted is
// what the status box falls back to when nothing is running, so the server keys
// keep a target across sessions; the start action owns writing it. Groups
// partition the entry list; with none defined the list renders as one flat list.
type prefs struct {
	Backend     config.Backend `json:"backend"`
	LastStarted string         `json:"last_started,omitempty"`
	Groups      []entryGroup   `json:"groups,omitempty"`
}

// entryGroup is one named section of the entry list. The array order of the
// groups is their display order — groups are the only thing ordered by hand; the
// entries of a group carry membership only, since within a heading they render
// in the tree's alphabetical order. A group holding no entries is legal: a
// just-emptied group stays findable until it is filed into or disbanded.
type entryGroup struct {
	Name    string   `json:"name"`
	Entries []string `json:"entries"`
}

// defaultPrefs is a first launch: llama, and nothing started yet. llama is the
// default because it is the backend that exists on every host cria runs on —
// mlx_lm.server is Apple silicon only (docs/TECH-STACK.md).
func defaultPrefs() prefs { return prefs{Backend: config.BackendLlama} }

// other is the backend the toggle switches to.
func (p prefs) other() config.Backend {
	if p.Backend == config.BackendMLX {
		return config.BackendLlama
	}
	return config.BackendMLX
}

// prefsPath is where one state root keeps the file.
func prefsPath(root string) string { return filepath.Join(root, prefsFile) }

// loadPrefs reads the file, and always answers with usable preferences.
//
// A file that is not there is a first launch, not a problem: the defaults are
// the whole answer and there is nothing to report. A file cria cannot read or
// cannot parse is reported and the defaults are used anyway — this is cria's
// own state, so a broken one is never worth refusing to start over, and the
// next change writes a good file over it (CLAUDE.md, feature-building mode).
func loadPrefs(root string) (prefs, error) {
	path := prefsPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return defaultPrefs(), nil
	}
	if err != nil {
		return defaultPrefs(), fmt.Errorf("cannot read the UI preferences at %s: %w; starting with the defaults", path, err)
	}

	saved, err := decodePrefs(data)
	if err != nil {
		return defaultPrefs(), fmt.Errorf("the UI preferences at %s are unreadable: %w; starting with the defaults, which the next change writes over them", path, err)
	}
	return saved, nil
}

// decodePrefs parses one preferences file, strictly: an unknown key or a wrong
// type is an error rather than a silent default, exactly as a state record is
// read (docs/specs/SERVE.md). cria owns this format, so a file that does not
// match it was hand-edited or written by a cria that no longer exists.
func decodePrefs(data []byte) (prefs, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var saved prefs
	if err := decoder.Decode(&saved); err != nil {
		return prefs{}, err
	}
	if decoder.More() {
		return prefs{}, errors.New("the file holds more than one JSON document")
	}
	if saved.Backend != config.BackendLlama && saved.Backend != config.BackendMLX {
		return prefs{}, fmt.Errorf("backend is %q, want %q or %q", saved.Backend, config.BackendLlama, config.BackendMLX)
	}
	if err := validateGroups(saved.Groups); err != nil {
		return prefs{}, err
	}
	return saved, nil
}

// validateGroups holds every rule the group list must satisfy. A name is how a
// group is shown and picked, so an unnamed or repeated one leaves the list
// ambiguous; an entry belongs to at most one group, so an id filed twice has no
// answer to which heading it renders under (docs/specs/TUI.md).
//
// Ids are not checked against the config tree: preferences know nothing about
// it, and an id whose entry is gone is skipped on render and pruned on the next
// write rather than making the whole file invalid.
func validateGroups(groups []entryGroup) error {
	named := make(map[string]bool, len(groups))
	filedIn := make(map[string]string)
	for _, group := range groups {
		if group.Name == "" {
			return errors.New("a group has no name")
		}
		if named[group.Name] {
			return fmt.Errorf("two groups are named %q", group.Name)
		}
		named[group.Name] = true

		for _, id := range group.Entries {
			owner, taken := filedIn[id]
			if taken && owner == group.Name {
				return fmt.Errorf("group %q holds entry %q twice", group.Name, id)
			}
			if taken {
				return fmt.Errorf("entry %q is in both group %q and group %q", id, owner, group.Name)
			}
			filedIn[id] = group.Name
		}
	}
	return nil
}

// savePrefs records a change. The write lands through a temporary file and a
// rename, like a state record: a half-written preferences file would be read
// back as a corrupt one on the next launch.
func savePrefs(root string, saved prefs) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("cannot create the state directory %s: %w", root, err)
	}

	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the UI preferences: %w", err)
	}
	data = append(data, '\n')

	path := prefsPath(root)
	temp := path + ".writing"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("cannot write the UI preferences: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("cannot write the UI preferences: %w", err)
	}
	return nil
}
