package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/format"
	"cria/internal/serve"
)

// The serve view is the picker: the active backend's entries on the left, the
// highlighted one's whole truth on the right. Picking a row picks model, quant
// and params in one gesture, and the detail pane carries what the row cannot —
// which is why entry names never have to be memorable (docs/specs/TUI.md).
const (
	// detailTitle names the pane beside the list. It is the entry, not a
	// summary of it: everything the file sets is in there, ending with the exact
	// command line a start would run (docs/specs/CONFIG.md).
	detailTitle = "entry"

	// sideBySideWidth is the width below which the detail pane stops fitting
	// beside the list and goes under it. Half of it has to hold a repo id and a
	// quant tag without truncating them into a guess.
	sideBySideWidth = 96

	// detailLabelWidth is the column the detail pane's labels occupy, values
	// starting after it and wrapping into the same indent.
	detailLabelWidth = 9

	// titleSeparator holds a pane's name apart from what qualifies it — the
	// active backend, the entry a log belongs to.
	titleSeparator = " · "
)

// The marks in front of a row: whether starting it would download anything
// (docs/specs/TUI.md). Unknown is its own mark — before the first cache walk
// answers, or after one fails, "not cached" would be a claim cria has not
// earned (CODING-RULES §4).
const (
	cachedMark  = "●"
	absentMark  = "○"
	unknownMark = "·"

	cursorMark  = "▸ "
	nothingHere = "  "
)

// row is one line of the entry list: an entry cria could start, or an entry file
// it had to refuse. A refused file has no entry to show — what would name its
// keys is exactly what failed to parse — so it carries its BrokenEntry instead,
// and that pointer is what tells the two apart.
type row struct {
	entry  config.Entry
	broken *config.BrokenEntry
}

// rows is the list the serve view draws: the active backend's entries in the
// order their groups put them in, the ungrouped ones after them, then the entry
// files cria refused. groups.go lays that order out, and it is the only one —
// the cursor indexes into this sequence and the pane draws it with the headings
// woven in.
//
// One backend at a time, never a mixed list (docs/specs/TUI.md). A refused file
// appears under both backends, because the key that would sort it under one is
// exactly the key that could not be read; it stays visible either way, since a
// broken file disables only itself and a file nobody can see is one nobody fixes
// (docs/specs/CONFIG.md).
func (m model) rows() []row {
	return entryRows(m.tree, m.prefs.Groups, m.prefs.Backend)
}

// selectedRow is what the cursor is on, if the list has anything on it.
func (m model) selectedRow() (row, bool) {
	rows := m.rows()
	if m.selected < 0 || m.selected >= len(rows) {
		return row{}, false
	}
	return rows[m.selected], true
}

// entryNamed finds one entry anywhere in the tree, whatever backend it declares.
// The server keys act on what the status box shows, and that entry belongs to
// whichever backend started it — not to the tab the user is standing on
// (docs/specs/TUI.md).
func (m model) entryNamed(id string) (config.Entry, bool) {
	if m.tree == nil {
		return config.Entry{}, false
	}
	for _, entry := range m.tree.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return config.Entry{}, false
}

// serveScreen draws the list and the detail pane into the rows the frame left
// them: side by side where there is width for both, stacked where there is not.
// Neither is dropped — a picker with no detail is a list of names cria has
// already said are not worth memorising.
func (m model) serveScreen(width, rows int) string {
	title := m.serveTitle()

	if width >= sideBySideWidth {
		listWidth := width / 2
		detailWidth := width - listWidth
		return lipgloss.JoinHorizontal(lipgloss.Top,
			pane(title, listWidth, m.listLines(listWidth-4, rows-2)),
			pane(paneTitle(detailTitle), detailWidth, m.detailLines(detailWidth-4, rows-2)))
	}

	detailRows := rows / 2
	listRows := rows - detailRows
	if detailRows < minPaneRows || listRows < minPaneRows {
		// Too short to stack: the list is the half that can be acted on, so it
		// is the half that stays.
		return pane(title, width, m.listLines(width-4, rows-2))
	}
	return pane(title, width, m.listLines(width-4, listRows-2)) + "\n" +
		pane(paneTitle(detailTitle), width, m.detailLines(width-4, detailRows-2))
}

// serveTitle names the list and says whose it is: the active backend in its own
// colour, which is where the backend toggle shows up. The toggle says nothing
// else — a line under the status box would report a change the title already
// carries, and would still be sitting there three keypresses later
// (docs/specs/TUI.md).
func (m model) serveTitle() string {
	return paneTitle("serve"+titleSeparator) + backendTone(m.prefs.Backend).Render(string(m.prefs.Backend))
}

