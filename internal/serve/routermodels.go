package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"cria/internal/config"
	"cria/internal/engine"
	"cria/internal/picks"
)

// Which models the router holds is state, not config: the tree declares entries
// and the router's own store says which of them it serves and under which
// combination (OVERVIEW ruling 2, internal/picks). This file is where that store
// meets the tree — one preset composed from both — and where cria asks a running
// router what each of those models is doing.
//
// The router's children are the router's own processes. cria never looks for
// them in the process table: it asks the router, which publishes one row per
// model it holds (docs/specs/SERVE.md).

const (
	// routerChildrenTimeout bounds the one look at what the router holds. It sits
	// where a health probe sits — on a status, on a refresh — and asks a port on
	// this machine, so a router that has not answered in two seconds is not
	// answering.
	routerChildrenTimeout = 2 * time.Second

	// routerLoadBudget bounds a load or an unload. A load is a model coming into
	// memory, which for a large one is minutes of disk and GPU work, and it was
	// asked for deliberately — the same budget a warm gets (warm.go), for the same
	// reason.
	routerLoadBudget = 15 * time.Minute
)

// RouterModels is what the router serves, as composing its preset judged it: the
// file's whole text, the models that got a section, and the ones that got none
// with the reason each was refused.
//
// A refused model never fails the router. One entry whose args cannot be written
// as preset keys, or whose file has since gone, would otherwise take every other
// model down with it — so the verdict is per model, and the surfaces print it
// (docs/specs/SERVE.md).
type RouterModels struct {
	Preset  string           // the composed preset, exactly as it is written at a start
	Served  []ServedModel    // ordered by entry id
	Skipped []engine.Skipped // ordered by entry id
}

// ServedModel is one entry the router serves: the id clients address it by — the
// alias its section carries — the model reference that section is named after,
// and the combination it is held under.
type ServedModel struct {
	ID        string           `json:"id"`
	Repo      string           `json:"repo"`
	Quant     string           `json:"quant,omitempty"`
	Selection config.Selection `json:"selection,omitempty"`
}

// RouterModels reads the store of models the router holds and composes what it
// would serve from the tree as it stands now.
//
// It is pure: nothing is written and no request is made, so it answers the same
// whether or not a router is running — what the next start would serve, and what
// the one that is running was started from.
//
// Two things refuse outright rather than per model: a store cria cannot read, and
// an engine file whose own args cannot become the preset's defaults. Both are the
// router's configuration rather than one of its models.
func (m *Manager) RouterModels(tree *config.Tree) (RouterModels, error) {
	held, err := picks.LoadRouter(m.routerStateRoot())
	if err != nil {
		return RouterModels{}, err
	}

	var models []engine.RouterModel
	resolved := map[string]ServedModel{}
	refused := map[string]string{}
	for _, id := range held.IDs() {
		entry, found := tree.Entry(id)
		if !found {
			refused[id] = missingEntry(tree, id)
			continue
		}
		if err := engine.RouterServes(entry.Backend); err != nil {
			refused[id] = err.Error()
			continue
		}

		// The picks the router holds it under, with the entry's config defaults
		// under them: a pick naming an option the file no longer has falls back the
		// way every stored pick does (internal/picks, Merge). Inclusion itself never
		// falls back — that is the difference between a stale pick and a model
		// somebody asked the router to hold.
		selection, err := picks.Merge(entry, held.Picks(id), nil)
		if err != nil {
			refused[id] = err.Error()
			continue
		}

		// No engine args: the router's own are the preset's [*] section, written
		// once, and upstream applies them under every model section it reads
		// (config.ResolveUnder).
		launch, err := config.ResolveUnder(entry, selection, nil)
		if err != nil {
			refused[id] = err.Error()
			continue
		}

		models = append(models, engine.RouterModel{ID: id, Repo: launch.Repo, Quant: launch.Quant, Args: launch.Args})
		resolved[id] = ServedModel{ID: id, Repo: launch.Repo, Quant: launch.Quant, Selection: selection}
	}

	composed, err := engine.RouterPreset(tree.Router.Args, models)
	if err != nil {
		return RouterModels{}, fmt.Errorf("cannot compose the router's preset: %w", err)
	}
	for _, skipped := range composed.Skipped {
		refused[skipped.ID] = skipped.Reason
	}

	serving := RouterModels{Preset: composed.Preset}
	for _, id := range held.IDs() {
		if reason, skipped := refused[id]; skipped {
			serving.Skipped = append(serving.Skipped, engine.Skipped{ID: id, Reason: reason})
			continue
		}
		serving.Served = append(serving.Served, resolved[id])
	}
	return serving, nil
}

