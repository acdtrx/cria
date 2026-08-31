package tui

import (
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/format"
	"cria/internal/serve"
)

// The router's screen: the models this host's router holds, what each of them is
// doing right now, and — for the one under the cursor — the entry it was
// composed from and the preset section a start would write for it.
//
// It is the third position of the engine toggle rather than a view of its own
// (docs/specs/TUI.md). The toggle chooses what the screen is about, and the
// router's answer is not a filtered entry list: no entry declares it, and which
// entries it serves is state of its own (docs/specs/CONFIG.md). Everything else
// about the screen is the entry view's — two panes, one cursor, the same detail
// pane hierarchy — because it is the same gesture: stand on a thing and read what
// serving it would come to.
//
// The rows are the store's, never the running router's. What the router holds
// right now is drawn onto them where it can be, so a model included since the
// last start reads as included and not yet served rather than disappearing from a
// list the operator just wrote.

// The words a row carries where the router's own word for a model would go.
const (
	// routerNoSection is a model the preset could not carry. It has no state to
	// report because it was never offered to the router at all.
	routerNoSection = "skipped"

	// routerNotRunning is every model's state while there is no router: the store
	// says what the next start would serve, and nothing is serving it yet.
	routerNotRunning = "not running"

	// routerNotServed is a model the running router does not list — included
	// after it started, since the preset is composed at every start and never
	// edited (docs/specs/SERVE.md). The pane says the rest.
	routerNotServed = "not served"

	// routerUnasked is a router that could not be asked what it holds.
	routerUnasked = "unasked"
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

	// Why the router could not be read — its store, its record, or the
	// observation of it. It is held and shown the way a failed observation of
	// the entries' servers is (tui.go, notes): an empty list would say "the
	// router holds nothing", which is a plausible-looking lie about something
	// cria could not ask (CODING-RULES §4).
	failure error
}

// live reports whether there is a router answering for the models in the list.
func (r routerState) live() bool { return r.found && r.status.Phase != serve.PhaseExited }

// routerRow is one model the router holds: the id it is held and addressed
// under, what composing the preset made of it, and what the running router says
// about it.
type routerRow struct {
	id      string
	model   serve.ServedModel // zero for a model the preset could not carry
	reason  string            // why it got no section; empty for a model that did
	child   serve.RouterChild // what the router says about it; zero when it says nothing
	held    bool              // the running router lists it
	running bool              // there is a router to have listed it
}

// skipped reports whether the preset left this model out.
func (r routerRow) skipped() bool { return r.reason != "" }

// onRouter reports whether the screen in front of the user is the router's. It
// is the serve view under the router engine — the toggle's third position — and
// the cache view is nobody's engine.
func (m model) onRouter() bool {
	return m.view == viewServe && m.prefs.Backend == config.BackendRouter
}

// routerRows is the list: every model the store holds, in its own order — sorted
// by id, which is the order the preset writes their sections in
// (internal/picks) — with the running router's word drawn onto each.
//
// Served and skipped models are one list rather than two. They are the same
// question asked of the same entries, and a skipped model kept in a footnote is
// the one a client 404s on hours later (docs/specs/SERVE.md).
func (m model) routerRows() []routerRow {
	held := m.router
	rows := make([]routerRow, 0, len(held.models.Served)+len(held.models.Skipped))
	for _, model := range held.models.Served {
		row := routerRow{id: model.ID, model: model, running: held.live()}
		if row.running && held.children.Detail == "" {
			row.child, row.held = held.children.Child(model.ID)
		}
		rows = append(rows, row)
	}
	for _, skipped := range held.models.Skipped {
		rows = append(rows, routerRow{id: skipped.ID, reason: skipped.Reason, running: held.live()})
	}

	// One order for both halves, since the two lists are composed separately and
	// the screen is read down the ids.
	slices.SortFunc(rows, func(a, b routerRow) int { return strings.Compare(a.id, b.id) })
	return rows
}

// selectedRouterRow is the model the cursor stands on.
func (m model) selectedRouterRow() (routerRow, bool) {
	rows := m.routerRows()
	if len(rows) == 0 {
		return routerRow{}, false
	}
	return rows[clamped(m.routerSelected, len(rows))], true
}

// routerLines is the model list, drawn to fill exactly the rows it was given.
//
// It is a table like the entry list: the id column is as wide as the widest id,
// so the states line up under each other and a row is read across.
func (m model) routerLines(inner, capacity int) []string {
	rows := m.routerRows()
	if len(rows) == 0 {
		return sizeLines(m.emptyRouter(inner), capacity)
	}

	column := routerIDColumn(rows)
	cursor := clamped(m.routerSelected, len(rows))
	lines := make([]string, 0, len(rows))
	for at, row := range rows {
		lines = append(lines, m.routerRowLine(row, at == cursor, inner, column))
	}
	return sizeLines(window(lines, cursor, capacity), capacity)
}

// routerRowLine is one model as a row: the id it is addressed by, the router's
// own word for what it is doing, and the model reference behind it.
//
// The state is the router's word and never cria's phase. Three of the six states
// a router publishes have no phase in cria's vocabulary — a model nobody has
// asked for is not starting and has not exited (docs/specs/SERVE.md) — so the
// word is what is shown, and the phase is only what colours it.
func (m model) routerRowLine(row routerRow, selected bool, inner, column int) string {
	paint := paintFor(selected)
	state, tone := m.routerRowState(row, paint)
	pieces := []string{
		paint.cell(row.id, paint.name(), column),
		paint.cell(state, tone, routerStateColumn),
	}
	if !row.skipped() {
		pieces = append(pieces, paint.fact().Render(format.HubReference(row.model.Repo, row.model.Quant)))
	}
	return paint.fill(paint.marker()+paint.join(pieces...), inner)
}

