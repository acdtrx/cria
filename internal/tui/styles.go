package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/serve"
)

// The palette. cria's screens are read at a glance on a dark terminal, so colour
// carries meaning and nothing else: a phase, a backend, a key, a field label,
// the row the cursor is on. Everything else is body text or frame.
//
// The values are truecolor hex rather than 256-colour indices — lipgloss
// downgrades them on a terminal that cannot do more, and naming the exact
// channels is what makes the contrast computable. Legibility is enforced rather
// than judged: styles_test.go computes the WCAG relative-luminance ratio of
// every colour below against the background it is read on and refuses anything
// under AA (4.5:1), the frame excepted as chrome at 3:1.
const (
	inkHex    = "#c6cdd5" // the facts themselves
	dimHex    = "#7d8590" // context: field values that are not the point, hints, scope labels
	borderHex = "#586170" // the frame: a rule, not a word
	greenHex  = "#4ac26b" // running: the server answers
	yellowHex = "#d9a03d" // downloading: something is happening, nothing is wrong
	amberHex  = "#e0a458" // starting, notices, sizes, and the llama backend
	redHex    = "#f47067" // unhealthy, exited, and anything cria could not do
	blueHex   = "#6cb6ff" // the labels of a detail pane, and the mlx backend
	keyHex    = "#ff8f85" // the keys themselves, in the bar

	// The group headings the entry list is partitioned by: a hue of its own, so
	// a heading is not read as a row that lost its dot, and muted well under
	// body text, because the heading is furniture and the entries under it are
	// what the eye is hunting for. It has no band reading — the cursor never
	// stops on a heading (docs/specs/TUI.md).
	headingHex = "#9088b0"

	// The cursor's row sits on a band: the amber accent at a low alpha over the
	// terminal's own background. Body text and the accents clear AA on it as they
	// stand; the dim tone does not, so the band carries its own.
	bandHex    = "#2d2720"
	bandDimHex = "#939ca6"
)

// terminalBG is the background cria is read against: a dark terminal, taken at
// its darkest so a colour that clears AA here clears it on every darker-than-mid
// theme too.
const terminalBG = "#000000"

// The floors every colour is held to. textFloor is WCAG AA for body-sized text;
// chromeFloor is what a rule has to clear to be seen at all — the frame is drawn,
// not read, and holding it to AA would make the boxes shout over their contents.
const (
	textFloor   = 4.5
	chromeFloor = 3.0
)

// swatch is one colour of the palette with the background it is read on and the
// ratio it has to clear there.
type swatch struct {
	name  string
	hex   string
	on    string
	floor float64
}

// palette is every colour the frame draws with, declared once so the contrast
// test can enumerate it: a colour that is not in this table is a colour no
// screen may use. The band entries are the second reading of a colour that has
// two — on the terminal's own background, and on the cursor's row.
var palette = []swatch{
	{name: "ink", hex: inkHex, on: terminalBG, floor: textFloor},
	{name: "dim", hex: dimHex, on: terminalBG, floor: textFloor},
	{name: "border", hex: borderHex, on: terminalBG, floor: chromeFloor},
	{name: "green", hex: greenHex, on: terminalBG, floor: textFloor},
	{name: "yellow", hex: yellowHex, on: terminalBG, floor: textFloor},
	{name: "amber", hex: amberHex, on: terminalBG, floor: textFloor},
	{name: "red", hex: redHex, on: terminalBG, floor: textFloor},
	{name: "blue", hex: blueHex, on: terminalBG, floor: textFloor},
	{name: "key", hex: keyHex, on: terminalBG, floor: textFloor},
	{name: "heading", hex: headingHex, on: terminalBG, floor: textFloor},

	{name: "band ink", hex: inkHex, on: bandHex, floor: textFloor},
	{name: "band green", hex: greenHex, on: bandHex, floor: textFloor},
	{name: "band yellow", hex: yellowHex, on: bandHex, floor: textFloor},
	{name: "band dim", hex: bandDimHex, on: bandHex, floor: textFloor},
	{name: "band amber", hex: amberHex, on: bandHex, floor: textFloor},
	{name: "band red", hex: redHex, on: bandHex, floor: textFloor},
}

