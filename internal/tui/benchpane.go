package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/format"
	"cria/internal/serve"
)

// The bench pane is this session's measurements: every sweep that has finished
// since cria was opened, oldest first, and the one still running at the bottom
// of them. It is a pane rather than a screen — it hangs over whichever view is
// behind it and one key takes it away again (docs/specs/TUI.md).
//
// The log lives as long as the program does and no longer. A benchmark is a
// reading of this machine at this moment — another server on the host, a
// thermal state, a cache — and a file of them read back next week would invite
// exactly the comparison the numbers do not support. What the pane is for is
// comparing two servers on one afternoon, which is a session.
//
// Nothing here decides how a server is measured: the sweep, its sizes and every
// number in the table are serve's (internal/serve/bench.go). What is here is
// which server a keypress means, and how the answer is drawn.

// The messages a bench answers with.
type (
	// benchStepMsg is one step of a running sweep, read off the channel the
	// sweep reports through. more is false when the channel closed, which is
	// what stops the reader arming itself again.
	benchStepMsg struct {
		step serve.BenchStep
		more bool
	}
	// benchedMsg is a finished sweep, with everything it measured.
	benchedMsg struct{ result serve.BenchResult }
)

// benchStepBuffer is how many steps the sweep may run ahead of the frame. The
// sweep sends from its own goroutine and the frame reads with a command, so a
// frame busy redrawing never holds a measurement up.
const benchStepBuffer = 16

// benchRun is the sweep in flight: which server it is measuring, where it has
// got to, and the channel it reports through. One at a time — two sweeps
// against one host would each be timing the other's work.
type benchRun struct {
	entryID string
	step    serve.BenchStep
	stepped bool // a step has arrived, so there is progress to draw
	steps   chan serve.BenchStep
}

// openBench is b: show the session's benchmarks.
//
// Unlike the tools report, opening this asks the host nothing. Everything the
// pane draws has already been measured; the only thing that costs a server
// anything is ⏎.
func (m model) openBench() (tea.Model, tea.Cmd) {
	m.benchOpen = true
	m.alert = alert{}
	return m.syncEscScope(), nil
}

// pressInBench is the keyboard while the pane is up: start a bench, close it,
// or quit. The keys underneath are deliberately not live — the pane is read,
// and the one thing acted from it is its own.
func (m model) pressInBench(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.leaveBench):
		// A sweep that is running keeps running: it is minutes of work, it is
		// not the pane's, and its result lands in the log either way.
		m.benchOpen = false
		return m.syncEscScope(), nil
	case key.Matches(pressed, m.keys.runBench):
		return m.askWhichToBench()
	}
	return m, nil
}

// askWhichToBench is ⏎ in the pane. Its two refusals are the reason the key
// stays on the bar with nothing to measure: the pane exists to start benches,
// and a bar without ⏎ in it would leave the empty pane looking inert. What it
// cannot do, it says.
func (m model) askWhichToBench() (tea.Model, tea.Cmd) {
	if m.benching != nil {
		m.alert = alert{text: "a bench is already running", bad: true}
		return m.syncEscScope(), nil
	}
	if len(m.pickable(pickBench)) == 0 {
		m.alert = alert{text: "nothing is running to benchmark; start a server first", bad: true}
		return m.syncEscScope(), nil
	}
	// One live server is the whole answer and it is measured; several ask which
	// (pick.go). The sweep itself is never chosen here: the pane runs the
	// default sweep and only the default sweep — one keypress is the whole point
	// of it, and choosing sizes is what `cria bench` and its flags are for.
	return m.aim(pickBench)
}

// startBench runs one sweep against the server the key landed on.
//
// It is two commands rather than one. The sweep is minutes of blocking work in
// a goroutine of its own, and the frame has to keep redrawing while it runs —
// so the sweep reports its steps through a channel, and a second command reads
// one step and arms itself for the next. That is what puts progress on screen
// without the sweep ever touching the model.
func (m model) startBench(record serve.Record) (tea.Model, tea.Cmd) {
	steps := make(chan serve.BenchStep, benchStepBuffer)
	m.benching = &benchRun{entryID: record.EntryID, steps: steps}
	m.alert = alert{}

	servers := m.host.servers
	sweep := func() tea.Msg {
		result := servers.Bench(record, serve.DefaultBenchSpec(), func(step serve.BenchStep) { steps <- step })
		close(steps)
		return benchedMsg{result: result}
	}
	return m, tea.Batch(sweep, readBenchStep(steps))
}