// missingEntry is why an id the router holds names nothing the tree declares.
//
// A broken file gets its own reason: the author needs the offending key, not the
// news that their entry has disappeared (docs/specs/CONFIG.md). Either way the
// inclusion stays — it was asked for, and a rename or a typo must not silently
// empty the router.
func missingEntry(tree *config.Tree, id string) string {
	for _, broken := range tree.Broken {
		if broken.ID == id {
			return fmt.Sprintf("its file no longer loads (%s: %v); fix that file and the router serves it again", broken.Path, broken.Err)
		}
	}
	return fmt.Sprintf("the tree declares no entry named %q any more; write that file again, or exclude it from the router", id)
}

// RouterChildren is what the router said about the models it holds, or why it
// said nothing. The detail is never an error: which models are loaded is
// something to display, exactly like a health probe, and a router that is still
// coming up has nothing to answer with yet.
type RouterChildren struct {
	Children []RouterChild
	Detail   string // why there are none; empty when the router answered
}

// RouterChild is one model the router holds, as the router reports it: the
// reference it lists the model under, the names it answers to — cria includes
// entries under their id, which is the alias its section carries — the state in
// the router's own words, and cria's phase where its vocabulary can say the same
// thing.
type RouterChild struct {
	Model   string   `json:"model"`
	Aliases []string `json:"aliases,omitempty"`
	State   string   `json:"state"`
	Phase   Phase    `json:"phase,omitempty"`
}

// Child finds the model one name addresses among the ones the router holds,
// matching the aliases as well as the reference — a client names a model by its
// entry id, and so does cria.
func (c RouterChildren) Child(name string) (RouterChild, bool) {
	for _, child := range c.Children {
		if child.Model == name || slices.Contains(child.Aliases, name) {
			return child, true
		}
	}
	return RouterChild{}, false
}

// routerChildren asks a router which models it holds; routerCommander sends it
// one of the two requests that load and unload one. Both are transports, injected
// like every other request this package makes, so the rules around them run with
// no router and no port.
type (
	routerChildren  func(url string) RouterChildren
	routerCommander func(url, model string) error
)

// RouterChildren asks the router what it holds right now.
func (m *Manager) RouterChildren(record Record) RouterChildren {
	return m.children(serverURL(record, engine.RouterModelsPath()))
}

// RouterLoad and RouterUnload are the two deliberate verbs over the models the
// router holds: load one now, or let one go.
//
// The router loads a model on the request that names it all by itself, which is
// the feature clients rely on. These exist because a mechanism must be
// invocable on its own (CODING-RULES §7): a model wanted resident before the
// first request, or one holding memory that is needed elsewhere, is a thing to
// ask for rather than a thing to arrange by sending a fake completion.
func (m *Manager) RouterLoad(record Record, id string) error {
	return m.commandRouter(serverURL(record, engine.RouterLoadPath()), id)
}

func (m *Manager) RouterUnload(record Record, id string) error {
	return m.commandRouter(serverURL(record, engine.RouterUnloadPath()), id)
}

