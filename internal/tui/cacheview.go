package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"cria/internal/format"
	"cria/internal/hubcache"
	"cria/internal/serve"
)

// The cache view is one list of everything the hub cache holds: GGUF repos with
// their quants nested under them, MLX and other repos whole, and a row for the
// unfinished downloads a repo is sitting on — every name spelled exactly as
// Hugging Face spells it (docs/specs/CACHE.md). The pane beside it says what the
// filesystem already knows about the highlighted row, and the delete key acts on
// that row and nothing else (surgery.go).
const (
	// cacheTitle names the list pane, and cacheDetailTitle the pane beside it,
	// which holds facts about one cached thing rather than a summary of the
	// cache — the totals are in the list's own header, where the "where did my
	// disk go" question is asked.
	cacheTitle       = "cache"
	cacheDetailTitle = "details"

	// itemIndent is how far a quant sits under the repo it belongs to. The tree
	// renders open: v1 has no expand, because a cache of a dozen repos is read
	// in one screenful and a collapsed row hides exactly the thing being looked
	// for (CLAUDE.md, Scope).
	itemIndent = "  "

	// sizeGap is the space between a row's facts and its size column.
	sizeGap = "  "

	// detailSeparator holds two facts apart inside one detail value — a file
	// name and its size, a partial and its age. The pane wraps its values on
	// whitespace, which is why the separator is a glyph rather than the spaces
	// the list's own columns are held apart by.
	detailSeparator = " · "
)

// partialMark flags the rows that are reclaimable rather than served: unfinished
// downloads, and the quants an interrupted one left half-written.
const partialMark = "⚠"

// cacheRowKind is what one row of the cache list stands for — the units
// docs/specs/CACHE.md makes selectable, which are the units it makes deletable.
type cacheRowKind int

const (
	repoRow     cacheRowKind = iota // a repository: a GGUF repo's header, or an MLX/other repo whole
	itemRow                         // one quantization of a GGUF repo, its shards folded in
	partialsRow                     // one repository's unfinished downloads
)

// cacheRow is one line of the list, pointing into the walk it was built from.
type cacheRow struct {
	kind cacheRowKind
	repo *hubcache.Repo
	item *hubcache.Item // the quantization, on an itemRow
}

// cacheRows is the list the cache view draws: every repository the walk found,
// in its order, with a GGUF repo's quants under it and a partials row wherever
// there are bytes to reclaim.
func (m model) cacheRows() []cacheRow {
	if m.cache == nil {
		return nil
	}

	var rows []cacheRow
	for i := range m.cache.Repos {
		repo := &m.cache.Repos[i]
		rows = append(rows, cacheRow{kind: repoRow, repo: repo})
		for j := range repo.Items {
			rows = append(rows, cacheRow{kind: itemRow, repo: repo, item: &repo.Items[j]})
		}
		if len(repo.Partials) > 0 {
			rows = append(rows, cacheRow{kind: partialsRow, repo: repo})
		}
	}
	return rows
}

// selectedCacheRow is what the cache cursor is on, if the list has anything on
// it.
func (m model) selectedCacheRow() (cacheRow, bool) {
	rows := m.cacheRows()
	if m.cacheSelected < 0 || m.cacheSelected >= len(rows) {
		return cacheRow{}, false
	}
	return rows[m.cacheSelected], true
}

// cacheScreen draws the list and its details pane into the rows the frame left
// them, the way the serve view does: side by side where there is width for both,
// stacked where there is not. The list is the half that can be acted on, so it
// is the half that stays when there is room for only one.
func (m model) cacheScreen(width, rows int) string {
	if width >= sideBySideWidth {
		listWidth := width / 2
		detailWidth := width - listWidth
		return lipgloss.JoinHorizontal(lipgloss.Top,
			pane(paneTitle(cacheTitle), listWidth, m.cacheListLines(listWidth-4, rows-2)),
			pane(paneTitle(cacheDetailTitle), detailWidth, m.cacheDetailLines(detailWidth-4, rows-2)))
	}

	detailRows := rows / 2
	listRows := rows - detailRows
	if detailRows < minPaneRows || listRows < minPaneRows {
		return pane(paneTitle(cacheTitle), width, m.cacheListLines(width-4, rows-2))
	}
	return pane(paneTitle(cacheTitle), width, m.cacheListLines(width-4, listRows-2)) + "\n" +
		pane(paneTitle(cacheDetailTitle), width, m.cacheDetailLines(width-4, detailRows-2))
}

