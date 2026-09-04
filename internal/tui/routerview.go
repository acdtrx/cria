package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/format"
	"cria/internal/picks"
	"cria/internal/serve"
)

// The router's screen: every profile this host's router could serve, which of
// them it holds, what each of those is doing right now, and — for the one under
// the cursor — the entry it was composed from and the preset section a start
// would write for it.
//
// It is the third position of the engine toggle rather than a view of its own
// (docs/specs/TUI.md). The toggle chooses what the screen is about, and the
// router's answer is the llama entries read through one question: is this one in
// the router? Everything else about the screen is the entry view's — two panes,
// one cursor, the same detail pane hierarchy, the same groups in the same order
// — because it is the same list asked something else.
//
// Inclusion is the store's, and the store is what the mark in front of each row
// carries. What the *running* router holds is drawn onto the rows where it can
// be: the store is the next start, the running router is right now, and a model
// included since that router started reads as included and not yet served rather
// than disappearing from a list the operator just wrote.

// The words a row carries where the router's own word for a model would go.
const (
	// routerNoSection is a model the preset could not carry. It has no state to
	// report because it was never offered to the router at all.
	routerNoSection = "skipped"

	// routerNotRunning is an included model's state while there is no router: the
	// store says what the next start would serve, and nothing is serving it yet.
	routerNotRunning = "not running"

	// routerNotServed is an included model the running router does not list —
	// included after it started, since the preset is composed at every start and
	// never edited (docs/specs/SERVE.md). The pane says the rest.
	routerNotServed = "not served"

	// routerUnasked is a router that could not be asked what it holds.
	routerUnasked = "unasked"
)

// The mark in front of a row: whether the router holds this profile. The glyphs
// are the entry list's own (serveview.go), asked the question this list is
// about — lit means the next start serves it, hollow means it does not. A store
// cria could not read leaves the same "·" a failed cache walk does: "not
// included" would be a claim cria has not earned (CODING-RULES §4).
const (
	includedMark = "●"
	excludedMark = "○"
)

// routerState is this host's router as the last refresh saw it: whether cria
// holds a record of one and what an observation of it says, what the store and
// the tree compose it to serve, and what the running one answers that it holds.
//
// The three are read together on every tick, and each stands alone: a router
// that is not running still has models to list, and a store cria cannot read
// still leaves a running router to observe.
type routerState struct {
	found    bool
	status   serve.Status
	models   serve.RouterModels
	children serve.RouterChildren

	// The composition succeeded, so the store was read and which profiles are
	// included is known. It is its own fact rather than "models is empty":
	// a host whose router holds nothing composes an empty listing, and that is a
	// true answer, where a store cria could not read has no answer at all.
	composed bool

	// Why the router could not be read — its store, its record, or the
	// observation of it. It is held and shown the way a failed observation of
	// the entries' servers is (tui.go, notes): an empty list would say "the
	// router holds nothing", which is a plausible-looking lie about something
	// cria could not ask (CODING-RULES §4).
	failure error
}

// live reports whether there is a router answering for the models in the list.
func (r routerState) live() bool { return r.found && r.status.Phase != serve.PhaseExited }

// routerRow is one line of the router's list: a profile the router can serve, an
// entry file cria refused, or an id the store holds that no profile of this tree
// answers for any more. It carries what the store says about it, what composing
// the preset made of it, and what the running router says about it.
type routerRow struct {
	id     string
	entry  config.Entry        // the profile; zero for a refused file and for a ghost
	broken *config.BrokenEntry // the file cria refused; nil for every other row
	ghost  bool                // held under an id this tree has no profile for

	included bool
	model    serve.ServedModel // what the preset composed for it; zero unless it got a section
	reason   string            // why it got none; empty for a model that did

	child   serve.RouterChild // what the router says about it; zero when it says nothing
	held    bool              // the running router lists it
	running bool              // there is a router to have listed it
}

// skipped reports whether the preset left this included model out.
func (r routerRow) skipped() bool { return r.reason != "" }

