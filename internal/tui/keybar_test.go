package tui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// escapes matches the colour sequences lipgloss writes around every styled
// fragment. The assertions in this package are about what a person reads, so
// they read the plain text out of a rendered frame.
var escapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plain is one rendered string with its colours taken off.
func plain(rendered string) string { return escapes.ReplaceAllString(rendered, "") }

// binding is one key as a test declares it.
func binding(keys, help, desc string, enabled bool) key.Binding {
	bound := key.NewBinding(key.WithKeys(keys), key.WithHelp(help, desc))
	bound.SetEnabled(enabled)
	return bound
}

// The bar is grouped by scope, and every scope carries its label: that grouping
// is what makes "what works right now" legible without a help screen
// (docs/specs/TUI.md).
func TestKeybarGroupsByScope(t *testing.T) {
	bar := plain(renderKeybar(120,
		keyGroup{label: selectionScope, bindings: []key.Binding{binding("enter", "↵", "start", true)}},
		keyGroup{label: serverScope, bindings: []key.Binding{binding("s", "s", "stop", true), binding("l", "l", "log", true)}},
		keyGroup{label: globalScope, bindings: []key.Binding{binding("q", "q", "quit", true)}},
	))

	want := "selection ↵ start   server s stop · l log   global q quit"
	if strings.TrimRight(bar, " ") != want {
		t.Errorf("the keybar reads\n  %q\nwant\n  %q", strings.TrimRight(bar, " "), want)
	}
}

// The key is the bright thing in the bar and what it does sits beside it in the
// dim tone: the reader is scanning for a key, not for a sentence.
func TestKeybarColoursTheKeysOnly(t *testing.T) {
	bar := renderKeybar(120, keyGroup{label: serverScope, bindings: []key.Binding{binding("s", "s", "stop", true)}})

	if !strings.Contains(bar, keyStyle.Render("s")) {
		t.Errorf("the bar does not draw its key in the key colour: %q", bar)
	}
	if !strings.Contains(bar, hintStyle.Render("stop")) {
		t.Errorf("the bar does not draw what the key does in the hint tone: %q", bar)
	}
	if !strings.Contains(bar, quietStyle.Render(serverScope)) {
		t.Errorf("the bar does not draw its scope label quietly: %q", bar)
	}
	if keyStyle.GetForeground() == hintStyle.GetForeground() {
		t.Error("the key and what it does are drawn in one colour; the key is what the reader is looking for")
	}
}

// A key that does not apply right now is not drawn — pressing it does nothing,
// and a bar that offered it would be making a promise cria does not keep.
func TestKeybarHidesKeysThatDoNotApply(t *testing.T) {
	bar := plain(renderKeybar(120, keyGroup{label: serverScope, bindings: []key.Binding{
		binding("s", "s", "stop", false),
		binding("r", "r", "restart", true),
		binding("d", "d", "dismiss", false),
	}}))

	if strings.Contains(bar, "stop") || strings.Contains(bar, "dismiss") {
		t.Errorf("the keybar offers keys that do not apply: %q", bar)
	}
	if !strings.Contains(bar, "r restart") {
		t.Errorf("the keybar reads %q, want the key that does apply", bar)
	}
}

// A scope with nothing enabled leaves no label behind: an empty "selection"
// would read as a scope with keys the user cannot see.
func TestKeybarDropsEmptyScopes(t *testing.T) {
	cases := map[string][]key.Binding{
		"no bindings at all":  nil,
		"every binding unfit": {binding("s", "s", "stop", false)},
	}

	for name, bindings := range cases {
		t.Run(name, func(t *testing.T) {
			bar := plain(renderKeybar(120,
				keyGroup{label: selectionScope, bindings: bindings},
				keyGroup{label: globalScope, bindings: []key.Binding{binding("q", "q", "quit", true)}},
			))
			if strings.Contains(bar, selectionScope) {
				t.Errorf("the keybar reads %q, want no empty scope", bar)
			}
			if !strings.HasPrefix(bar, "global q quit") {
				t.Errorf("the keybar reads %q, want it to start with the scope that has keys", bar)
			}
		})
	}
}

// The bar is one line whatever the terminal: a narrow one loses the far end of
// the bar rather than wrapping the frame.
func TestKeybarIsOneLine(t *testing.T) {
	bar := renderKeybar(20, keyGroup{label: globalScope, bindings: []key.Binding{
		binding("tab", "⇥", "backend", true),
		binding("v", "v", "view", true),
		binding("t", "t", "tools", true),
		binding("q", "q", "quit", true),
	}})

	if strings.Contains(bar, "\n") {
		t.Errorf("the keybar wrapped: %q", bar)
	}
	if width := lipgloss.Width(bar); width != 20 {
		t.Errorf("the keybar is %d cells wide, want 20", width)
	}
}
