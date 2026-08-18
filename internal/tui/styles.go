package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"cria/internal/serve"
)

// The palette. cria's screens are mostly dim: the frame, the labels and the key
// hints recede so that the few facts that change — a phase, a failure, the
// server that is actually running — are the only bright things on screen.
//
// 256-colour indices rather than hex, so the frame keeps its intent on a
// terminal that cannot do more.
var (
	ink    = lipgloss.Color("252") // the facts themselves
	muted  = lipgloss.Color("245") // labels and keys: readable, not loud
	faint  = lipgloss.Color("240") // the frame, and everything that is only context
	green  = lipgloss.Color("42")  // running: the server answers
	yellow = lipgloss.Color("220") // downloading: something is happening, nothing is wrong
	amber  = lipgloss.Color("214") // starting, and what a keypress just did
	red    = lipgloss.Color("203") // unhealthy, exited, and anything cria could not do
)

// The styles every screen draws with. One file holds them so a change of palette
// is one edit rather than a hunt through the views.
var (
	frameStyle  = lipgloss.NewStyle().Foreground(faint)
	titleStyle  = lipgloss.NewStyle().Foreground(muted).Bold(true)
	factStyle   = lipgloss.NewStyle().Foreground(ink)
	labelStyle  = lipgloss.NewStyle().Foreground(faint)
	keyStyle    = lipgloss.NewStyle().Foreground(muted).Bold(true)
	quietStyle  = lipgloss.NewStyle().Foreground(faint)
	noticeStyle = lipgloss.NewStyle().Foreground(amber)
	alarmStyle  = lipgloss.NewStyle().Foreground(red)
)

// phaseTone is the colour a phase is spelled in (docs/specs/SERVE.md names the
// phases; this is the only place that decides what each one looks like).
//
// Exited shares red with unhealthy but not its weight: an exited record is a
// crash report to read, not an alarm to react to, so the whole line is drawn in
// it rather than the phase word alone (see statusLines).
func phaseTone(phase serve.Phase) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(phaseColor(phase))
}

// phaseColor is the mapping itself. A phase cria does not know is drawn as
// context rather than as a state: an unrecognised word must never borrow the
// authority of green.
func phaseColor(phase serve.Phase) color.Color {
	switch phase {
	case serve.PhaseRunning:
		return green
	case serve.PhaseDownloading:
		return yellow
	case serve.PhaseStarting:
		return amber
	case serve.PhaseUnhealthy, serve.PhaseExited:
		return red
	default:
		return faint
	}
}

// The frame's widths. defaultWidth is what a view draws at before the terminal
// has said how wide it is — the first render happens before the first
// WindowSizeMsg arrives.
//
// minWidth is the floor the frame stops following the terminal at: a box needs
// two border cells and two padding columns before it can hold a single
// character, and a terminal narrower than that has nothing to show either way.
// The frame draws at the floor there rather than computing a negative box.
const (
	defaultWidth = 80
	minWidth     = 8
)

// pane draws one bordered box with its title sitting in the top border. Every
// screen is one of these, which is what makes the status box read as the same
// object in every view (docs/specs/TUI.md).
func pane(title string, width int, lines []string) string {
	if width < minWidth {
		width = minWidth
	}
	border := lipgloss.RoundedBorder()
	inner := width - 4 // two border cells, and one padding column on each side

	var box strings.Builder
	box.WriteString(topBorder(border, title, width))
	for _, line := range lines {
		box.WriteString("\n")
		box.WriteString(frameStyle.Render(border.Left))
		box.WriteString(" " + fit(line, inner) + " ")
		box.WriteString(frameStyle.Render(border.Right))
	}
	box.WriteString("\n")
	box.WriteString(frameStyle.Render(border.BottomLeft + strings.Repeat(border.Bottom, width-2) + border.BottomRight))
	return box.String()
}

// topBorder is the box's first line: the border with the title let into it. A
// title too long for the box loses its tail rather than pushing the corner off
// the screen, and a box too narrow to hold any of it is drawn plain.
func topBorder(border lipgloss.Border, title string, width int) string {
	const lead = 3 // the corner, one rule cell, and the space before the title

	room := width - lead - 2 // the space after the title, and the far corner
	if title == "" || room < 1 {
		return frameStyle.Render(border.TopLeft + strings.Repeat(border.Top, width-2) + border.TopRight)
	}
	if lipgloss.Width(title) > room {
		title = lipgloss.NewStyle().MaxWidth(room).Render(title)
	}

	fill := width - lead - lipgloss.Width(title) - 2
	return frameStyle.Render(border.TopLeft+border.Top+" ") +
		titleStyle.Render(title) +
		frameStyle.Render(" "+strings.Repeat(border.Top, fill)+border.TopRight)
}

// fit makes one line exactly width cells wide: truncated when it is too long,
// padded when it is too short. Both halves measure rendered width rather than
// bytes, because every line reaching here already carries its colours.
func fit(line string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(line) > width {
		line = lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return line + strings.Repeat(" ", width-lipgloss.Width(line))
}