// includable reports whether this row is a profile the router could be told to
// hold. A refused file is not one — the key that would say which program serves
// it is exactly the key that could not be read — and a ghost is an inclusion
// with no profile left behind it, so the only thing either can do is come out
// (docs/specs/CLI.md, `cria router exclude`).
func (r routerRow) includable() bool { return r.broken == nil && !r.ghost }

// routerSection is one heading's worth of the router's list: the group it stands
// for, and the rows under it. The list is the entry list's own sections read
// through the router's question, so a profile sits under the same heading in
// both positions of the toggle (groups.go).
type routerSection struct {
	name    string
	rows    []routerRow
	heading bool
}

// onRouter reports whether the screen in front of the user is the router's. It
// is the serve view under the router engine — the toggle's third position — and
// the cache view is nobody's engine.
func (m model) onRouter() bool {
	return m.view == viewServe && m.prefs.Backend == config.BackendRouter
}

// routerSections is the list: every llama entry of the tree, in the entry list's
// own order and under its own headings, with the store's answer drawn in front
// of each and the running router's word drawn onto it. The ids the store holds
// that no profile answers for trail at the end.
//
// Only llama entries are here. An mlx entry is served by another program and can
// never be one of a llama-server router's models (docs/specs/SERVE.md), so
// listing it would offer a mark that cannot be set.
func (m model) routerSections() []routerSection {
	composed, refused := m.routerComposition()
	listed := entrySections(m.tree, m.prefs.Groups, config.BackendLlama)

	sections := make([]routerSection, 0, len(listed)+1)
	profiles := make(map[string]bool)
	for _, group := range listed {
		section := routerSection{name: group.name, heading: group.heading}
		for _, entry := range group.rows {
			row := m.routerRow(entry.id(), composed, refused)
			row.entry, row.broken = entry.entry, entry.broken
			profiles[row.id] = true
			section.rows = append(section.rows, row)
		}
		sections = append(sections, section)
	}

	// A held id with no profile of its own: the entry was renamed away, or an mlx
	// entry was included from the CLI. Inclusion is never auto-pruned
	// (docs/specs/SERVE.md), so the row is how that inclusion is seen and taken
	// out. Only a refused model can be one — a composed section names an entry
	// this list already drew.
	var ghosts routerSection
	for _, skipped := range m.router.models.Skipped {
		if profiles[skipped.ID] {
			continue
		}
		row := m.routerRow(skipped.ID, composed, refused)
		row.ghost = true
		ghosts.rows = append(ghosts.rows, row)
	}
	if len(ghosts.rows) > 0 {
		sections = append(sections, ghosts)
	}
	return sections
}

// routerRow is one id as the store and the running router answer for it.
func (m model) routerRow(id string, composed map[string]serve.ServedModel, refused map[string]string) routerRow {
	row := routerRow{id: id, running: m.router.live()}
	if model, served := composed[id]; served {
		row.included, row.model = true, model
	} else if reason, skipped := refused[id]; skipped {
		row.included, row.reason = true, reason
	}
	// The running router's word is drawn onto every row that has one, included or
	// not: a model excluded a moment ago is still being served by the router that
	// started with it, and hiding that would make the screen disagree with the
	// port (docs/specs/TUI.md).
	if row.running && m.router.children.Detail == "" {
		row.child, row.held = m.router.children.Child(id)
	}
	return row
}

// routerComposition is the store as the last composition read it: the section
// each included entry got, or the reason it got none. Presence is the inclusion
// itself — the composition walks the store's own keys, so every id it holds
// lands in exactly one of the two (docs/specs/SERVE.md).
func (m model) routerComposition() (map[string]serve.ServedModel, map[string]string) {
	composed := make(map[string]serve.ServedModel, len(m.router.models.Served))
	for _, model := range m.router.models.Served {
		composed[model.ID] = model
	}
	refused := make(map[string]string, len(m.router.models.Skipped))
	for _, skipped := range m.router.models.Skipped {
		refused[skipped.ID] = skipped.Reason
	}
	return composed, refused
}

// routerRows is the sections concatenated: the flat list the cursor indexes
// into, the same way the entry list's is (groups.go).
func (m model) routerRows() []routerRow {
	var rows []routerRow
	for _, section := range m.routerSections() {
		rows = append(rows, section.rows...)
	}
	return rows
}

