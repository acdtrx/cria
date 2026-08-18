package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/tools"
)

// The tools pane is the host's own state: what cria found, where, and what each
// finding costs (docs/specs/TOOLS.md). It is a pane rather than a screen — it
// hangs over whichever view is behind it and one key takes it away again
// (docs/specs/TUI.md).
//
// Nothing here judges a tool. The report is the tool check's, rendered as it
// came: the status, the resolved path, the build number that decides llama
// serving, and the one action that clears anything degraded.

// toolsMsg is one run of the tool check, taken off the UI thread because it
// execs `llama-server --version`.
type toolsMsg struct{ report tools.Report }

// The marks in front of a tool, the same three weights the entry list's dots
// carry: what cria can use, what it cannot, and what it found but will not.
const (
	usableMark   = "●"
	degradedMark = "⚠"
	missingMark  = "○"
)

// openTools is t: show the report, freshly taken.
//
// Fresh is the point. The check execs a program, so nothing puts it on the
// refresh tick; opening this pane is a deliberate question about the host right
// now, and the answer it gets is the one a start would get.
func (m model) openTools() (tea.Model, tea.Cmd) {
	m.toolsOpen = true
	m.alert = alert{}
	return m, m.checkTools
}

// checkTools runs the check.
func (m model) checkTools() tea.Msg { return toolsMsg{report: m.host.tools(m.settings())} }

// pressInTools is the keyboard while the pane is up: close it, or quit. The keys
// underneath are deliberately not live — the pane is read, not acted from.
func (m model) pressInTools(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.leaveTools):
		m.toolsOpen = false
		return m, nil
	}
	return m, nil
}

// toolsPanel draws the report: one line per managed tool, and the lines that say
// what a degraded one takes away and what clears it.
func (m model) toolsPanel(width, rows int) string {
	inner := width - 4
	if !m.reported {
		return pane(paneTitle(toolsScope), width, sizeLines([]string{quietStyle.Render("asking the host what it has…")}, rows-2))
	}

	var lines []string
	for _, tool := range m.report.All() {
		lines = append(lines, toolLines(tool, inner)...)
	}
	return pane(paneTitle(toolsScope), width, sizeLines(lines, rows-2))
}

// toolLines is one tool's finding: where it resolved and what cria may do with
// it, then — only when something is wrong — what that costs and the one action
// that fixes it (docs/specs/TOOLS.md).
func toolLines(tool tools.Tool, inner int) []string {
	facts := []string{statusMark(tool.Status), factStyle.Render(string(tool.Name))}
	if tool.Path == "" {
		facts = append(facts, alarmStyle.Render("not on this host"))
	} else {
		facts = append(facts, quietStyle.Render(tool.Path))
	}
	if tool.Override {
		facts = append(facts, quietStyle.Render("(config.toml)"))
	}
	// The build number is the whole llama-server verdict: it is what says
	// whether a `-hf` download lands in the hub cache cria reads
	// (docs/specs/TOOLS.md).
	if tool.Build > 0 {
		facts = append(facts, quietStyle.Render(fmt.Sprintf("build %d", tool.Build)))
	}
	if tool.Name == tools.LlamaServer && tool.Usable() {
		facts = append(facts, factStyle.Render("hub cache ok"))
	}

	lines := []string{strings.Join(facts, factSeparator)}
	if tool.Disables != "" {
		lines = append(lines, indented("disables "+tool.Disables, inner, quietStyle)...)
	}
	if tool.Fix != "" {
		lines = append(lines, indented("fix "+tool.Fix, inner, noticeStyle)...)
	}
	return lines
}

// statusMark is the tool's state at a glance. An outdated or unverified build is
// its own mark: it is present, and cria still will not launch with it.
func statusMark(status tools.Status) string {
	switch status {
	case tools.StatusFound:
		return factStyle.Render(usableMark)
	case tools.StatusMissing:
		return alarmStyle.Render(missingMark)
	default:
		return noticeStyle.Render(degradedMark)
	}
}

// toolIndent is how far a tool's own sentences sit under its finding.
const toolIndent = "  "

// indented wraps one sentence under the line it belongs to, so a tool's own
// lines read as its own.
func indented(message string, inner int, style lipgloss.Style) []string {
	var lines []string
	for _, chunk := range wrap(message, max(inner-len(toolIndent), 1)) {
		lines = append(lines, toolIndent+style.Render(chunk))
	}
	return lines
}