// listLines is the entry list, drawn to fill exactly the rows it was given: the
// sections in order, each headed by its group's name where groups.go says the
// heading is drawn at all.
//
// The list is a table: the id column is as wide as the widest id on it, so the
// models line up under each other and a row is read across rather than parsed.
// The column is the whole pane's rather than each section's — ids that line up
// down the screen are what makes this one table instead of several.
//
// A heading is a drawn line and never a row, so the selection stays an index
// over entries alone. What that costs is here: the window is taken over the
// drawn lines, around the line the selected entry landed on, which is what keeps
// the cursor on screen while the headings above it take capacity of their own.
//
// A mode standing on the headings is the one thing that changes any of this: it
// makes them what the cursor is on, so every group gets one whether this backend
// has anything under it or not, and the window follows that cursor instead — the
// entry cursor stays where it was, drawn as it was. The move adds the two
// headings that are answers rather than groups: the tail's own line when it can
// take the entry, and `new group…` closing the list (grouppick.go,
// managegroups.go).
func (m model) listLines(inner, capacity int) []string {
	rows := m.rows()
	if len(rows) == 0 {
		return sizeLines(m.emptyList(), capacity)
	}

	heading := m.headingCursor()
	sections := entrySections(m.tree, m.prefs.Groups, m.prefs.Backend)
	column := idColumn(rows)
	lines := make([]string, 0, len(rows))
	cursor, at := 0, 0
	for i, listed := range sections {
		target := sectionTarget(i, len(sections))
		if listed.heading || heading.draws(target) {
			if heading.on(target) {
				cursor = len(lines)
			}
			lines = append(lines, headingLine(listed.name, heading.on(target), heading.held, inner))
		}
		for _, drawn := range listed.rows {
			if at == m.selected && !heading.up {
				cursor = len(lines)
			}
			lines = append(lines, m.rowLine(drawn, at == m.selected, inner, column))
			at++
		}
	}
	if heading.newGroup {
		if heading.on(moveToNewGroup) {
			cursor = len(lines)
		}
		lines = append(lines, headingLine(newGroupLabel, heading.on(moveToNewGroup), heading.held, inner))
	}
	return sizeLines(window(lines, cursor, capacity), capacity)
}

// ungroupedHeading names the tail of the list, the entries no group holds. The
// tail is a section like any other once there are groups above it to be told
// apart from, so it is named rather than left to run on from the last group
// (docs/specs/TUI.md).
const ungroupedHeading = "ungrouped"

// headingLine is one section's name over the rows filed under it: the name, in
// the heading's own muted tone, and nothing else. No count, no rule — the rows
// under it are the count, and the heading sits at the pane's left edge while
// every row is indented past the column the cursor's marker keeps, which is what
// makes the two read apart without spending a glyph on it.
//
// The heading a mode is standing on is drawn on the cursor's own band, spanning
// the pane the way a picked row does — and without the marker, because a heading
// pointed at is still not a row the list stops on (grouppick.go). A heading the
// manage mode holds in the air rides the carry band instead, teal on teal, so
// "in your hand" is never read as "selected" (styles.go).
func headingLine(name string, picked, held bool, inner int) string {
	if name == "" {
		name = ungroupedHeading
	}
	if !picked {
		return headingStyle.Render(name)
	}
	paint := rowPaint{cursor: true, held: held}
	return paint.fill(paint.heading().Render(name), inner)
}

// idColumn is how wide the id column has to be for every id on the list to fit
// in it. A refused file counts too: its id is a column of the same table.
func idColumn(rows []row) int {
	column := 0
	for _, listed := range rows {
		column = max(column, lipgloss.Width(listed.id()))
	}
	return column
}

// id is what the list calls a row, whether cria could read the file or not.
func (r row) id() string {
	if r.broken != nil {
		return r.broken.ID
	}
	return r.entry.ID
}