// cacheListLines is the whole list: the header the cache is read against, then
// the rows, windowed around the cursor.
func (m model) cacheListLines(inner, capacity int) []string {
	switch {
	case m.cache == nil:
		return sizeLines([]string{quietStyle.Render("reading the hub cache…")}, capacity)
	case len(m.cache.Repos) == 0:
		return sizeLines([]string{
			quietStyle.Render("the hub cache holds nothing yet"),
			quietStyle.Render(m.cache.Root),
		}, capacity)
	}

	header := m.cacheHeaderLines()
	rows := m.cacheRows()
	column := sizeColumn(rows)

	lines := make([]string, 0, len(rows))
	for i, listed := range rows {
		lines = append(lines, m.cacheRowLine(listed, i == m.cacheSelected, inner, column))
	}
	return sizeLines(append(header, window(lines, m.cacheSelected, capacity-len(header))...), capacity)
}

// cacheHeaderLines is what the whole cache comes to: where it is, and the
// walker's totals. The total is the number `du` would report for that directory,
// which is what makes it worth trusting for "where did my disk go"
// (docs/specs/CACHE.md).
func (m model) cacheHeaderLines() []string {
	totals := []string{
		sizeStyle.Render(format.Bytes(m.cache.Bytes)),
		quietStyle.Render(fmt.Sprintf("%d repos", len(m.cache.Repos))),
	}
	if m.cache.PartialBytes > 0 {
		totals = append(totals, noticeStyle.Render(fmt.Sprintf("%s %s unfinished", partialMark, format.Bytes(m.cache.PartialBytes))))
	}
	return []string{
		quietStyle.Render(m.cache.Root),
		strings.Join(totals, factSeparator),
	}
}

// cacheRowLine is one row: what it is on the left, what it occupies on the
// right. Every size sits in the same column, so the list reads as the table the
// question "what is taking the disk" is answered from.
func (m model) cacheRowLine(listed cacheRow, selected bool, inner, column int) string {
	paint := paintFor(selected)
	return sizedLine(paint.marker()+rowFacts(listed, paint),
		paint.size().Render(format.Bytes(rowBytes(listed))), inner, column, paint)
}

// rowFacts is what one row says about itself, in the provider's own spelling: a
// repository by its Hub id and what cria can do with it, a quant by its tag, a
// partials row by what it is holding back (docs/specs/CACHE.md).
func rowFacts(listed cacheRow, paint rowPaint) string {
	switch listed.kind {
	case itemRow:
		facts := []string{paint.pad(itemIndent) + paint.name().Render(listed.item.Label)}
		if !listed.item.Complete {
			facts = append(facts, paint.notice().Render(partialMark+" incomplete"))
		}
		return paint.join(facts...)
	case partialsRow:
		return paint.pad(itemIndent) + paint.notice().Render(partialMark+" "+unfinishedWords(len(listed.repo.Partials)))
	}

	facts := []string{paint.name().Render(listed.repo.ID), paint.quiet().Render(listed.repo.Kind.String())}
	// A dataset or a space is not a model cria could serve, and the row says so
	// rather than letting "other" stand for both that and an unrecognised model.
	if listed.repo.Type != hubcache.RepoModel {
		facts = append(facts, paint.quiet().Render(string(listed.repo.Type)))
	}
	return paint.join(facts...)
}

// unfinishedWords counts what a repository is holding back, in English: one
// abandoned download is the common case, and "1 unfinished downloads" reads as
// a bug in the thing about to delete files.
func unfinishedWords(count int) string {
	if count == 1 {
		return "1 unfinished download"
	}
	return fmt.Sprintf("%d unfinished downloads", count)
}

// rowBytes is what one row occupies on disk. A repo's own number is the whole
// directory, its quants' are their blobs counted once each, so items need not
// sum to their repo — true bytes win over tidy arithmetic (docs/specs/CACHE.md).
func rowBytes(listed cacheRow) int64 {
	switch listed.kind {
	case itemRow:
		return listed.item.Bytes
	case partialsRow:
		return listed.repo.PartialBytes
	}
	return listed.repo.Bytes
}