// readBenchStep waits for the sweep's next step. A closed channel is the sweep
// having nothing left to say, and it is answered with more:false rather than
// with silence, so the frame stops waiting on a channel nobody will write to.
func readBenchStep(steps chan serve.BenchStep) tea.Cmd {
	return func() tea.Msg {
		step, open := <-steps
		return benchStepMsg{step: step, more: open}
	}
}

// benchStepped takes one step of the running sweep and arms the next read.
func (m model) benchStepped(msg benchStepMsg) (model, tea.Cmd) {
	if m.benching == nil || !msg.more {
		return m, nil
	}
	running := *m.benching
	running.step, running.stepped = msg.step, true
	m.benching = &running
	return m, readBenchStep(m.benching.steps)
}

// benched takes a finished sweep: it joins the log, newest last, and stops
// being what the pane draws as running.
//
// It says nothing under the status box while the pane is open — the result is
// right there, and a line repeating it would be one more true thing to read
// (docs/specs/TUI.md). With the pane closed it is exactly what the boxes cannot
// show: the sweep the user walked away from has finished.
func (m model) benched(msg benchedMsg) (model, tea.Cmd) {
	m.benching = nil
	m.benchLog = append(append([]serve.BenchResult{}, m.benchLog...), msg.result)
	if m.benchOpen {
		return m, nil
	}

	if failed := failedSizes(msg.result); failed > 0 {
		m.alert = alert{
			text: fmt.Sprintf("benchmarked %s: %d size(s) could not be measured; b shows why", msg.result.EntryID, failed),
			bad:  true,
		}
		return m.syncEscScope(), nil
	}
	m.alert = alert{text: fmt.Sprintf("benchmarked %s; b shows the numbers", msg.result.EntryID)}
	return m.syncEscScope(), nil
}

// failedSizes counts the sizes of one sweep that came back with no measurement
// at all — the holes in it, rather than the rows that lost a run.
func failedSizes(result serve.BenchResult) int {
	failed := 0
	for _, size := range result.Sizes {
		if !size.Measured() {
			failed++
		}
	}
	return failed
}

// benchPanel draws the pane: every finished sweep in the order they were taken,
// then the one still running.
//
// The newest is at the bottom, so a pane too short to hold everything keeps its
// tail: what was just measured is what the user is looking for, and the sweep
// from ten minutes ago is the one that can scroll off.
func (m model) benchPanel(width, rows int) string {
	inner := width - 4

	var lines []string
	if len(m.benchLog) == 0 && m.benching == nil {
		lines = append(lines, quietStyle.Render("no benches yet — ⏎ starts one"))
	}
	for i, result := range m.benchLog {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, benchLines(result, inner)...)
	}
	if m.benching != nil {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, benchRunningLines(*m.benching)...)
	}

	if capacity := rows - 2; capacity > 0 && len(lines) > capacity {
		lines = lines[len(lines)-capacity:]
	}
	return pane(paneTitle(benchScope), width, sizeLines(lines, rows-2))
}