// rowLine is one entry as the list reads it: whether it is on disk, its id, the
// model it serves, and the port when that port is the entry's own choice rather
// than the tree's default (docs/specs/CONFIG.md). The row the cursor is on is
// drawn on the band, marker included.
func (m model) rowLine(listed row, selected bool, inner, column int) string {
	paint := paintFor(selected)
	if listed.broken != nil {
		// A refused file has nothing to say about the cache, so its dot column
		// is blank — the table still lines up under it.
		return paint.fill(paint.marker()+paint.join(
			paint.pad(" "),
			paint.cell(listed.broken.ID, paint.broken(), column),
			paint.broken().Render(listed.broken.Err.Error())), inner)
	}

	facts := []string{
		m.presenceMark(listed.entry, paint),
		paint.cell(listed.entry.ID, paint.name(), column),
		paint.quiet().Render(format.HubReference(listed.entry.Repo, listed.entry.Quant)),
	}
	if listed.entry.Port != m.tree.Settings.DefaultPort {
		facts = append(facts, paint.quiet().Render(":"+strconv.Itoa(listed.entry.Port)))
	}
	return paint.fill(paint.marker()+paint.join(facts...), inner)
}

// presenceMark is the cached dot: starting this entry serves what is already on
// disk, or fetches it first (docs/specs/TUI.md). The answer is the cache walk's
// own — the same read the cache view lists (docs/specs/CACHE.md), asked one
// entry at a time.
func (m model) presenceMark(entry config.Entry, paint rowPaint) string {
	switch {
	case m.cache == nil:
		return paint.quiet().Render(unknownMark)
	case m.cache.Presence(entry).Cached:
		// Green, the colour a server that answers is drawn in: this one would
		// serve now. A dot that differed from the absent one only by glyph and
		// by a shade of grey read as one dot that never changed.
		return paint.ready().Render(cachedMark)
	}
	return paint.quiet().Render(absentMark)
}

// emptyList is a backend with nothing declared for it. The tree is written by
// hand or by a coding agent, so the answer is where to write and what prints the
// schema (docs/cria.md, principle 5).
func (m model) emptyList() []string {
	if m.tree == nil {
		return []string{quietStyle.Render("reading the config tree…")}
	}
	return []string{
		quietStyle.Render(fmt.Sprintf("no %s entries in %s", m.prefs.Backend, filepath.Join(m.tree.Root, "models"))),
		quietStyle.Render("write one there — `cria docs` prints the schema and a complete example"),
	}
}

// detailLines is the highlighted row in full: every key the entry sets with the
// value cria resolved for it, the args as the file wrote them, and the exact
// command line a start would run. That line is the entry's documentation
// (docs/specs/CONFIG.md), and it is composed by the same call Start spawns with,
// so what is read here is what would run.
func (m model) detailLines(inner, capacity int) []string {
	selected, ok := m.selectedRow()
	switch {
	case !ok:
		return sizeLines(nil, capacity)
	case selected.broken != nil:
		return sizeLines(brokenDetail(*selected.broken, inner), capacity)
	}
	return sizeLines(m.entryDetail(selected.entry, inner), capacity)
}

// entryDetail is one entry's contents.
func (m model) entryDetail(entry config.Entry, inner int) []string {
	var lines []string
	add := func(label, value string, style lipgloss.Style) {
		lines = append(lines, detailField(label, value, inner, style)...)
	}

	// Every value is body text: the labels beside them carry the structure, in
	// their own colour, so nothing here has to be dimmed to stay out of the way.
	add("name", entry.Name, factStyle)
	add("file", entry.Path, factStyle)
	add("backend", string(entry.Backend), backendTone(entry.Backend))
	add("repo", entry.Repo, factStyle)
	if entry.Quant != "" {
		add("quant", entry.Quant, factStyle)
	}
	add("port", strconv.Itoa(entry.Port), factStyle)
	add("host", entry.Host, factStyle)
	// Args go one flag to a line, verbatim: a file's args list is read to check
	// what this entry sets, and a single wrapped string hides where one flag
	// ends and the next begins.
	for i, arg := range argRows(entry.Args) {
		label := "args"
		if i > 0 {
			label = ""
		}
		add(label, arg, factStyle)
	}
	add("cached", m.cachedWord(entry), factStyle)

	command, refused := m.composedCommand(entry)
	style := factStyle
	if refused {
		style = alarmStyle
	}
	add("command", command, style)
	return lines
}

// cachedWord is the dot spelled out, for the pane that has room for words.
func (m model) cachedWord(entry config.Entry) string {
	switch {
	case m.cache == nil:
		return "not read yet"
	case m.cache.Presence(entry).Cached:
		return "yes — starting it serves what is on disk"
	}
	return "no — starting it downloads first"
}