// sizeColumn is how wide the size column has to be for every row's size to sit
// in it, right-aligned.
func sizeColumn(rows []cacheRow) int {
	column := 0
	for _, listed := range rows {
		column = max(column, lipgloss.Width(format.Bytes(rowBytes(listed))))
	}
	return column
}

// sizedLine puts a row's facts on the left and its size right-aligned on the
// right, the two filling exactly the pane's inner width.
func sizedLine(facts, size string, inner, column int, paint rowPaint) string {
	room := max(inner-column-lipgloss.Width(sizeGap), 1)
	pad := max(column-lipgloss.Width(size), 0)
	return paint.fill(facts, room) + paint.pad(sizeGap+strings.Repeat(" ", pad)) + size
}

// cacheDetailLines is the highlighted row in full: what the filesystem already
// knows about it, which config entries reference it, and whether a server has it
// open right now (docs/specs/CACHE.md).
func (m model) cacheDetailLines(inner, capacity int) []string {
	selected, ok := m.selectedCacheRow()
	if !ok {
		return sizeLines(nil, capacity)
	}
	switch selected.kind {
	case itemRow:
		return sizeLines(m.itemDetail(selected.repo, selected.item, inner), capacity)
	case partialsRow:
		return sizeLines(partialsDetail(selected.repo, inner), capacity)
	}
	return sizeLines(m.repoDetail(selected.repo, inner), capacity)
}

// itemDetail is one quantization: the thing a llama entry names, and the thing
// the delete key takes.
func (m model) itemDetail(repo *hubcache.Repo, item *hubcache.Item, inner int) []string {
	detail := &details{inner: inner}
	detail.add("repo", repo.ID, factStyle)
	detail.add("quant", item.Label, factStyle)
	detail.add("size", format.Bytes(item.Bytes), sizeStyle)
	if !item.Complete {
		detail.add("state", "incomplete — some of its shards are not on disk", alarmStyle)
	}
	detail.add("revision", orUnknown(repo.Revision), factStyle)
	detail.add("modified", stamp(item.Modified), factStyle)
	detail.files(item.Files)
	m.crossReference(detail, repo.ID, item.Label)
	return detail.lines
}

// repoDetail is a repository as one unit: how an MLX model and everything else
// the cache holds are read and deleted.
func (m model) repoDetail(repo *hubcache.Repo, inner int) []string {
	detail := &details{inner: inner}
	detail.add("repo", repo.ID, factStyle)
	detail.add("kind", repoKindWords(repo), factStyle)
	detail.add("size", format.Bytes(repo.Bytes), sizeStyle)
	if len(repo.Files) > 0 && !repo.Complete {
		detail.add("state", "incomplete — a download of it did not finish", alarmStyle)
	}
	detail.add("revision", orUnknown(repo.Revision), factStyle)
	detail.add("modified", stamp(repo.Modified), factStyle)
	detail.add("dir", repo.Dir, factStyle)
	detail.files(repo.Files)
	// No quant is named, so every entry and every server on this repository is
	// one this row's delete would take with it.
	m.crossReference(detail, repo.ID, "")
	return detail.lines
}

// partialsDetail is what a repository is holding back: the unfinished downloads
// themselves, and how long each has been sitting there — the age is what tells
// a fetch in flight from bytes nothing is coming back for.
func partialsDetail(repo *hubcache.Repo, inner int) []string {
	detail := &details{inner: inner}
	detail.add("repo", repo.ID, factStyle)
	detail.add("partials", unfinishedWords(len(repo.Partials)), noticeStyle)
	detail.add("reclaims", format.Bytes(repo.PartialBytes), sizeStyle)
	for i, partial := range repo.Partials {
		label := "files"
		if i > 0 {
			label = ""
		}
		detail.add(label, strings.Join([]string{filepath.Base(partial.Path),
			format.Bytes(partial.Bytes), age(partial.Modified)}, detailSeparator), factStyle)
	}
	return detail.lines
}