// routerStateColumn is how wide the state column is drawn: the widest word it
// can carry — cria's four and the longest of the router's own — so a model going
// from held to loading moves nothing beside it. The column is the same width
// whatever is in it: a row that reflows under a refresh is a row nobody can read
// (docs/specs/TUI.md).
const routerStateColumn = len(routerNotRunning)

// routerRowState is what a row says about one model, and the ink it says it in.
func (m model) routerRowState(row routerRow, paint rowPaint) (string, lipgloss.Style) {
	switch {
	case row.skipped():
		return routerNoSection, paint.alarm()
	case !row.running:
		return routerNotRunning, paint.quiet()
	case m.router.children.Detail != "":
		return routerUnasked, paint.quiet()
	case !row.held:
		return routerNotServed, paint.notice()
	case row.child.Phase != "":
		return row.child.State, paint.phase(row.child.Phase)
	}
	// A state cria has no phase for is a state cria does not colour: the word is
	// the router's and it is shown as written (docs/specs/SERVE.md).
	return row.child.State, paint.fact()
}

// routerStateNote is the sentence behind a row's state word, where the word is
// short for something the pane has room to say. The states a router publishes
// speak for themselves; the ones cria writes in their place do not.
func (m model) routerStateNote(row routerRow) string {
	switch {
	case row.skipped(), !row.running:
		return ""
	case m.router.children.Detail != "":
		return "cria could not ask the router what it holds: " + m.router.children.Detail
	case !row.held:
		return "the running router does not hold it — it was included after that router started, and the preset is composed at every start; restart the router to serve it"
	}
	return ""
}

// routerIDColumn is how wide the id column has to be for every id to fit.
func routerIDColumn(rows []routerRow) int {
	column := 0
	for _, row := range rows {
		column = max(column, utf8.RuneCountInString(row.id))
	}
	return column
}

// emptyRouter is the list with nothing in it, which is the ordinary state of a
// host that has not put anything under the router yet. It names the command that
// changes that: inclusion is settled by a verb rather than in this list
// (docs/specs/CLI.md).
func (m model) emptyRouter(inner int) []string {
	if m.router.failure != nil {
		// What went wrong is on the line under the box, where every failed
		// reading is reported (tui.go, notes). The list says only that it is not
		// showing what it would have.
		return wrapped("cria could not read what the router holds", inner, alarmStyle)
	}
	return wrapped("the router holds no models; `cria router include <id>` adds one", inner, quietStyle)
}

// routerDetail is the pane beside the list: the entry the selected model was
// composed from, under the picks the *router* holds it with, and the preset
// section a start would write for it.
//
// The picks are the router's own and may differ from the ones a bare
// `cria start` would use for the same entry — that is the whole point of holding
// them per engine (docs/plans/engines/OVERVIEW.md, ruling 2) — so everything
// here is read through them.
func (m model) routerDetail(inner, capacity int) []string {
	row, ok := m.selectedRouterRow()
	if !ok {
		return sizeLines(nil, capacity)
	}
	if row.skipped() {
		return sizeLines(m.skippedDetail(row, inner), capacity)
	}

	facts := m.routerModelFacts(row, inner)
	section := detailBlock("preset", presetLines(row.model.Section, inner))
	return sizeLines(sizeDetail(facts, section, capacity), capacity)
}

// skippedDetail is a model the preset could not carry: what happened, in the
// words whoever included it has to act on. There is no command line to anchor
// here — nothing was composed — so the reason is the whole pane.
func (m model) skippedDetail(row routerRow, inner int) []string {
	lines := detailField("model", row.id, inner, factStyle)
	lines = append(lines, detailBlock("skipped", wrapped(row.reason, inner-detailLabelWidth, alarmStyle))...)
	return lines
}

// routerModelFacts is the entry behind one row, as the router holds it: where its
// file is, what it serves, what the running router says about it, and the axes it
// is held along with the router's own picks marked.
func (m model) routerModelFacts(row routerRow, inner int) []string {
	model := row.model
	var lines []string
	if entry, found := m.entryNamed(row.id); found {
		lines = append(lines, detailField("file", entry.Path, inner, quietStyle)...)
	}
	lines = append(lines, detailField("repo", model.Repo, inner, factStyle)...)
	if model.Quant != "" {
		lines = append(lines, detailField("quant", model.Quant, inner, factStyle)...)
	}
	lines = append(lines, detailField("client", row.id, inner, factStyle)...)

	state, _ := m.routerRowState(row, paintFor(false))
	lines = append(lines, detailField("state", state, inner, factStyle)...)
	if note := m.routerStateNote(row); note != "" {
		lines = append(lines, detailBlock("", wrapped(note, inner-detailLabelWidth, quietStyle))...)
	}

	if entry, found := m.entryNamed(row.id); found && len(entry.Choices) > 0 {
		lines = append(lines, choiceRows(entry, model.Selection, inner)...)
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
// than "entry" because that is what the row is here: an entry the router holds,
// under the router's own picks for it.
const routerDetailTitle = "model"

// routerPane is the list half.
func (m model) routerPane(width, rows int) string {
	return pane(m.serveTitle(), width, m.routerLines(width-4, rows-2))
}
