package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/picks"
	"cria/internal/procs"
)

// routerStore writes what the router holds, where a manager reads it back from.
func routerStore(t *testing.T, manager *Manager, held picks.Router) {
	t.Helper()
	if err := picks.SaveRouter(manager.routerStateRoot(), held); err != nil {
		t.Fatalf("writing the models the router holds: %v", err)
	}
}

// servedIDs names the models one composition carries, in the order its sections
// were written.
func servedIDs(models RouterModels) []string {
	ids := make([]string, 0, len(models.Served))
	for _, model := range models.Served {
		ids = append(ids, model.ID)
	}
	return ids
}

// writeBrokenRouterStore leaves a store file no cria can read where the router's
// own state lives.
func writeBrokenRouterStore(manager *Manager) error {
	if err := os.MkdirAll(manager.routerStateRoot(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(manager.routerStateRoot(), "models.json"), []byte(`{"models": {"qwen": "q4"}}`), 0o644)
}

// routerRecordAt is the record of a router serving where a test server listens.
func routerRecordAt(t *testing.T, address string) Record {
	t.Helper()
	record := recordAt(t, llamaEntry(), address)
	record.EntryID = string(config.BackendRouter)
	record.Backend = config.BackendRouter
	record.Repo, record.Quant = "", ""
	record.Preset = "/state/engines/router/preset.ini"
	return record
}

// The router serves the entries its own store holds, each under the combination
// that store holds it in — the entry's own picks, resolved as the router serves
// them: the entry's args and its picked options', with the engine level left to
// the preset's [*] section rather than merged into every model's.
func TestTheRouterServesTheModelsItsStoreHolds(t *testing.T) {
	manager := newManager(t, &fakeHost{})

	flat := llamaEntry()
	flat.EngineArgs = []string{"-ngl", "99"} // engines/llama.toml: the llama engine's, not the router's
	varied := choicesEntry()
	varied.EngineArgs = flat.EngineArgs

	held := picks.Router{}
	held.Include(flat.ID, nil)
	held.Include(varied.ID, config.Selection{"quant": "q6", "context": "long"})
	routerStore(t, manager, held)

	models, err := manager.RouterModels(routerTree(routerConfig(), flat, varied))
	if err != nil {
		t.Fatalf("reading what the router serves: %v", err)
	}

	if got := strings.Join(servedIDs(models), ", "); got != "qwen, qwen-choices" {
		t.Errorf("the router serves %q, want both entries its store holds, by id", got)
	}
	if len(models.Skipped) != 0 {
		t.Errorf("the router skipped %+v, want nothing", models.Skipped)
	}

	want := "[*]\nngl = 99\nfa = on\n" +
		"\n[unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL]\nalias = qwen\nctx-size = 16384\n" +
		"\n[unsloth/Qwen3-30B-A3B-GGUF:UD-Q6_K_XL]\nalias = qwen-choices\nctx-size = 16384\nn-cpu-moe = 12\ncache-type-k = f16\n"
	if models.Preset != want {
		t.Errorf("the preset is\n%s\nwant\n%s", models.Preset, want)
	}

	// The picked quant is what the section is named after, and the picks are
	// carried on the model so a surface can show what it is held under.
	held6 := models.Served[1]
	if held6.Quant != "UD-Q6_K_XL" || held6.Selection["context"] != "long" {
		t.Errorf("the router holds %+v, want the combination its store named", held6)
	}
}

// The router's picks are its own. The same entry can be held here under one
// combination while a bare `cria start` launches it under another — that is the
// whole reason inclusion lives in the router's state rather than in the entry
// (OVERVIEW ruling 2).
func TestTheRoutersPicksAreNotTheEntrysStoredPicks(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	entry := choicesEntry()

	// What a bare start would launch: the entry's own stored picks.
	if err := picks.Save(manager.root, picks.Picks{entry.ID: {"quant": "q4", "context": "short"}}); err != nil {
		t.Fatalf("writing the entry's picks: %v", err)
	}
	held := picks.Router{}
	held.Include(entry.ID, config.Selection{"quant": "q6"})
	routerStore(t, manager, held)

	models, err := manager.RouterModels(routerTree(routerConfig(), entry))
	if err != nil {
		t.Fatalf("reading what the router serves: %v", err)
	}

	if len(models.Served) != 1 || models.Served[0].Quant != "UD-Q6_K_XL" {
		t.Fatalf("the router serves %+v, want the quantization its own store picked", models.Served)
	}
	// The choice the router's store says nothing about falls back to the entry's
	// config default, never to what the entry's own picks say.
	if got := models.Served[0].Selection["context"]; got != "short" {
		t.Errorf("the unpicked axis resolved to %q, want the entry's config default", got)
	}
	if strings.Contains(models.Preset, "cache-type-k = f16") {
		t.Errorf("the preset is\n%s\nwant the config default's args, not the entry's stored pick", models.Preset)
	}
}

// A model the router cannot carry is skipped with its reason and costs no other
// model its section: one entry served by another program, one whose file is
// gone, one whose file no longer loads, and one whose args cannot be written as
// preset keys — with the good entry served through all of it.
func TestModelsTheRouterCannotCarryAreSkippedNotFatal(t *testing.T) {
	manager := newManager(t, &fakeHost{})

	good := llamaEntry()
	mlx := llamaEntry()
	mlx.ID, mlx.Backend, mlx.Quant = "qwen-mlx", config.BackendMLX, ""
	unwritable := llamaEntry()
	unwritable.ID = "qwen-twice"
	unwritable.Repo = "unsloth/Qwen3-8B-GGUF"
	unwritable.Args = []string{"--override-kv", "a=int:1", "--override-kv", "b=int:2"}

	held := picks.Router{}
	for _, id := range []string{good.ID, mlx.ID, unwritable.ID, "gone", "broken"} {
		held.Include(id, nil)
	}
	routerStore(t, manager, held)

	tree := routerTree(routerConfig(), good, mlx, unwritable)
	tree.Broken = []config.BrokenEntry{{ID: "broken", Path: "/home/u/.config/cria/models/broken.toml", Err: &config.KeyError{Key: "port", Reason: "want an integer"}}}

	models, err := manager.RouterModels(tree)
	if err != nil {
		t.Fatalf("reading what the router serves: %v", err)
	}

	if got := strings.Join(servedIDs(models), ", "); got != "qwen" {
		t.Errorf("the router serves %q, want the one entry it can carry", got)
	}
	reasons := map[string]string{}
	for _, skipped := range models.Skipped {
		reasons[skipped.ID] = skipped.Reason
	}
	for id, want := range map[string]string{
		"qwen-mlx":   "mlx_lm.server",
		"qwen-twice": "more than once",
		"gone":       "declares no entry",
		"broken":     "no longer loads",
	} {
		if !strings.Contains(reasons[id], want) {
			t.Errorf("%s was skipped with %q, want a reason naming %q", id, reasons[id], want)
		}
	}
	if len(models.Skipped) != 4 {
		t.Errorf("the router skipped %+v, want one verdict per model it could not carry", models.Skipped)
	}
	if !strings.Contains(models.Preset, "alias = qwen\n") {
		t.Errorf("the preset is\n%s\nwant the entry that could be carried", models.Preset)
	}
}

// A store cria cannot read refuses outright: which entries the router holds has
// no default to fall back on, and an empty answer would start a router serving
// nothing and call it a start.
func TestARouterStoreThatCannotBeReadRefusesTheStart(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	spawner := &fakeSpawner{pid: 4242}
	manager.spawn = spawner.launch

	if err := writeBrokenRouterStore(manager); err != nil {
		t.Fatalf("writing a broken store: %v", err)
	}

	_, _, err := manager.StartRouter(routerTree(routerConfig(), llamaEntry()), routerReport())
	if err == nil {
		t.Fatal("the router started from a store cria cannot read")
	}
	if !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("the refusal reads %v, want it to say the store cannot be read", err)
	}
	if len(spawner.launches) != 0 {
		t.Errorf("cria spawned %v after refusing the start", spawner.launches)
	}
}