// selectedRouterRow is the profile the cursor stands on.
func (m model) selectedRouterRow() (routerRow, bool) {
	rows := m.routerRows()
	if len(rows) == 0 {
		return routerRow{}, false
	}
	return rows[clamped(m.routerSelected, len(rows))], true
}

// routerLines is the list, drawn to fill exactly the rows it was given: the
// sections in order, each headed by its group's name where groups.go says the
// heading is drawn at all, exactly as the entry list draws them.
//
// It is a table like the entry list: the id column is as wide as the widest id,
// so the marks and the states line up under each other and a row is read across.
// The cursor never stops on a heading here either — the headings are managed
// from the entry list, and this list is the picker for a different question.
func (m model) routerLines(inner, capacity int) []string {
	sections := m.routerSections()
	rows := m.routerRows()
	if len(rows) == 0 {
		return sizeLines(m.emptyRouter(inner), capacity)
	}

	column := routerIDColumn(rows)
	selected := clamped(m.routerSelected, len(rows))
	lines := make([]string, 0, len(rows))
	cursor, at := 0, 0
	for _, section := range sections {
		if section.heading {
			lines = append(lines, headingLine(section.name, false, false, inner))
		}
		for _, row := range section.rows {
			if at == selected {
				cursor = len(lines)
			}
			lines = append(lines, m.routerRowLine(row, at == selected, inner, column))
			at++
		}
	}
	return sizeLines(window(lines, cursor, capacity), capacity)
}

// routerRowLine is one profile as a row: whether the router holds it, the id it
// is addressed by, the router's own word for what it is doing, and the model
// behind it.
//
// The state is the router's word and never cria's phase. Three of the six states
// a router publishes have no phase in cria's vocabulary — a model nobody has
// asked for is not starting and has not exited (docs/specs/SERVE.md) — so the
// word is what is shown, and the phase is only what colours it.
func (m model) routerRowLine(row routerRow, selected bool, inner, column int) string {
	paint := paintFor(selected)
	state, tone := m.routerRowState(row, paint)
	name := paint.name()
	if row.broken != nil || row.ghost {
		name = paint.broken()
	}
	pieces := []string{
		m.inclusionMark(row, paint),
		paint.cell(row.id, name, column),
		paint.cell(state, tone, routerStateColumn),
	}
	if tail := row.tail(paint); tail != "" {
		pieces = append(pieces, tail)
	}
	return paint.fill(paint.marker()+paint.join(pieces...), inner)
}

// tail is what a row carries after its state: the model the router serves it as,
// or — for the rows that name no model cria could read — why. An included
// profile is drawn as a fact and an excluded one as context: the list is read
// for what is in the router, and everything else on it is the alternatives.
func (r routerRow) tail(paint rowPaint) string {
	switch {
	case r.broken != nil:
		return paint.broken().Render(r.broken.Err.Error())
	case r.ghost:
		return paint.broken().Render(r.reason)
	case r.included && r.model.Repo != "":
		return paint.fact().Render(format.HubReference(r.model.Repo, r.model.Quant))
	case r.included:
		return paint.fact().Render(format.HubReference(r.entry.Repo, r.entry.Quant))
	}
	return paint.quiet().Render(format.HubReference(r.entry.Repo, r.entry.Quant))
}

// inclusionMark is the checkbox: does this host's router hold the profile. It is
// the one thing the row is about that the user can change from here, so it leads
// the row — and it is a mark of fixed width, because a toggle that moved the
// column beside it would be a row nobody can read (docs/specs/TUI.md).
//
// Lit is the green a cached dot takes in the entry list: there it means "starting
// this serves what is on disk", here "the next start serves it".
func (m model) inclusionMark(row routerRow, paint rowPaint) string {
	switch {
	case !m.router.composed:
		return paint.quiet().Render(unknownMark)
	case row.included:
		return paint.ready().Render(includedMark)
	}
	return paint.quiet().Render(excludedMark)
}

