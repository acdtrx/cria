package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"

	"cria/internal/format"
	"cria/internal/serve"
)

// statusTitle names the box in every view. It carries no key hints of its own —
// the keys that act on what it shows live in the bar's server scope, wherever
// the user happens to be (docs/specs/TUI.md).
const statusTitle = "servers"

// factSeparator is what holds one line's facts apart. Two spaces rather than a
// glyph: the box is read at a glance, and a separator that draws attention
// competes with the facts it separates.
const factSeparator = "  "

// barWidth is how wide a download's bar is drawn. Fixed rather than
// proportional: the bar sits beside the byte counts, and one that grows with
// the terminal would push them around every resize.
const barWidth = 24

// boxCursor is the pick cursor as the box draws it: a server key that could mean
// more than one server turns the box into that key's picker, and this says which
// row the answer is standing on and how wide the band under it reaches (pick.go).
// The zero value is every other draw of the box — no marker column, no band.
type boxCursor struct {
	picking bool
	row     int // the picked server, as a position in the listing
	inner   int // the pane's inner width, so the band spans the box
}

// on reports whether the pick cursor is standing on the row at this position.
func (c boxCursor) on(row int) bool { return c.picking && c.row == row }

// statusLines is everything the persistent box shows, one line per fact worth a
// line. Every display state docs/specs/TUI.md names is decided here.
//
// Nothing at all in the state directory is the stopped state: the box falls
// back to the entry that was started last, so the server keys keep a target
// across sessions. An exited record is *not* that state — it is a crash report
// cria still holds, and it stays on screen until it is dismissed or the entry
// starts again.
//
// What cria is in the middle of doing is shown here too, and nowhere else: an
// entry with an action in flight reads as starting, stopping or restarting from
// the keypress rather than from the next observation (lifecycle.go).
func statusLines(listing serve.StatusListing, pending pendingActions, saved prefs, bar progress.Model, cursor boxCursor) []string {
	if len(listing.Servers) == 0 && len(listing.Broken) == 0 && len(pending) == 0 {
		return []string{stoppedLine(saved)}
	}

	// Several servers at once is entries declaring different ports (docs/cria.md,
	// v1 surface). Their lines are a table: each fact is a column as wide as
	// that fact on any row, so two servers are compared down the box rather than
	// re-parsed per line. A single server is the same table with one row.
	//
	// An entry cria is starting has no record to observe yet, so it is a row of
	// its own, under the ones there is something to say about. It joins the same
	// table, which is what keeps the box from shifting sideways when the record
	// appears and its row fills in.
	rows := make([]serverRow, 0, len(listing.Servers)+len(pending))
	for i, status := range listing.Servers {
		rows = append(rows, statusRow(status, pending.verb(status.EntryID), paintFor(cursor.on(i))))
	}
	unrecorded := pending.unrecorded(listing.Servers)
	for _, entryID := range unrecorded {
		rows = append(rows, pendingRow(entryID, pending.verb(entryID), paintFor(false)))
	}
	widths := columnWidths(rows)

	var lines []string
	for i, row := range rows {
		line := row.line(widths)
		if cursor.picking {
			// Every row keeps the two cells the marker sits in while the box is
			// a picker, so the table does not shift as the cursor moves down it.
			line = row.paint.fill(row.paint.marker()+line, cursor.inner)
		}
		lines = append(lines, line)
		if i >= len(listing.Servers) {
			continue // an entry with no record has nothing to add under its row
		}
		if listing.Servers[i].Phase == serve.PhaseDownloading {
			lines = append(lines, downloadLine(listing.Servers[i].Progress, bar))
		}
		if listing.Servers[i].Phase == serve.PhaseExited {
			lines = append(lines, quietStyle.Render("log "+listing.Servers[i].LogPath))
		}
	}
	for _, broken := range listing.Broken {
		lines = append(lines, brokenLines(broken)...)
	}
	return lines
}

// unrecorded is the entries cria is acting on that the listing has no row for:
// a start between its keypress and the record it writes, or a restart of an
// entry whose record has just gone. Sorted, so the box does not reorder itself
// between two draws of the same thing.
func (p pendingActions) unrecorded(servers []serve.Status) []string {
	var ids []string
	for entryID := range p {
		if slices.ContainsFunc(servers, func(status serve.Status) bool { return status.EntryID == entryID }) {
			continue
		}
		ids = append(ids, entryID)
	}
	slices.Sort(ids)
	return ids
}