// The palette as lipgloss reads it.
var (
	ink     = lipgloss.Color(inkHex)
	dim     = lipgloss.Color(dimHex)
	border  = lipgloss.Color(borderHex)
	green   = lipgloss.Color(greenHex)
	yellow  = lipgloss.Color(yellowHex)
	amber   = lipgloss.Color(amberHex)
	red     = lipgloss.Color(redHex)
	blue    = lipgloss.Color(blueHex)
	keyRed  = lipgloss.Color(keyHex)
	heading = lipgloss.Color(headingHex)
	band    = lipgloss.Color(bandHex)
	bandDim = lipgloss.Color(bandDimHex)
)

// The styles every screen draws with. One file holds them so a change of palette
// is one edit rather than a hunt through the views.
//
// labelStyle is a detail pane's left column and keyStyle the bar's keys: both
// are structure rather than content, and both are coloured for it — the label
// says "this is what the value is", the key says "this is what you press".
// brokenStyle is a row that is listed but cannot be acted on; it borrows the
// alarm colour because a file cria refused is exactly that. headingStyle names
// a group of the entry list: structure again, and left unbolded so the name
// stands over its entries without competing with them for the eye.
var (
	frameStyle   = lipgloss.NewStyle().Foreground(border)
	readyStyle   = lipgloss.NewStyle().Foreground(green)
	titleStyle   = lipgloss.NewStyle().Foreground(dim).Bold(true)
	factStyle    = lipgloss.NewStyle().Foreground(ink)
	labelStyle   = lipgloss.NewStyle().Foreground(blue)
	keyStyle     = lipgloss.NewStyle().Foreground(keyRed).Bold(true)
	hintStyle    = lipgloss.NewStyle().Foreground(dim)
	quietStyle   = lipgloss.NewStyle().Foreground(dim)
	noticeStyle  = lipgloss.NewStyle().Foreground(amber)
	sizeStyle    = lipgloss.NewStyle().Foreground(amber)
	alarmStyle   = lipgloss.NewStyle().Foreground(red)
	brokenStyle  = lipgloss.NewStyle().Foreground(red)
	headingStyle = lipgloss.NewStyle().Foreground(heading)
)

// The same styles on the cursor's band. A row under the cursor is drawn on a
// background, so every fragment of it — the separators and the padding included
// — carries that background, and the dim tone is swapped for the one that stays
// legible over it.
var (
	bandStyle       = lipgloss.NewStyle().Background(band)
	bandNameStyle   = lipgloss.NewStyle().Foreground(amber).Background(band).Bold(true)
	bandReadyStyle  = lipgloss.NewStyle().Foreground(green).Background(band)
	bandFactStyle   = lipgloss.NewStyle().Foreground(ink).Background(band)
	bandQuietStyle  = lipgloss.NewStyle().Foreground(bandDim).Background(band)
	bandNoticeStyle = lipgloss.NewStyle().Foreground(amber).Background(band)
	bandAlarmStyle  = lipgloss.NewStyle().Foreground(red).Background(band)
)

// rowPaint is how one row of a list is drawn: in the palette as it stands, or on
// the cursor's band. Both lists ask for it the same way, so the highlight is one
// decision rather than one per view.
type rowPaint struct{ cursor bool }

// paintFor is the paint a row is drawn with.
func paintFor(cursor bool) rowPaint { return rowPaint{cursor: cursor} }

// name is the row's own identity: the entry id, the repo, the quant tag.
func (p rowPaint) name() lipgloss.Style {
	if p.cursor {
		return bandNameStyle
	}
	return factStyle
}

// ready is a row that could be acted on now rather than after a download: the
// cached dot, in the same green a running server's phase is drawn in. The two
// greys it used to be told apart by read as one dot at a glyph's size.
func (p rowPaint) ready() lipgloss.Style {
	if p.cursor {
		return bandReadyStyle
	}
	return readyStyle
}

// fact is something the row states outright.
func (p rowPaint) fact() lipgloss.Style {
	if p.cursor {
		return bandFactStyle
	}
	return factStyle
}