// routerStateColumn is how wide the state column is drawn: the widest word it
// can carry — cria's four and the longest of the router's own — so a model going
// from held to loading moves nothing beside it. The column is the same width
// whatever is in it: a row that reflows under a refresh is a row nobody can read
// (docs/specs/TUI.md).
const routerStateColumn = len(routerNotRunning)

// routerRowState is what a row says about one model, and the ink it says it in.
//
// A profile the router does not hold has an empty column rather than a word: the
// router has nothing to say about a model it was never given, the mark in front
// of the row is the whole answer, and the column keeps its width so the table
// lines up under it (the entry list's refused files leave their dot column blank
// for the same reason).
func (m model) routerRowState(row routerRow, paint rowPaint) (string, lipgloss.Style) {
	switch {
	case row.skipped():
		return routerNoSection, paint.alarm()
	case row.held && row.child.Phase != "":
		return row.child.State, paint.phase(row.child.Phase)
	case row.held:
		// A state cria has no phase for is a state cria does not colour: the word
		// is the router's and it is shown as written (docs/specs/SERVE.md).
		return row.child.State, paint.fact()
	case !row.included:
		return "", paint.quiet()
	case !row.running:
		return routerNotRunning, paint.quiet()
	case m.router.children.Detail != "":
		return routerUnasked, paint.quiet()
	}
	return routerNotServed, paint.notice()
}

// routerStateNote is the sentence behind a row's state word, where the word is
// short for something the pane has room to say. The states a router publishes
// speak for themselves; the ones cria writes in their place do not.
func (m model) routerStateNote(row routerRow) string {
	switch {
	case row.skipped(), !row.included, !row.running, row.held:
		return ""
	case m.router.children.Detail != "":
		return "cria could not ask the router what it holds: " + m.router.children.Detail
	}
	return "the running router does not hold it — it was included after that router started, and the preset is composed at every start; restart the router to serve it"
}

// routerIDColumn is how wide the id column has to be for every id to fit.
func routerIDColumn(rows []routerRow) int {
	column := 0
	for _, row := range rows {
		column = max(column, utf8.RuneCountInString(row.id))
	}
	return column
}

// emptyRouter is the list with nothing on it, which is a tree with no llama
// entry to put under the router. The tree is written by hand or by a coding
// agent, so the answer is where to write and what prints the schema
// (docs/cria.md, principle 5).
func (m model) emptyRouter(inner int) []string {
	switch {
	case m.tree == nil:
		return []string{quietStyle.Render("reading the config tree…")}
	case m.router.failure != nil:
		// What went wrong is on the line under the box, where every failed
		// reading is reported (tui.go, notes). The list says only that it is not
		// showing what it would have.
		return wrapped("cria could not read what the router holds", inner, alarmStyle)
	}
	lines := wrapped(fmt.Sprintf("no llama entries in %s: the router serves those",
		filepath.Join(m.tree.Root, "models")), inner, quietStyle)
	return append(lines, wrapped("write one there — `cria docs` prints the schema and a complete example",
		inner, quietStyle)...)
}

// routerDetail is the pane beside the list: the entry the selected profile was
// composed from, under the picks the *router* holds it with, and the preset
// section a start would write for it.
//
// The picks are the router's own and may differ from the ones a bare
// `cria start` would use for the same entry — that is the whole point of holding
// them per engine (docs/plans/engines/OVERVIEW.md, ruling 2) — so everything
// here is read through them. A profile the router does not hold has none, so it
// is read through the entry's config defaults, which is what including it would
// hold it under.
func (m model) routerDetail(inner, capacity int) []string {
	row, ok := m.selectedRouterRow()
	switch {
	case !ok:
		return sizeLines(nil, capacity)
	case row.broken != nil:
		// The file, the offending key and the one thing that clears it — the same
		// pane the entry list draws for it, because it is the same problem and the
		// same fix (docs/specs/CONFIG.md).
		return sizeLines(brokenDetail(*row.broken, inner), capacity)
	case row.skipped():
		return sizeLines(m.skippedDetail(row, inner), capacity)
	case !row.included:
		return sizeLines(m.excludedDetail(row, inner), capacity)
	}

	facts := m.routerModelFacts(row, inner)
	section := detailBlock("preset", presetLines(row.model.Section, inner))
	return sizeLines(sizeDetail(facts, section, capacity), capacity)
}