// benchLines is one finished sweep: what was measured, when, and the table.
func benchLines(result serve.BenchResult, inner int) []string {
	head := strings.Join([]string{
		factStyle.Render(result.EntryID),
		quietStyle.Render(string(result.Backend)),
		factStyle.Render(format.HubReference(result.Repo, result.Quant)),
		quietStyle.Render(result.StartedAt.Format(time.TimeOnly)),
	}, factSeparator)

	lines := []string{head}
	lines = append(lines, benchTable(result)...)
	for _, size := range result.Sizes {
		// A size with no measurement at all is a hole in the sweep; one that
		// lost a run still answers, and its reason is why the row is thin.
		if size.Err != nil {
			style := noticeStyle
			if !size.Measured() {
				style = alarmStyle
			}
			lines = append(lines, wrapped(size.Err.Error(), inner, style)...)
		}
		if size.Cached() {
			// A prefix the server already held is a prefill that did not happen,
			// so the rate beside it is not the rate of reading that prompt.
			lines = append(lines, wrapped(fmt.Sprintf(
				"the %d-token size was answered partly out of the server's prompt cache; its prefill rate is optimistic",
				size.Tokens), inner, noticeStyle)...)
		}
		// A model that ends its answer early is not a slow model, but a size
		// whose answers were cut materially short had its decode rate measured
		// over a fraction of them. Which shortfalls are material is serve's
		// (serve.BenchSize.EndedEarly), so pane and CLI say it on the same rule.
		if size.EndedEarly(result.Spec.GenTokens) {
			lines = append(lines, wrapped(fmt.Sprintf(
				"the model ended its answer early on the %d-token size (%.0f of %d tokens on average)",
				size.Tokens, size.Mean.GenTokens, result.Spec.GenTokens), inner, noticeStyle)...)
		}
	}
	return lines
}

// benchTable is the sweep's numbers: the header, then one row per size. A size
// that was not measured keeps its row with the numbers left out — the reason is
// its own line under the table, where there is room for a sentence.
func benchTable(result serve.BenchResult) []string {
	rows := [][]string{{"size", "tokens", "prefill t/s", "ttft", "decode t/s"}}
	for _, size := range result.Sizes {
		if !size.Measured() {
			rows = append(rows, []string{strconv.Itoa(size.Tokens), benchMissing, benchMissing, benchMissing, benchMissing})
			continue
		}
		decode := benchMissing
		if size.Mean.DecodeRate > 0 {
			decode = fmt.Sprintf("%.1f", size.Mean.DecodeRate)
		}
		rows = append(rows, []string{
			strconv.Itoa(size.Tokens),
			fmt.Sprintf("%.0f", size.Mean.PromptTokens),
			fmt.Sprintf("%.0f", size.Mean.PrefillRate),
			size.Mean.TTFT.Round(time.Millisecond).String(),
			decode,
		})
	}

	widths := benchColumns(rows)
	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		style := factStyle
		if i == 0 {
			style = quietStyle
		}
		var cells []string
		for column, text := range row {
			cells = append(cells, style.Render(text)+strings.Repeat(" ", widths[column]-lipgloss.Width(text)))
		}
		lines = append(lines, benchIndent+strings.TrimRight(strings.Join(cells, factSeparator), " "))
	}
	return lines
}

// benchColumns is how wide each column of a table has to be for its widest cell
// — the same grid the status box draws its rows on, so a table is read across
// rather than parsed.
func benchColumns(rows [][]string) []int {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, text := range row {
			widths[i] = max(widths[i], lipgloss.Width(text))
		}
	}
	return widths
}

// benchRunningLines is the sweep in flight: which server, and where it has got
// to. A sweep that has not reported yet says so rather than showing a size it
// is not measuring.
func benchRunningLines(running benchRun) []string {
	lines := []string{strings.Join([]string{
		factStyle.Render(running.entryID),
		noticeStyle.Render("benchmarking…"),
	}, factSeparator)}

	switch {
	case !running.stepped:
		return append(lines, benchIndent+quietStyle.Render("starting"))
	case running.step.Warmup:
		return append(lines, benchIndent+quietStyle.Render("warming up (unmeasured)"))
	}
	return append(lines, benchIndent+quietStyle.Render(fmt.Sprintf("%d tokens (size %d/%d), run %d/%d",
		running.step.Size, running.step.Nth, running.step.Sizes, running.step.Run, running.step.Runs)))
}

// benchIndent is how far a sweep's own lines sit under the server they belong
// to, and benchMissing is what a size that was not measured shows in the
// columns the others hold numbers in.
const (
	benchIndent  = "  "
	benchMissing = "—"
)