// quiet is what the row carries as context.
func (p rowPaint) quiet() lipgloss.Style {
	if p.cursor {
		return bandQuietStyle
	}
	return quietStyle
}

// notice is what the row flags: an unfinished download, an incomplete quant.
func (p rowPaint) notice() lipgloss.Style {
	if p.cursor {
		return bandNoticeStyle
	}
	return noticeStyle
}

// broken is a row cria cannot act on.
func (p rowPaint) broken() lipgloss.Style {
	if p.cursor {
		return bandAlarmStyle
	}
	return brokenStyle
}

// alarm is a row reporting something that went wrong — a crash report, which is
// drawn in it whole rather than by the phase word alone (see statusRow).
func (p rowPaint) alarm() lipgloss.Style {
	if p.cursor {
		return bandAlarmStyle
	}
	return alarmStyle
}

// phase is a phase word in the colour that phase is spelled in, painted like the
// row. The one phase drawn as context rather than as a state — a word cria does
// not know — takes the band's own dim tone, the only grey legible there.
func (p rowPaint) phase(phase serve.Phase) lipgloss.Style {
	if !p.cursor {
		return phaseTone(phase)
	}
	if phaseColor(phase) == dim {
		return bandQuietStyle
	}
	return phaseTone(phase).Background(band)
}

// size is a number of bytes, which every list quotes in the accent so the
// column reads as one thing down the screen.
func (p rowPaint) size() lipgloss.Style {
	if p.cursor {
		return bandNoticeStyle
	}
	return sizeStyle
}

// marker is the cursor's own mark, in the two cells every row keeps for it.
func (p rowPaint) marker() string {
	if p.cursor {
		return bandNameStyle.Render(cursorMark)
	}
	return nothingHere
}

// cell is one column of a row: its text in its own style, padded to the column's
// width with the row's own background so the columns line up down the list.
func (p rowPaint) cell(text string, style lipgloss.Style, width int) string {
	return style.Render(text) + p.pad(strings.Repeat(" ", max(width-lipgloss.Width(text), 0)))
}

// pad is plain space painted like the row, so the band runs unbroken across the
// separators and the padding rather than showing the terminal through them.
func (p rowPaint) pad(spaces string) string {
	if !p.cursor {
		return spaces
	}
	return bandStyle.Render(spaces)
}

// join holds a row's facts apart, the separator painted like the row.
func (p rowPaint) join(facts ...string) string {
	return strings.Join(facts, p.pad(factSeparator))
}

// fill makes a row exactly as wide as the pane it sits in, so the cursor's row
// reads as a band across it rather than as a coloured word. A row that is not
// the cursor's is left for the pane to pad.
func (p rowPaint) fill(line string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(line) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return line + p.pad(strings.Repeat(" ", width-lipgloss.Width(line)))
}

// backendTone is the colour a backend's name is spelled in. The two are
// different hues rather than two weights of one: which backend the lists are
// showing is the one thing about the serve view that changes under the user,
// and it has to be recognisable without reading (docs/specs/TUI.md).
func backendTone(backend config.Backend) lipgloss.Style {
	if backend == config.BackendMLX {
		return lipgloss.NewStyle().Foreground(blue).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(amber).Bold(true)
}

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
		return dim
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
// The frame's heights. defaultRows is what a view is given before the terminal
// has said how tall it is, and minPaneRows is the least a box can be: two border
// rows and one line worth reading. A screen with less than that left drops the
// view pane rather than drawing a box with nothing in it.
const (
	defaultWidth = 80
	minWidth     = 8
	defaultRows  = 12
	minPaneRows  = 3
)

// pane draws one bordered box with its title sitting in the top border. Every
// screen is one of these, which is what makes the status box read as the same
// object in every view (docs/specs/TUI.md).
//
// The title arrives already rendered. Most are one word in the frame's title
// tone, but the serve view's carries the active backend in that backend's own
// colour, so what a title is spelled in belongs to the screen that names it
// rather than to the box.
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

// paneTitle is a box's name in the frame's own title tone — what a screen passes
// to pane when its title is just a word.
func paneTitle(name string) string { return titleStyle.Render(name) }

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
	return frameStyle.Render(border.TopLeft+border.Top+" ") + title +
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