// composedCommand is the argv a start would run, or why cria cannot spell it.
// A missing or unfit tool is the honest answer: the program cria would exec is
// half of that line, and the tool check is what names it (docs/specs/TOOLS.md).
func (m model) composedCommand(entry config.Entry) (string, bool) {
	if !m.reported {
		return "reading the host's tools…", false
	}
	// The picks a start off this frame would use, so the line shown is the line
	// spawned rather than one composed from another combination.
	launch, err := config.Resolve(entry, m.picks(entry))
	if err != nil {
		return err.Error(), true
	}
	command, err := serve.ComposedCommand(entry, launch, m.report)
	if err != nil {
		return err.Error(), true
	}
	return strings.Join(command, " "), false
}

// argRows groups an args list the way it was written: each flag with the values
// that follow it, so `--ctx-size 16384` is one line and `--jinja` is another.
// Nothing is reformatted — the tokens are the file's own, in the file's order.
func argRows(args []string) []string {
	var rows []string
	for _, arg := range args {
		if len(rows) == 0 || strings.HasPrefix(arg, "-") {
			rows = append(rows, arg)
			continue
		}
		rows[len(rows)-1] += " " + arg
	}
	return rows
}

// brokenDetail is an entry file cria refused: which file, which key, and the one
// thing that clears it (docs/specs/CONFIG.md).
func brokenDetail(broken config.BrokenEntry, inner int) []string {
	lines := detailField("file", broken.Path, inner, factStyle)
	lines = append(lines, detailField("error", broken.Err.Error(), inner, brokenStyle)...)
	return append(lines, detailField("fix", "edit that file; `cria docs` prints the schema", inner, factStyle)...)
}

// detailField is one label and its value, wrapped into the label's indent so a
// long path or a whole command line is read rather than truncated.
func detailField(label, value string, inner int, style lipgloss.Style) []string {
	room := max(inner-detailLabelWidth, 1)

	var lines []string
	for i, chunk := range wrap(value, room) {
		head := labelStyle.Render(fit(label, detailLabelWidth))
		if i > 0 {
			head = strings.Repeat(" ", detailLabelWidth)
		}
		lines = append(lines, head+style.Render(chunk))
	}
	return lines
}

// wrapped is one message drawn across as many lines as it needs — for the
// screens with no label column, where a truncated sentence would lose the half
// that says what to do about it.
func wrapped(message string, width int, style lipgloss.Style) []string {
	var lines []string
	for _, chunk := range wrap(message, width) {
		lines = append(lines, style.Render(chunk))
	}
	return lines
}

// wrap breaks one value into lines of at most width cells, on spaces where there
// are any and mid-word where a single word is longer than the line.
func wrap(value string, width int) []string {
	if width < 1 {
		return []string{value}
	}

	var lines []string
	line := ""
	flush := func() { lines, line = append(lines, line), "" }

	for _, word := range strings.Fields(value) {
		if line != "" && lipgloss.Width(line)+1+lipgloss.Width(word) > width {
			flush()
		}
		if line != "" {
			line += " "
		}
		// A word with no room left to break on — a path, a repo id — is cut
		// rather than left to be truncated out of the pane.
		for lipgloss.Width(line)+lipgloss.Width(word) > width {
			head, tail := split(word, width-lipgloss.Width(line))
			line, word = line+head, tail
			flush()
		}
		line += word
	}
	if line != "" || len(lines) == 0 {
		lines = append(lines, line)
	}
	return lines
}

// split cuts a word at a cell count, answering the part that fits and the rest.
// Nothing fits in no room at all, which is how wrap knows to break the line
// first.
func split(word string, room int) (string, string) {
	if room < 1 {
		return "", word
	}
	runes := []rune(word)
	if room > len(runes) {
		room = len(runes)
	}
	return string(runes[:room]), string(runes[room:])
}

// window is the slice of a list a pane this tall can show, kept around the
// cursor so moving it never scrolls the selection off screen.
func window(lines []string, selected, capacity int) []string {
	if capacity < 1 || len(lines) <= capacity {
		return lines
	}
	start := min(max(selected-capacity/2, 0), len(lines)-capacity)
	return lines[start : start+capacity]
}

// sizeLines makes a pane's contents exactly as tall as the pane: the tail is
// dropped when there is more, and blank lines fill the rest when there is less,
// so the screen fills the terminal rather than leaving the keybar floating.
func sizeLines(lines []string, capacity int) []string {
	if capacity < 1 {
		return nil
	}
	if len(lines) > capacity {
		return lines[:capacity]
	}
	for len(lines) < capacity {
		lines = append(lines, "")
	}
	return lines
}