// RouterGenerating reports whether one of the router's models is answering
// somebody right now — the question that decides whether unloading it would cut
// somebody off.
//
// It is the same per-slot signal an entry's server publishes, asked of the
// child the router runs for one model: the endpoint takes the model's name, and
// the name is the alias cria included it under (verified live 2026-08-31). The
// caller names a model the router listed, so an id that addresses nothing is a
// refusal it has already given.
func (m *Manager) RouterGenerating(record Record, id string) Generation {
	served, err := engine.For(record.Backend)
	if err != nil {
		return unverifiable(err.Error())
	}
	path, published := served.SlotsPath()
	if !published {
		return unverifiable(fmt.Sprintf("%s publishes no per-slot signal, so cria cannot tell whether %s is generating", served.Program(), id))
	}
	return m.slots(serverURL(record, engine.RouterModelQuery(path, id)))
}

// routerModelRow is what cria reads of one row of the router's model listing.
// The endpoint publishes far more per model — where its weights are, what it can
// take as input, what it was launched with — and it is the router's payload to
// grow: unknown fields are ignored rather than refused, because this reads three
// values out of somebody else's API (CODING-RULES §4).
type routerModelRow struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	Status  struct {
		Value string `json:"value"`
	} `json:"status"`
}

// routerChildPhase is the router's word for one model in cria's own vocabulary,
// and it is deliberately partial.
//
// Three of the states a router publishes say the same thing cria's phases do: a
// model whose weights are being fetched is downloading, one whose child is
// coming up is starting, and one being served is running. The other three —
// downloaded, unloaded, sleeping — all mean the router holds this model and
// nothing is resident for it, which no phase of cria's says: a server is not
// "starting" when nobody has asked for it, and it has not "exited" when it was
// never launched.
//
// So those get no phase at all, and every surface shows the router's own word
// instead. An answer that is visibly absent beats one that is plausible and
// wrong (CODING-RULES §4) — and a state upstream adds later lands in the same
// place rather than being read as something cria knows.
func routerChildPhase(state string) Phase {
	switch state {
	case engine.RouterDownloading:
		return PhaseDownloading
	case engine.RouterLoading:
		return PhaseStarting
	case engine.RouterLoaded:
		return PhaseRunning
	default:
		return ""
	}
}

// newHTTPRouterChildren builds the real listing reader: one GET, no retry.
// Everything that can stand between it and the listing — a router that is not
// answering yet, a build whose endpoint is not there, a payload cria cannot read
// — comes back as the same answer, carrying what happened.
func newHTTPRouterChildren() routerChildren {
	client := &http.Client{Timeout: routerChildrenTimeout}
	return func(url string) RouterChildren {
		response, err := client.Get(url)
		if err != nil {
			return unheard(fmt.Sprintf("%s: %s", url, requestFailure(err, routerChildrenTimeout)))
		}
		defer response.Body.Close()

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return unheard(fmt.Sprintf("%s answered %s%s", url, response.Status, refusal(response.Body)))
		}

		var listing struct {
			Data []routerModelRow `json:"data"`
		}
		if err := json.NewDecoder(response.Body).Decode(&listing); err != nil {
			return unheard(fmt.Sprintf("%s answered something cria cannot read as a model listing: %v", url, err))
		}

		children := make([]RouterChild, 0, len(listing.Data))
		for _, row := range listing.Data {
			children = append(children, RouterChild{
				Model:   row.ID,
				Aliases: row.Aliases,
				State:   row.Status.Value,
				Phase:   routerChildPhase(row.Status.Value),
			})
		}
		return RouterChildren{Children: children}
	}
}

// unheard is the answer to everything that stands between cria and the router's
// listing, carrying what happened so a caller can name it.
func unheard(detail string) RouterChildren { return RouterChildren{Detail: detail} }

// newHTTPRouterCommand builds the real load/unload sender: one POST naming the
// model, and whatever the router answers is the verdict. A refusal is quoted as
// the router gave it, which is the only account of it cria has.
func newHTTPRouterCommand() routerCommander {
	client := &http.Client{Timeout: routerLoadBudget}
	return func(url, model string) error {
		body, err := json.Marshal(struct {
			Model string `json:"model"`
		}{Model: model})
		if err != nil {
			return err
		}

		response, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("%s: %s", url, requestFailure(err, routerLoadBudget))
		}
		defer response.Body.Close()

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("%s answered %s%s", url, response.Status, refusal(response.Body))
		}
		return nil
	}
}
