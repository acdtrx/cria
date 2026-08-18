package tui

import (
	"fmt"
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

// statusLines is everything the persistent box shows, one line per fact worth a
// line. Every display state docs/specs/TUI.md names is decided here.
//
// Nothing at all in the state directory is the stopped state: the box falls
// back to the entry that was started last, so the server keys keep a target
// across sessions. An exited record is *not* that state — it is a crash report
// cria still holds, and it stays on screen until it is dismissed or the entry
// starts again.
func statusLines(listing serve.StatusListing, saved prefs, bar progress.Model) []string {
	if len(listing.Servers) == 0 && len(listing.Broken) == 0 {
		return []string{stoppedLine(saved)}
	}

	// Several servers at once is entries declaring different ports (docs/cria.md,
	// v1 surface). Their lines are a table: each fact is a column as wide as
	// that fact on any row, so two servers are compared down the box rather than
	// re-parsed per line. A single server is the same table with one row.
	rows := make([]serverRow, len(listing.Servers))
	for i, status := range listing.Servers {
		rows[i] = statusRow(status)
	}
	widths := columnWidths(rows)

	var lines []string
	for i, row := range rows {
		lines = append(lines, row.line(widths))
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

// serverRow is one server's line as columns plus an uncolumned tail. Live and
// exited rows share their first columns (entry, phase, backend, model), so the
// box lines up across states; what follows differs too much between the two to
// be worth forcing into one grid.
type serverRow struct {
	cells []cell
	tail  string
}

// cell is one column of a server's line: the text and the style it is drawn in.
type cell struct {
	text  string
	style lipgloss.Style
}

// statusRow is a server's place in the box. The phase word carries the colour,
// so every other fact is spelled in the same weight whatever the phase — a
// probe that says "connection refused" is normal during a start and alarming
// during a run, and the phase is what says which. An exited row is the crash
// report: nothing is claimed about what it costs or answers — cria never
// collected its exit status, and the log is the evidence (docs/specs/SERVE.md).
func statusRow(status serve.Status) serverRow {
	if status.Phase == serve.PhaseExited {
		return serverRow{
			cells: []cell{
				{status.EntryID, alarmStyle},
				{string(status.Phase), alarmStyle},
				{string(status.Backend), alarmStyle},
				{format.HubReference(status.Repo, status.Quant), alarmStyle},
			},
			tail: alarmStyle.Render(fmt.Sprintf("pid %d is gone", status.PID) +
				factSeparator + "launched " + status.LaunchedAt.Format(time.DateTime)),
		}
	}

	row := serverRow{cells: []cell{
		{status.EntryID, factStyle},
		{string(status.Phase), phaseTone(status.Phase)},
		{string(status.Backend), quietStyle},
		{format.HubReference(status.Repo, status.Quant), factStyle},
		{fmt.Sprintf("pid %d", status.PID), quietStyle},
		{fmt.Sprintf(":%d", status.Port), quietStyle},
		{"up " + status.Uptime.Round(time.Second).String(), quietStyle},
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
	row.cells = append(row.cells, cell{rss, quietStyle}, cell{cpu, quietStyle})
	if status.Health.Detail != "" {
		row.tail = quietStyle.Render(status.Health.Detail)
	}
	return row
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
// it is. Empty trailing columns collapse rather than leaving a gap before the
// tail.
func (r serverRow) line(widths []int) string {
	var facts []string
	for i, c := range r.cells {
		if c.text == "" && i >= len(r.cells)-2 && widths[i] == 0 {
			continue
		}
		facts = append(facts, c.style.Render(c.text)+strings.Repeat(" ", widths[i]-lipgloss.Width(c.text)))
	}
	if r.tail != "" {
		facts = append(facts, r.tail)
	}
	return strings.TrimRight(strings.Join(facts, factSeparator), " ")
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
// it, so a key is only offered when there is something for it to do.
type boxTarget struct {
	live   bool // a server cria can still see: stop and log have something to act on
	exited bool // a crash report is on screen: there is something to dismiss
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
