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

// rows is the list the serve view draws: the active backend's entries, then the
// entry files cria refused.
//
// One backend at a time, never a mixed list (docs/specs/TUI.md). A refused file
// appears under both backends, because the key that would sort it under one is
// exactly the key that could not be read; it stays visible either way, since a
// broken file disables only itself and a file nobody can see is one nobody fixes
// (docs/specs/CONFIG.md).
func (m model) rows() []row {
	if m.tree == nil {
		return nil
	}

	var rows []row
	for _, entry := range m.tree.Entries {
		if entry.Backend != m.prefs.Backend {
			continue
		}
		rows = append(rows, row{entry: entry})
	}
	for i := range m.tree.Broken {
		rows = append(rows, row{broken: &m.tree.Broken[i]})
	}
	return rows
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
	title := "serve · " + string(m.prefs.Backend)

	if width >= sideBySideWidth {
		listWidth := width / 2
		detailWidth := width - listWidth
		return lipgloss.JoinHorizontal(lipgloss.Top,
			pane(title, listWidth, m.listLines(rows-2)),
			pane(detailTitle, detailWidth, m.detailLines(detailWidth-4, rows-2)))
	}

	detailRows := rows / 2
	listRows := rows - detailRows
	if detailRows < minPaneRows || listRows < minPaneRows {
		// Too short to stack: the list is the half that can be acted on, so it
		// is the half that stays.
		return pane(title, width, m.listLines(rows-2))
	}
	return pane(title, width, m.listLines(listRows-2)) + "\n" +
		pane(detailTitle, width, m.detailLines(width-4, detailRows-2))
}

// listLines is the entry list, drawn to fill exactly the rows it was given.
func (m model) listLines(capacity int) []string {
	rows := m.rows()
	if len(rows) == 0 {
		return sizeLines(m.emptyList(), capacity)
	}

	lines := make([]string, 0, len(rows))
	for i, listed := range rows {
		lines = append(lines, m.rowLine(listed, i == m.selected))
	}
	return sizeLines(window(lines, m.selected, capacity), capacity)
}

// rowLine is one entry as the list reads it: whether it is on disk, its id, the
// model it serves, and the port when that port is the entry's own choice rather
// than the tree's default (docs/specs/CONFIG.md).
func (m model) rowLine(listed row, selected bool) string {
	cursor := nothingHere
	if selected {
		cursor = keyStyle.Render(cursorMark)
	}
	if listed.broken != nil {
		return cursor + brokenStyle.Render(strings.Join(
			[]string{listed.broken.ID, listed.broken.Err.Error()}, factSeparator))
	}

	name := factStyle
	if selected {
		name = selectedStyle
	}
	facts := []string{
		m.presenceMark(listed.entry),
		name.Render(listed.entry.ID),
		quietStyle.Render(format.HubReference(listed.entry.Repo, listed.entry.Quant)),
	}
	if listed.entry.Port != m.tree.Settings.DefaultPort {
		facts = append(facts, quietStyle.Render(":"+strconv.Itoa(listed.entry.Port)))
	}
	return cursor + strings.Join(facts, factSeparator)
}

// presenceMark is the cached dot: starting this entry serves what is already on
// disk, or fetches it first (docs/specs/TUI.md). The answer is the cache walk's
// own — the same read the cache view lists (docs/specs/CACHE.md), asked one
// entry at a time.
func (m model) presenceMark(entry config.Entry) string {
	switch {
	case m.cache == nil:
		return quietStyle.Render(unknownMark)
	case m.cache.Presence(entry).Cached:
		return factStyle.Render(cachedMark)
	}
	return quietStyle.Render(absentMark)
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

	add("name", entry.Name, factStyle)
	add("file", entry.Path, quietStyle)
	add("backend", string(entry.Backend), factStyle)
	add("repo", entry.Repo, factStyle)
	if entry.Quant != "" {
		add("quant", entry.Quant, factStyle)
	}
	add("port", strconv.Itoa(entry.Port), factStyle)
	add("host", entry.Host, factStyle)
	if len(entry.Args) > 0 {
		add("args", strings.Join(entry.Args, " "), factStyle)
	}
	add("cached", m.cachedWord(entry), quietStyle)

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
	command, err := serve.ComposedCommand(entry, m.report)
	if err != nil {
		return err.Error(), true
	}
	return strings.Join(command, " "), false
}

// brokenDetail is an entry file cria refused: which file, which key, and the one
// thing that clears it (docs/specs/CONFIG.md).
func brokenDetail(broken config.BrokenEntry, inner int) []string {
	lines := detailField("file", broken.Path, inner, quietStyle)
	lines = append(lines, detailField("error", broken.Err.Error(), inner, brokenStyle)...)
	return append(lines, detailField("fix", "edit that file; `cria docs` prints the schema", inner, quietStyle)...)
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