// crossReference is the two questions a delete is answered against: what in the
// config tree names these bytes, and whether a server has them open right now
// (docs/specs/CACHE.md).
func (m model) crossReference(detail *details, repo, quant string) {
	used := m.entriesUsing(repo, quant)
	if len(used) == 0 {
		detail.add("used by", "no config entry references it", quietStyle)
	}
	for i, id := range used {
		label := "used by"
		if i > 0 {
			label = ""
		}
		detail.add(label, "entry "+id, factStyle)
	}

	serving := m.servingNow(repo, quant)
	if len(serving) == 0 {
		detail.add("serving", "nothing is serving it right now", quietStyle)
		return
	}
	for i, status := range serving {
		label := "serving"
		if i > 0 {
			label = ""
		}
		detail.add(label, fmt.Sprintf("%s (pid %d) — stop it before deleting", status.EntryID, status.PID), noticeStyle)
	}
}

// entriesUsing lists the config entries that name one cached model, in tree
// order. The repo match is exact, the way the cache itself resolves a repo: an
// entry spelled with different capitalisation names a different directory, and
// would download into it. The quant match is MatchQuant's — the tag as the
// repo's files spell it, case aside (docs/specs/CACHE.md).
//
// An empty quant on either side widens the join to the whole repository: a row
// that names no quant is the whole repo, and an entry that names none is served
// with whichever file llama-server picks, which may be this one.
func (m model) entriesUsing(repo, quant string) []string {
	if m.tree == nil {
		return nil
	}

	var ids []string
	for _, entry := range m.tree.Entries {
		if entry.Repo != repo {
			continue
		}
		if quant == "" || entry.Quant == "" {
			ids = append(ids, entry.ID)
			continue
		}
		if _, same := hubcache.MatchQuant([]string{quant}, entry.Quant); same {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

// servingNow lists the servers that have one cached model open right now, off
// the live records. The rule is the deletion guard's, so what the pane says and
// what a delete refuses cannot disagree (docs/specs/CACHE.md): case-insensitive
// on the repo, because llama.cpp's own `-hf repo:TAG` resolution is, and an
// empty quant on either side widens to the whole repository.
func (m model) servingNow(repo, quant string) []serve.Status {
	var serving []serve.Status
	for _, status := range m.listing.Servers {
		if status.Phase == serve.PhaseExited || !strings.EqualFold(status.Repo, repo) {
			continue
		}
		if quant == "" || status.Quant == "" || strings.EqualFold(status.Quant, quant) {
			serving = append(serving, status)
		}
	}
	return serving
}

// details is a pane's lines under construction: one label column, values wrapped
// into it. It is the same field layout the serve view's detail pane uses, so the
// two panes read as one object in two views.
type details struct {
	inner int
	lines []string
}

// add appends one labelled field.
func (d *details) add(label, value string, style lipgloss.Style) {
	d.lines = append(d.lines, detailField(label, value, d.inner, style)...)
}

// files lists a set of files with their sizes, each shard on its own line —
// shards are what a sharded quant is on disk, and hiding them would hide half
// of what a delete removes.
func (d *details) files(files []hubcache.File) {
	for i, file := range files {
		label := "files"
		if i > 0 {
			label = ""
		}
		d.add(label, file.Name+detailSeparator+format.Bytes(file.Bytes), factStyle)
	}
}

// repoKindWords says what cria can do with a repository, in the words the view
// has room for: the kind the walk judged, and the Hub namespace when it is not a
// model.
func repoKindWords(repo *hubcache.Repo) string {
	if repo.Type != hubcache.RepoModel {
		return repo.Kind.String() + detailSeparator + string(repo.Type)
	}
	return repo.Kind.String()
}

// orUnknown keeps a detail readable when the cache holds no answer: a repo with
// no refs/main has no current revision, which is a fact about the repo rather
// than a hole in the pane.
func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(unknown)"
	}
	return value
}

// stamp is when something landed on disk.
func stamp(at time.Time) string {
	if at.IsZero() {
		return "(unknown)"
	}
	return at.Format(time.DateTime)
}

// age is how long ago it landed, at the resolution the answer is read at: an
// unfinished download from ten minutes ago may still be being fetched, one from
// last week is dead weight.
func age(at time.Time) string {
	if at.IsZero() {
		return "(unknown)"
	}
	switch since := time.Since(at); {
	case since < time.Minute:
		return "just now"
	case since < time.Hour:
		return fmt.Sprintf("%dm ago", int(since.Minutes()))
	case since < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(since.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(since.Hours()/24))
	}
}