// serverRow is one server's line as columns plus an uncolumned tail. Live and
// exited rows share their first columns (entry, phase, backend, model), so the
// box lines up across states; what follows differs too much between the two to
// be worth forcing into one grid.
type serverRow struct {
	cells []cell
	tail  string
	paint rowPaint // as the palette stands, or on the pick cursor's band
}

// cell is one column of a server's line: the text and the style it is drawn in.
type cell struct {
	text  string
	style lipgloss.Style
}

// The combination a server is running sits against the model it varies: which
// quant, which layout, which context — the record's own picks, spelled the way
// `cria start` takes them (docs/specs/TUI.md). A flat entry picked nothing, so
// its cell is empty and the column collapses out of a box where no server has
// picks (line).
//
// statusRow is a server's place in the box. The phase word carries the colour,
// so every other fact is spelled in the same weight whatever the phase — a
// probe that says "connection refused" is normal during a start and alarming
// during a run, and the phase is what says which. An exited row is the crash
// report: nothing is claimed about what it costs or answers — cria never
// collected its exit status, and the log is the evidence (docs/specs/SERVE.md).
//
// A verb is an action cria is running on this server right now, and it takes the
// phase column: the observation behind the phase was taken before the keypress,
// so "running" there would be the older truth of the two (lifecycle.go).
func statusRow(status serve.Status, verb string, paint rowPaint) serverRow {
	phase := cell{string(status.Phase), paint.phase(status.Phase)}
	if status.Phase == serve.PhaseExited {
		phase.style = paint.alarm()
	}
	if verb != "" {
		phase = cell{verb, paint.notice()}
	}

	if status.Phase == serve.PhaseExited {
		return serverRow{
			paint: paint,
			cells: []cell{
				{status.EntryID, paint.alarm()},
				phase,
				{string(status.Backend), paint.alarm()},
				{format.HubReference(status.Repo, status.Quant), paint.alarm()},
				{format.Picks(status.Selection), paint.alarm()},
			},
			tail: paint.alarm().Render(fmt.Sprintf("pid %d is gone", status.PID) +
				factSeparator + "launched " + status.LaunchedAt.Format(time.DateTime)),
		}
	}

	row := serverRow{paint: paint, cells: []cell{
		{status.EntryID, paint.fact()},
		phase,
		{string(status.Backend), paint.quiet()},
		{format.HubReference(status.Repo, status.Quant), paint.fact()},
		{format.Picks(status.Selection), paint.fact()},
		{fmt.Sprintf("pid %d", status.PID), paint.quiet()},
		{fmt.Sprintf(":%d", status.Port), paint.quiet()},
		{"up " + status.Uptime.Round(time.Second).String(), paint.quiet()},
	}}

	// What the process table had to say, when it had anything: a pid it could
	// not find costs the line its two numbers rather than reporting zero. The
	// columns stay (empty), so the rows behind this one keep their places.
	rss, cpu := "", ""
	if status.Stats.RSSBytes > 0 {
		rss = format.Bytes(status.Stats.RSSBytes)
	}
	if status.Stats.CPUPercent > 0 {
		cpu = fmt.Sprintf("%.1f%% cpu", status.Stats.CPUPercent)
	}
	row.cells = append(row.cells, cell{rss, paint.quiet()}, cell{cpu, paint.quiet()})
	if status.Health.Detail != "" {
		row.tail = paint.quiet().Render(status.Health.Detail)
	}
	return row
}

// pendingRow is an entry cria is acting on that has no record to draw: the
// moment between ⏎ and the record a start writes. Its id and what cria is doing
// are the whole row — nothing else has happened yet, and a row of blank columns
// would look like facts cria failed to read (CODING-RULES §4).
func pendingRow(entryID, verb string, paint rowPaint) serverRow {
	return serverRow{paint: paint, cells: []cell{
		{entryID, paint.fact()},
		{verb, paint.notice()},
	}}
}