// skippedDetail is a model the preset could not carry: what happened, in the
// words whoever included it has to act on. There is no command line to anchor
// here — nothing was composed — so the reason is the whole pane. A ghost reaches
// it too: an inclusion whose profile is gone is a model with a reason and
// nothing else left to show.
func (m model) skippedDetail(row routerRow, inner int) []string {
	lines := detailField("model", row.id, inner, factStyle)
	lines = append(lines, detailBlock("skipped", wrapped(row.reason, inner-detailLabelWidth, alarmStyle))...)
	return lines
}

// excludedDetail is a profile the router does not hold: the entry as a start
// from this list would compose it — its config defaults, since an inclusion made
// here is made at those — and the state line saying it is not in the router.
//
// It is the same pane as an included model's, minus the preset section: nothing
// was composed for it, and a section drawn for a model the file does not carry
// would be a launch nobody is about to start.
func (m model) excludedDetail(row routerRow, inner int) []string {
	entry := row.entry
	// The entry's config defaults: the first option of each axis, which is what
	// an inclusion made here holds it under (toggleRouterInclusion).
	selection, _ := picks.Merge(entry, nil, nil)

	lines := detailField("file", entry.Path, inner, quietStyle)
	lines = append(lines, m.routerModelReference(entry, selection, inner)...)
	lines = append(lines, detailField("client", row.id, inner, factStyle)...)
	lines = append(lines, detailField("state", "not included", inner, factStyle)...)
	lines = append(lines, detailBlock("", wrapped(
		"the router does not hold this entry; including it holds it under these defaults, and the router composes its preset at every start — restart it to serve the change",
		inner-detailLabelWidth, quietStyle))...)
	return append(lines, choiceRows(entry, selection, inner)...)
}

// routerModelReference is the repo and quant a row's model is served as, the
// pair read through the selection it is held under: an option may replace either
// (docs/specs/CONFIG.md), so the declared pair would document a model nobody is
// about to serve.
func (m model) routerModelReference(entry config.Entry, selection config.Selection, inner int) []string {
	repo, quant := entry.Repo, entry.Quant
	if launched, resolved := launchedModel(entry, selection); resolved {
		repo, quant = launched.Repo, launched.Quant
	}
	lines := detailField("repo", repo, inner, factStyle)
	if quant != "" {
		lines = append(lines, detailField("quant", quant, inner, factStyle)...)
	}
	return lines
}

// routerModelFacts is the entry behind one included row, as the router holds it:
// where its file is, what it serves, what the running router says about it, and
// the axes it is held along with the router's own picks marked.
func (m model) routerModelFacts(row routerRow, inner int) []string {
	model := row.model
	var lines []string
	if row.entry.Path != "" {
		lines = append(lines, detailField("file", row.entry.Path, inner, quietStyle)...)
	}
	lines = append(lines, detailField("repo", model.Repo, inner, factStyle)...)
	if model.Quant != "" {
		lines = append(lines, detailField("quant", model.Quant, inner, factStyle)...)
	}
	lines = append(lines, detailField("client", row.id, inner, factStyle)...)

	// The same word the row carries, so the pane and the list can never disagree
	// about what the router is doing with this model.
	state, _ := m.routerRowState(row, paintFor(false))
	lines = append(lines, detailField("state", state, inner, factStyle)...)
	if note := m.routerStateNote(row); note != "" {
		lines = append(lines, detailBlock("", wrapped(note, inner-detailLabelWidth, quietStyle))...)
	}

	if len(row.entry.Choices) > 0 {
		lines = append(lines, choiceRows(row.entry, model.Selection, inner)...)
	}
	return lines
}