// A pick changed on one of the router's models is served by the next start: the
// preset is composed at every start (docs/specs/SERVE.md), so the section that
// model is written as is the only thing that changes.
func TestAPickChangeRegeneratesTheSectionAtTheNextStart(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	entry := choicesEntry()
	tree := routerTree(routerConfig(), entry)

	held := picks.Router{}
	held.Include(entry.ID, config.Selection{"quant": "q4", "context": "short"})
	routerStore(t, manager, held)

	record, _ := startRouterFor(t, manager, host, tree, 4242)
	before := readFile(t, manager.routerPresetPath())
	if err := manager.Stop(record); err != nil {
		t.Fatalf("stopping the router: %v", err)
	}

	held.Include(entry.ID, config.Selection{"quant": "q4", "context": "long"})
	routerStore(t, manager, held)
	startRouterFor(t, manager, host, tree, 4343)
	after := readFile(t, manager.routerPresetPath())

	added, removed := lineDiff(before, after)
	if strings.Join(removed, ",") != "cache-type-k = q8_0" || strings.Join(added, ",") != "cache-type-k = f16" {
		t.Errorf("the preset went from\n%s\nto\n%s\nwant exactly the picked option's line to change (removed %v, added %v)",
			before, after, removed, added)
	}
}

// startRouterFor starts the router over one tree, through a fake spawner.
func startRouterFor(t *testing.T, manager *Manager, host *fakeHost, tree *config.Tree, pid int) (Record, RouterModels) {
	t.Helper()
	spawner := &fakeSpawner{pid: pid}
	manager.spawn = spawner.launch
	if host.alive == nil {
		host.alive = map[int]procs.Identity{}
	}
	host.alive[pid] = identityOf("/opt/homebrew/bin/llama-server --models-preset " + manager.routerPresetPath())

	record, models, err := manager.StartRouter(tree, routerReport())
	if err != nil {
		t.Fatalf("starting the router: %v", err)
	}
	return record, models
}