// columnWidths is how wide each column has to be for that column's widest text
// on any row — the table's grid. A row's tail is not a column and sets nothing.
func columnWidths(rows []serverRow) []int {
	var widths []int
	for _, row := range rows {
		for i, c := range row.cells {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(c.text))
		}
	}
	return widths
}

// line draws one row on the grid: each cell padded to its column, the tail as
// it is. A column no row has anything for collapses rather than leaving a gap —
// the cost `ps` did not answer for, the picks a box of flat servers has none of
// — and one another row does fill is padded here, so the grid holds. The row's
// own paint carries the separators and the padding, so a row on the band reads
// as one lit line rather than as lit words.
func (r serverRow) line(widths []int) string {
	var facts []string
	for i, c := range r.cells {
		if c.text == "" && widths[i] == 0 {
			continue
		}
		facts = append(facts, r.paint.cell(c.text, c.style, widths[i]))
	}
	if r.tail != "" {
		facts = append(facts, r.tail)
	}
	return strings.TrimRight(strings.Join(facts, r.paint.pad(factSeparator)), " ")
}

// brokenLines is a record file cria refused. It names a pid cria started, so it
// is shown rather than dropped, with the one line that clears it.
func brokenLines(broken serve.BrokenRecord) []string {
	return []string{
		alarmStyle.Render(strings.Join([]string{broken.EntryID, "unreadable record", broken.Path}, factSeparator)),
		quietStyle.Render(broken.Err.Error() + "; delete that file once the pid it names is gone"),
	}
}

// stoppedLine is the box with nothing live in it. The entry it names is what the
// server keys would act on, which is why it is shown at all.
func stoppedLine(saved prefs) string {
	if saved.LastStarted == "" {
		return quietStyle.Render(strings.Join([]string{"stopped", "no server has been started yet"}, factSeparator))
	}
	return quietStyle.Render(strings.Join([]string{saved.LastStarted, "stopped", "started last; nothing is running now"}, factSeparator))
}

// downloadLine is how far a first start has got. The bar needs a total, and a
// total needs the Hub: when it could not answer, the bytes on disk are still the
// honest half and the reason travels with them (docs/specs/SERVE.md).
func downloadLine(done serve.Progress, bar progress.Model) string {
	if !done.Known || done.Total <= 0 {
		reason := ""
		if done.Reason != "" {
			reason = ": " + done.Reason
		}
		return quietStyle.Render(fmt.Sprintf("%s so far (no total%s)", format.Bytes(done.Bytes), reason))
	}

	// A cache that already holds more than the Hub says the model comes to is a
	// full bar rather than an overflowing one: shared blobs and a repo's other
	// files can both push the count past the total, and neither means the
	// download is more than done.
	fraction := min(float64(done.Bytes)/float64(done.Total), 1)
	counts := fmt.Sprintf("%s of %s", format.Bytes(done.Bytes), format.Bytes(done.Total))
	return bar.ViewAs(fraction) + factSeparator + quietStyle.Render(counts)
}

// newProgressBar builds the one bar the box draws. It is never animated: the
// box is redrawn from a fresh observation every refresh, and a spring
// interpolating towards a number that is already the truth would only make the
// display lag the cache.
func newProgressBar() progress.Model {
	return progress.New(progress.WithWidth(barWidth))
}

// boxTarget is what the status box is showing, which is what the server keys
// act on — whatever the list selection is (docs/specs/TUI.md). The keybar reads
// it, so a key is only offered when there is something for it to do: at least
// one of the rows the box shows. Which one a key means when there are several is
// the user's answer to give (pick.go), not the bar's.
type boxTarget struct {
	live   bool // at least one server cria can still see: stop, kill and log have something to act on
	exited bool // at least one crash report is on screen: there is something to dismiss
	shown  bool // the box names an entry at all, live, exited or merely last-started
}

// targetOf reads the box's target off the same two things the box is drawn
// from.
func targetOf(listing serve.StatusListing, saved prefs) boxTarget {
	target := boxTarget{shown: saved.LastStarted != ""}
	for _, status := range listing.Servers {
		target.shown = true
		if status.Phase == serve.PhaseExited {
			target.exited = true
			continue
		}
		target.live = true
	}
	return target
}
