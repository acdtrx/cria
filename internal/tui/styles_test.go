package tui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// The palette is legible by construction, not by taste: every colour the frame
// draws with is checked here against the background it is read on, by the same
// arithmetic WCAG defines. A colour that cannot clear its floor fails the suite
// rather than shipping as "a bit dim on my terminal".

// contrast is the WCAG contrast ratio between two colours: their relative
// luminances, lighter over darker, each offset by 0.05.
func contrast(a, b string) float64 {
	light, dark := luminance(a), luminance(b)
	if light < dark {
		light, dark = dark, light
	}
	return (light + 0.05) / (dark + 0.05)
}

// luminance is WCAG relative luminance: each channel linearised out of sRGB,
// then weighted for how much the eye takes from it.
func luminance(hex string) float64 {
	channel := func(at int) float64 {
		value, err := strconv.ParseInt(hex[at:at+2], 16, 32)
		if err != nil {
			panic("not a #rrggbb colour: " + hex)
		}
		part := float64(value) / 255
		if part <= 0.03928 {
			return part / 12.92
		}
		return math.Pow((part+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(1) + 0.7152*channel(3) + 0.0722*channel(5)
}

// Every colour in the palette clears its floor on the background it is read on:
// AA for anything with words in it, and the chrome floor for the frame.
func TestPaletteClearsWCAGAA(t *testing.T) {
	if len(palette) == 0 {
		t.Fatal("the palette is empty; the contrast floor guards nothing")
	}

	for _, colour := range palette {
		ratio := contrast(colour.hex, colour.on)
		if ratio < colour.floor {
			t.Errorf("%s (%s on %s) has a contrast ratio of %.2f:1, want at least %.1f:1",
				colour.name, colour.hex, colour.on, ratio, colour.floor)
		}
		t.Logf("%-12s %s on %s  %5.2f:1  (floor %.1f)", colour.name, colour.hex, colour.on, ratio, colour.floor)
	}
}

// The palette keeps its range: body text is the brightest thing on screen and
// the dim tone stays dim. A palette that passed AA by making everything white
// would be legible and unreadable at once — the hierarchy is what says which
// facts matter (docs/specs/TUI.md).
func TestPaletteKeepsItsHierarchy(t *testing.T) {
	body := contrast(inkHex, terminalBG)
	if body < 10 {
		t.Errorf("body text is %.2f:1 against the terminal, want a clear 10:1 or better", body)
	}

	for _, quiet := range []struct {
		name string
		hex  string
		on   string
	}{
		{"dim", dimHex, terminalBG},
		{"band dim", bandDimHex, bandHex},
	} {
		ratio := contrast(quiet.hex, quiet.on)
		if ratio > 7 {
			t.Errorf("the %s tone is %.2f:1 on %s, which is not dim any more — mute it by hue, not by darkness",
				quiet.name, ratio, quiet.on)
		}
	}

	// The band is a tint of the background, not a block of colour: a row under
	// the cursor has to read as the same list, lit.
	if tint := contrast(bandHex, terminalBG); tint > 2 {
		t.Errorf("the cursor band is %.2f:1 against the terminal, want a tint rather than a slab", tint)
	}
}

// Every colour is spelled the way the luminance arithmetic reads it, which is
// also the only spelling lipgloss takes as truecolor.
func TestPaletteIsHexTruecolor(t *testing.T) {
	for _, colour := range palette {
		for _, hex := range []string{colour.hex, colour.on} {
			if len(hex) != 7 || !strings.HasPrefix(hex, "#") {
				t.Errorf("%s carries %q, want a #rrggbb colour", colour.name, hex)
			}
		}
	}
}

// The styles are built from the palette rather than from colours of their own:
// what the contrast test checks is what the screens draw with.
func TestStylesDrawFromThePalette(t *testing.T) {
	declared := map[string]bool{}
	for _, colour := range palette {
		declared[colour.hex] = true
	}

	for name, style := range map[string]lipgloss.Style{
		"frame":   frameStyle,
		"ready":   readyStyle,
		"title":   titleStyle,
		"fact":    factStyle,
		"label":   labelStyle,
		"key":     keyStyle,
		"hint":    hintStyle,
		"quiet":   quietStyle,
		"notice":  noticeStyle,
		"size":    sizeStyle,
		"alarm":   alarmStyle,
		"broken":  brokenStyle,
		"heading": headingStyle,

		"band name":   bandNameStyle,
		"band ready":  bandReadyStyle,
		"band fact":   bandFactStyle,
		"band quiet":  bandQuietStyle,
		"band notice": bandNoticeStyle,
		"band alarm":  bandAlarmStyle,

		"llama": backendTone("llama"),
		"mlx":   backendTone("mlx"),
	} {
		if !declared[hexOf(style.GetForeground())] {
			t.Errorf("the %s style draws in %v, which is not in the palette", name, style.GetForeground())
		}
	}
}

// assertBanded checks that the row under the cursor is drawn as a band across
// the whole pane and the row beside it is not. The highlight is a background, so
// it has to reach both edges: one that stopped at the last word would read as a
// coloured word rather than as the row the keys act on.
func assertBanded(t *testing.T, cursorRow, otherRow string, width int) {
	t.Helper()

	if !strings.Contains(cursorRow, opener(bandStyle)) {
		t.Errorf("the cursor's row carries no background: %q", cursorRow)
	}
	if !strings.Contains(cursorRow, opener(bandNameStyle)) {
		t.Errorf("the cursor's row does not name itself in the band's accent: %q", cursorRow)
	}
	if got := lipgloss.Width(cursorRow); got != width {
		t.Errorf("the cursor's row is %d cells wide, want the pane's %d so the band spans it", got, width)
	}
	if strings.Contains(otherRow, opener(bandStyle)) {
		t.Errorf("a row the cursor is not on carries the band: %q", otherRow)
	}
}

// hexOf spells a rendered colour back the way the palette declares it.
func hexOf(colour color.Color) string {
	r, g, b, _ := colour.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
