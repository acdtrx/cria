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

// The router's store is the second thing cria remembers about what has been
// chosen: which entries this host's router holds, and the combination each is
// held under. Config declares what can vary and state holds what is chosen — the
// same doctrine as the picks beside it, one level up, because inclusion is a
// choice about entries rather than about one entry's axes.
//
// It lives under the router engine's own state folder rather than beside
// choices.json (docs/specs/SERVE.md, the engine state layout): it is the
// router's memory, and the picks it holds are the router's own — an entry
// included in the router may be held under a different combination than the one
// the llama engine starts it under, by design.
//
// routerModelsFile is that store's name inside the folder.
const routerModelsFile = "models.json"

// Router is the store: entry id → the picks that entry is held under. The key is
// the inclusion, so an entry held under its config defaults has an empty
// selection rather than no key — the two say different things here, unlike in
// choices.json where an entry picking nothing and an absent entry are the same.
type Router struct {
	Models map[string]config.Selection `json:"models"`
}

// IDs lists the entries the router holds, sorted — the order every surface
// prints them in, and the order the composed preset writes their sections in.
func (r Router) IDs() []string { return slices.Sorted(maps.Keys(r.Models)) }

// Holds reports whether the router holds one entry.
func (r Router) Holds(id string) bool {
	_, held := r.Models[id]
	return held
}

// Picks is the combination one entry is held under, as a copy: what the caller
// does with it afterwards must not rewrite the store.
func (r Router) Picks(id string) config.Selection { return maps.Clone(r.Models[id]) }

// Include holds an entry under one combination, replacing whatever it was held
// under before. The selection is copied for the same reason Picks hands out a
// copy.
func (r *Router) Include(id string, selection config.Selection) {
	if r.Models == nil {
		r.Models = map[string]config.Selection{}
	}
	held := config.Selection{}
	maps.Copy(held, selection)
	r.Models[id] = held
}

// Exclude drops an entry from the router. held is false when it was not one of
// its models, which is what makes a second exclude say so rather than report a
// change it did not make.
func (r *Router) Exclude(id string) bool {
	if !r.Holds(id) {
		return false
	}
	delete(r.Models, id)
	return true
}

// LoadRouter reads the store out of the router's state folder.
//
// A file that is not there means this host's router holds nothing yet — a fresh
// router, not a problem to report. Anything else is refused rather than degraded
// into an empty store, which is where this parts from the picks beside it: a
// stale pick has the config default to fall back on, while "which entries are
// included" has no answer cria can stand in for. An empty one would start a
// router serving nothing and look like success (CODING-RULES §4).
func LoadRouter(dir string) (Router, error) {
	path := routerModelsPath(dir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Router{Models: map[string]config.Selection{}}, nil
	}
	if err != nil {
		return Router{}, fmt.Errorf("cannot read the models the router holds at %s: %w", path, err)
	}

	held, err := decodeRouter(data)
	if err != nil {
		return Router{}, fmt.Errorf("the models the router holds, at %s, are unreadable: %w; fix that file, or delete it to start from an empty router", path, err)
	}
	return held, nil
}

// decodeRouter parses one store file, strictly. The shape is cria's own, so an
// unknown key or a wrong type is an error rather than a silent default
// (CLAUDE.md, feature-building mode) — and inside the models object every name
// is data, an entry id and a choice name, so what is checked there is that each
// one is a name at all.
//
// Names are not checked against the config tree: the store knows nothing about
// it, and an entry that has since been renamed is a model the router can no
// longer serve — reported when the preset is composed, naming the id, never
// dropped here.
func decodeRouter(data []byte) (Router, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var held Router
	if err := decoder.Decode(&held); err != nil {
		return Router{}, err
	}
	if decoder.More() {
		return Router{}, errors.New("the file holds more than one JSON document")
	}
	if held.Models == nil {
		return Router{}, errors.New("the file names no models object")
	}

	// Sorted, so a file with more than one thing wrong with it always reports the
	// same one.
	for _, id := range held.IDs() {
		if id == "" {
			return Router{}, errors.New("a model is held under an empty entry id")
		}
		if held.Models[id] == nil {
			return Router{}, fmt.Errorf("entry %q is held with no picks object; write {} for the entry's config defaults", id)
		}
		for _, choice := range slices.Sorted(maps.Keys(held.Models[id])) {
			if choice == "" {
				return Router{}, fmt.Errorf("entry %q is held with a pick for an unnamed choice", id)
			}
			if held.Models[id][choice] == "" {
				return Router{}, fmt.Errorf("entry %q picks nothing for choice %q", id, choice)
			}
		}
	}
	return held, nil
}

// SaveRouter records which models the router holds. The write lands through a
// temporary file and a rename, like a state record: a half-written store would
// be read back as a broken one, and this one refuses rather than degrading.
func SaveRouter(dir string, held Router) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create the router's state directory %s: %w", dir, err)
	}

	// An empty store is an empty object, never a null: the file cria writes is the
	// file cria can read back.
	if held.Models == nil {
		held.Models = map[string]config.Selection{}
	}
	data, err := json.MarshalIndent(held, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the models the router holds: %w", err)
	}
	data = append(data, '\n')

	path := routerModelsPath(dir)
	temp := path + ".writing"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("cannot write the models the router holds: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("cannot write the models the router holds: %w", err)
	}
	return nil
}

// routerModelsPath is where the store sits inside the router's state folder.
// Resolving that folder stays the caller's problem (serve.RouterStateDir), like
// every other path under the state tree.
func routerModelsPath(dir string) string { return filepath.Join(dir, routerModelsFile) }
