package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
)

// The three scopes every keybind belongs to (docs/specs/TUI.md). Selection keys
// read the highlighted item, server keys act on the running server from
// anywhere, and global keys navigate. The grouping is what makes "what works
// right now" legible without a help screen, so the labels are part of the bar
// rather than decoration on it.
// A screen that takes the keyboard names its own scope in place of the first
// two: a refusal to answer, or a log to leave. The bar always says what works
// right now, and while one of those is up, what works is theirs.
const (
	selectionScope = "selection"
	serverScope    = "server"
	globalScope    = "global"
	modalScope     = "held port"
	deleteScope    = "delete"
	toolsScope     = "tools"
	logScope       = "log"
	benchScope     = "bench"
	namingScope    = "name"
	moveScope      = "move where"
)

// How the bar reads: keys within a scope are separated by a thin dot, scopes by
// plain space wide enough to see the grouping.
const (
	keySeparator   = " · "
	groupSeparator = "   "
)

// keyGroup is one scope's keys as the bar draws them.
type keyGroup struct {
	label    string
	bindings []key.Binding
}

// renderKeybar draws the one bottom bar. A key that does not apply right now is
// not drawn — a disabled binding does nothing when pressed either, so the bar
// showing it would be a promise cria does not keep — and a scope with nothing
// enabled leaves no label behind.
func renderKeybar(width int, groups ...keyGroup) string {
	var scopes []string
	for _, group := range groups {
		if hints := group.hints(); hints != "" {
			scopes = append(scopes, hints)
		}
	}
	return fit(strings.Join(scopes, groupSeparator), width)
}

// hints is one scope: its label, then every key that applies. It is empty when
// none does.
//
// The key itself is the only bright thing in the bar — it is what the reader is
// looking for — and what it does sits beside it in the dim tone. The scope label
// is quieter still: it groups the keys rather than naming an action.
func (g keyGroup) hints() string {
	var keys []string
	for _, binding := range g.bindings {
		if !binding.Enabled() {
			continue
		}
		help := binding.Help()
		keys = append(keys, keyStyle.Render(help.Key)+" "+hintStyle.Render(help.Desc))
	}
	if len(keys) == 0 {
		return ""
	}
	return quietStyle.Render(g.label) + " " + strings.Join(keys, hintStyle.Render(keySeparator))
}