// lineDiff is what changed between two composed presets, in lines.
func lineDiff(before, after string) (added, removed []string) {
	was := map[string]bool{}
	for _, line := range strings.Split(before, "\n") {
		was[line] = true
	}
	is := map[string]bool{}
	for _, line := range strings.Split(after, "\n") {
		is[line] = true
		if !was[line] && line != "" {
			added = append(added, line)
		}
	}
	for _, line := range strings.Split(before, "\n") {
		if !is[line] && line != "" {
			removed = append(removed, line)
		}
	}
	return added, removed
}

// What each of the router's models is doing comes from the router's own listing,
// read as it publishes it: the reference it lists, the aliases it answers to —
// the entry ids cria included them under — and its own word for the state.
func TestTheRoutersChildrenComeFromItsModelListing(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[
			{"id":"unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL","aliases":["qwen"],"status":{"value":"loaded","args":["llama-server"]},"can_remove":true},
			{"id":"LiquidAI/LFM2.5-2.6B-GGUF:Q8_0","aliases":["lfm"],"status":{"value":"unloaded"}},
			{"id":"ggml-org/gemma-3-4b-it-GGUF:Q4_K_M","status":{"value":"loading"}}
		]}`)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	children := manager.RouterChildren(routerRecordAt(t, server.URL))

	if children.Detail != "" {
		t.Fatalf("the router answered, but cria reported %q", children.Detail)
	}
	if path != "/models" {
		t.Errorf("cria asked %q, want the documented model listing", path)
	}
	if len(children.Children) != 3 {
		t.Fatalf("the router holds %+v, want the three models it listed", children.Children)
	}

	first := children.Children[0]
	if first.Model != "unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL" || strings.Join(first.Aliases, ",") != "qwen" {
		t.Errorf("the first model reads %+v, want the reference and alias the router published", first)
	}
	if first.State != "loaded" || first.Phase != PhaseRunning {
		t.Errorf("a loaded model reads %q/%q, want the router's word and cria's running", first.State, first.Phase)
	}

	// The name a client sends is the alias, and that is the name cria addresses
	// too — matching the reference as well, for a model the router found itself.
	if _, held := children.Child("qwen"); !held {
		t.Error("the model included as qwen is not addressable by that name")
	}
	if _, held := children.Child("ggml-org/gemma-3-4b-it-GGUF:Q4_K_M"); !held {
		t.Error("a model with no alias is not addressable by its reference")
	}
	if _, held := children.Child("nothing-of-the-sort"); held {
		t.Error("a name the router did not list was reported as one of its models")
	}
}

// The router's states become cria's phases where cria's vocabulary says the same
// thing, and nothing where it does not: a model the router holds but has not
// loaded is neither starting nor exited, and inventing a phase for it would
// be an answer with no evidence under it.
func TestTheChildStatesMapOntoTheirPhases(t *testing.T) {
	tests := map[string]Phase{
		"downloading": PhaseDownloading,
		"loading":     PhaseStarting,
		"loaded":      PhaseRunning,
		"downloaded":  "",
		"unloaded":    "",
		"sleeping":    "",
		"reticulated": "", // a state upstream may add: shown as written, never guessed at
	}

	for state, want := range tests {
		if got := routerChildPhase(state); got != want {
			t.Errorf("a %q model reads as phase %q, want %q", state, got, want)
		}
	}
}

// A router that cannot be asked is a display fact, not a failure: whatever stood
// in the way is carried so a surface can say it.
func TestARouterThatCannotBeAskedSaysWhy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"not found"}`)
	}))
	record := routerRecordAt(t, server.URL)
	manager := newManager(t, &fakeHost{})

	children := manager.RouterChildren(record)
	if len(children.Children) != 0 || !strings.Contains(children.Detail, "404") {
		t.Errorf("a router answering 404 came back as %+v, want the refusal it gave", children)
	}

	server.Close()
	children = manager.RouterChildren(record)
	if len(children.Children) != 0 || children.Detail == "" {
		t.Errorf("a router that is not answering came back as %+v, want why cria could not ask", children)
	}
}

