package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"

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
	// v1 surface): rare, and worth no more than stacking their lines.
	var lines []string
	for _, status := range listing.Servers {
		lines = append(lines, serverLines(status, bar)...)
	}
	for _, broken := range listing.Broken {
		lines = append(lines, brokenLines(broken)...)
	}
	return lines
}

// serverLines is one record's place in the box.
func serverLines(status serve.Status, bar progress.Model) []string {
	if status.Phase == serve.PhaseExited {
		return exitedLines(status)
	}
	lines := []string{liveLine(status)}
	if status.Phase == serve.PhaseDownloading {
		lines = append(lines, downloadLine(status.Progress, bar))
	}
	return lines
}

// liveLine is a server cria can still see: what it is, what it costs, and what
// it last answered. The phase word carries the colour, so every other fact is
// spelled in the same weight whatever the phase — a probe that says "connection
// refused" is normal during a start and alarming during a run, and the phase is
// what says which.
func liveLine(status serve.Status) string {
	facts := []string{
		factStyle.Render(status.EntryID),
		phaseTone(status.Phase).Render(string(status.Phase)),
		quietStyle.Render(string(status.Backend)),
		factStyle.Render(format.HubReference(status.Repo, status.Quant)),
		quietStyle.Render(fmt.Sprintf("pid %d", status.PID)),
		quietStyle.Render(fmt.Sprintf(":%d", status.Port)),
		quietStyle.Render("up " + status.Uptime.Round(time.Second).String()),
	}

	// What the process table had to say, when it had anything: a pid it could
	// not find costs the line its two numbers rather than reporting zero.
	if status.Stats.RSSBytes > 0 {
		facts = append(facts, quietStyle.Render(format.Bytes(status.Stats.RSSBytes)))
	}
	if status.Stats.CPUPercent > 0 {
		facts = append(facts, quietStyle.Render(fmt.Sprintf("%.1f%% cpu", status.Stats.CPUPercent)))
	}
	if status.Health.Detail != "" {
		facts = append(facts, quietStyle.Render(status.Health.Detail))
	}
	return strings.Join(facts, factSeparator)
}

// exitedLines is the crash report: what died, when it was launched, and the log
// that says why. Nothing is claimed about what it costs or what it answers —
// cria never collected its exit status, and the log is the evidence
// (docs/specs/SERVE.md).
func exitedLines(status serve.Status) []string {
	facts := []string{
		status.EntryID,
		string(status.Phase),
		string(status.Backend),
		format.HubReference(status.Repo, status.Quant),
		fmt.Sprintf("pid %d is gone", status.PID),
		"launched " + status.LaunchedAt.Format(time.DateTime),
	}
	return []string{
		alarmStyle.Render(strings.Join(facts, factSeparator)),
		quietStyle.Render("log " + status.LogPath),
	}
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