// presetLines is one model's section as the preset carries it, one line to a
// line. It is the file's own text: what the next start writes for this model,
// shown rather than described — the picking-and-seeing loop the entry view's
// command line is (docs/specs/TUI.md).
func presetLines(section string, inner int) []string {
	trimmed := strings.TrimRight(section, "\n")
	if trimmed == "" {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(trimmed, "\n") {
		lines = append(lines, fit(readyStyle.Render(line), inner))
	}
	return lines
}

// toggleRouterInclusion is the checkbox key: hold the profile under the cursor,
// or stop holding it. It is the gesture `cria router include` and
// `cria router exclude` are the CLI's spelling of, and it writes the same store
// (docs/specs/CLI.md) — the verb stays, because an inclusion is a thing a coding
// agent settles too.
//
// The write lands at the keypress. Inclusion is a decision "until I change it",
// exactly as a pick is, so leaving the view is never a discard (choicepick.go,
// the picks doctrine). An inclusion is made at the entry's config defaults —
// `{}` in the store — and the picker is what moves it off them.
//
// Excluding drops the entry's router picks with it: that is what exclude means,
// and it is what the CLI verb does with the same call.
//
// The store is read again here rather than carried on the frame, for the reason
// the picker reads it: `cria router include` writes it too, and a copy held since
// the last tick could overwrite an inclusion made in another terminal a second
// ago.
func (m model) toggleRouterInclusion() model {
	row, ok := m.selectedRouterRow()
	if !ok || !m.router.composed || (!row.included && !row.includable()) {
		return m
	}

	dir := serve.RouterStateDir(m.root)
	held, err := picks.LoadRouter(dir)
	if err != nil {
		m.alert = alert{text: err.Error(), bad: true}
		return m
	}
	if held.Holds(row.id) {
		held.Exclude(row.id)
	} else {
		held.Include(row.id, nil)
	}

	m.alert = alert{}
	if err := picks.SaveRouter(dir, held); err != nil {
		// The write failing is not the toggle failing to be meant: what was lost
		// is cria's memory of it, and the line under the box says so, exactly as a
		// failed pick write does (choicepick.go).
		m.alert = alert{text: err.Error(), bad: true}
		return m
	}
	return m.recomposeRouter()
}

// recomposeRouter re-reads what the store and the tree compose the router to
// serve, at the keypress that changed it. The mark a toggle just moved is that
// keypress's own answer, and leaving it to the next tick would put a checkbox on
// screen that lags the key by a beat (docs/specs/TUI.md — an action shows at the
// keypress). It is the same call the refresh makes, and it asks no server.
func (m model) recomposeRouter() model {
	if m.tree == nil {
		return m
	}
	models, err := m.host.servers.RouterModels(m.tree)
	if err != nil {
		m.router.composed, m.router.failure = false, err
		return m
	}
	m.router.models, m.router.composed = models, true
	return m.reselect(m.cursor())
}

// routerScreen draws the router's two panes, in the geometry the entry view uses
// so the toggle changes what is on screen and never where things are.
func (m model) routerScreen(width, rows int) string {
	var body string
	listWidth, listRows := width, rows
	switch {
	case width >= sideBySideWidth:
		listWidth = width / 2
		detailWidth := width - listWidth
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.routerPane(listWidth, rows),
			pane(paneTitle(routerDetailTitle), detailWidth, m.routerDetail(detailWidth-4, rows-2)))
	default:
		detailRows := rows / 2
		stackedRows := rows - detailRows
		if detailRows < minPaneRows || stackedRows < minPaneRows {
			body = m.routerPane(width, rows)
			break
		}
		listRows = stackedRows
		body = m.routerPane(width, listRows) + "\n" +
			pane(paneTitle(routerDetailTitle), width, m.routerDetail(width-4, detailRows-2))
	}

	if m.picker == nil {
		return body
	}
	return overlaid(body, m.pickerBox(listWidth, listRows), listWidth, listRows)
}

// routerDetailTitle names the pane beside the model list. It is "model" rather
// than "entry" because that is what the row is here: an entry the router holds
// or could hold, under the picks that go with it.
const routerDetailTitle = "model"

// routerPane is the list half.
func (m model) routerPane(width, rows int) string {
	return pane(m.serveTitle(), width, m.routerLines(width-4, rows-2))
}