// Loading and unloading one of the router's models is one POST naming it, and
// the router's answer is the verdict.
func TestLoadingAndUnloadingOneOfTheRoutersModels(t *testing.T) {
	var asked []string
	var sent struct {
		Model string `json:"model"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.URL.Path)
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("cria sent something the router cannot read: %v", err)
		}
		if r.URL.Path == "/models/unload" {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"no such model"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := newManager(t, &fakeHost{})
	record := routerRecordAt(t, server.URL)

	if err := manager.RouterLoad(record, "qwen"); err != nil {
		t.Fatalf("loading a model the router holds: %v", err)
	}
	if sent.Model != "qwen" {
		t.Errorf("cria asked the router to load %q, want the name it was given", sent.Model)
	}

	err := manager.RouterUnload(record, "qwen")
	if err == nil {
		t.Fatal("a refused unload came back as done")
	}
	for _, want := range []string{"500 Internal Server Error", "no such model"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure reads %v, want it to carry %q", err, want)
		}
	}
	if strings.Join(asked, ", ") != "POST /models/load, POST /models/unload" {
		t.Errorf("cria sent %v, want one POST to each documented endpoint", asked)
	}
}

// Whether one of the router's models is answering somebody is the same per-slot
// signal an entry's server publishes, asked of the child by the name the model
// was included under.
func TestWhetherOneOfTheRoutersModelsIsGenerating(t *testing.T) {
	manager := newManager(t, &fakeHost{})
	var asked string
	manager.slots = func(url string) Generation {
		asked = url
		return Generation{Busy: BusyGenerating}
	}

	record := routerRecordAt(t, "http://127.0.0.1:11434")
	if got := manager.RouterGenerating(record, "qwen-q6"); got.Busy != BusyGenerating {
		t.Errorf("the model reads %q, want the answer the slot signal gave", got.Busy)
	}
	if want := "http://127.0.0.1:11434/slots?model=qwen-q6"; asked != want {
		t.Errorf("cria asked %q, want the child addressed by its alias: %q", asked, want)
	}
}

// Inside one model's section the levels compose the way they compose on a
// command line: a picked option replaces the flag group the entry set, and the
// section carries one key with the picked value rather than two lines upstream
// would have to choose between.
func TestAPickedOptionReplacesTheEntrysFlagInItsSection(t *testing.T) {
	manager := newManager(t, &fakeHost{})

	entry := llamaEntry()
	entry.ID = "qwen-context"
	entry.Args = []string{"--ctx-size", "16384", "--jinja"}
	entry.Choices = []config.Choice{{Name: "context", Options: []config.ChoiceOption{
		{Name: "short"},
		{Name: "long", Args: []string{"--ctx-size", "262144"}},
	}}}

	held := picks.Router{}
	held.Include(entry.ID, config.Selection{"context": "long"})
	routerStore(t, manager, held)

	models, err := manager.RouterModels(routerTree(routerConfig(), entry))
	if err != nil {
		t.Fatalf("reading what the router serves: %v", err)
	}

	want := "[*]\nngl = 99\nfa = on\n" +
		"\n[unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL]\nalias = qwen-context\nctx-size = 262144\njinja = true\n"
	if models.Preset != want {
		t.Errorf("the preset is\n%s\nwant the picked option's value in the entry's own place:\n%s", models.Preset, want)
	}
}
