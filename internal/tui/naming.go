package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// This file is the one place cria takes typed text. A group is named on the
// notice line — the row under the status box is reserved whether or not it has
// anything to say, so a name costs no layout shift and the list it is about
// stays on screen while it is typed (docs/specs/TUI.md).
//
// It is a name field, not an editor: characters go on the end, backspace takes
// the last one off, ⏎ confirms and esc cancels. There is no cursor to move and
// no selection to make — a group name is short enough that retyping it costs
// less than any of that would cost to learn.
//
// The mode collects a name and says whether it can be used. What the name is
// *for* belongs to the key that asked for it, which hands in what to do with the
// answer: that is what keeps "create a group and file this entry into it" and
// "rename this group" out of here, and leaves this file holding one question —
// what may a group be called.

const (
	// nameCursor is where the next character will land, drawn after the text.
	nameCursor = "▌"

	// nameRefusal holds a refused name apart from the reason it was refused, so
	// the two read as two things on the one line they share.
	nameRefusal = " — "
)

// nameCommit is the arming key's half of the mode: a name has been typed and
// accepted, and this is what it was wanted for. It takes the frame rather than
// closing over one — the model is a value, and the frame this runs on is the
// frame as it stands when ⏎ is pressed, not the one the key was pressed from.
type nameCommit func(m model, name string) (tea.Model, tea.Cmd)

// naming is a name being typed: what the line asks for, what has been typed so
// far, the name the input opened on, why the last ⏎ was refused, and who the
// finished name goes to.
type naming struct {
	prompt  string
	text    string
	opened  string
	refusal string
	commit  nameCommit
}

// askName opens the input. The prompt is what the line asks in the words the
// user pressed a key for — "new group", "rename group" — and prefill is what the
// input starts on, which is the current name when the key is a rename and
// nothing when it is a creation.
func (m model) askName(prompt, prefill string, commit nameCommit) model {
	m.naming = &naming{prompt: prompt, text: prefill, opened: prefill, commit: commit}
	return m.syncEscScope()
}

// pressInNaming is the keyboard while a name is being typed, and it takes every
// key: a letter is a letter here, so q types rather than quits and nothing
// underneath acts. ctrl+c is the exception every terminal program keeps.
func (m model) pressInNaming(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quitTyping):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.cancelName):
		return m.leaveNaming(), nil
	case key.Matches(pressed, m.keys.confirmName):
		return m.nameConfirmed()
	case key.Matches(pressed, m.keys.eraseName):
		return m.editName(withoutLastRune(m.naming.text)), nil
	case pressed.Text != "":
		// Text is what the terminal says the key printed, so it is empty for
		// every key that printed nothing — an arrow, a function key, a modifier
		// held on its own — and those leave the name alone.
		return m.editName(m.naming.text + pressed.Text), nil
	}
	return m, nil
}

// nameConfirmed is ⏎: the name as it would be used — trimmed, because a name is
// typed and a space at either end of one is a slip rather than a choice —
// checked against the rules, then handed to the key that asked for it.
//
// A refused name keeps the mode up with the text exactly as it was typed. The
// correction is a backspace away, and clearing the field would make the user
// retype the part that was never the problem.
func (m model) nameConfirmed() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.naming.text)
	if refusal := m.refuseName(name); refusal != "" {
		return m.refuseTypedName(refusal), nil
	}

	commit := m.naming.commit
	return commit(m.leaveNaming(), name)
}

// refuseName is why a name cannot be used, and the empty string when it can. The
// rules are the group list's own: a name is how a group is shown and picked, so
// an unnamed group names nothing, two groups sharing a name leave the list
// ambiguous, and the tail's own word already means "in no group at all"
// (docs/specs/TUI.md).
//
// A name is never a collision with itself. An input that opened on a name is
// that name's editor, and confirming it unchanged is a no-op rather than a
// refusal.
func (m model) refuseName(name string) string {
	switch {
	case name == "":
		return "a group needs a name"
	case name == ungroupedHeading:
		return fmt.Sprintf("%q is the list's own tail, not a group", ungroupedHeading)
	}

	for _, group := range m.prefs.Groups {
		if group.Name == name && name != m.naming.opened {
			return fmt.Sprintf("there is already a group named %q", name)
		}
	}
	return ""
}

// editName is the input after a keystroke changed the text. The refusal goes
// with the change: it answered a ⏎ about a name that no longer reads that way.
//
// The value is copied rather than edited in place, as the pick's cursor is: the
// frame travels by value, and a mode edited through its pointer would change the
// frame a message is still being handled against.
func (m model) editName(text string) model {
	typing := *m.naming
	typing.text, typing.refusal = text, ""
	m.naming = &typing
	return m
}

// refuseTypedName puts the reason beside the name and leaves the mode up.
func (m model) refuseTypedName(refusal string) model {
	typing := *m.naming
	typing.refusal = refusal
	m.naming = &typing
	return m
}

// leaveNaming drops the mode, confirmed or cancelled. The notice line goes back
// to whatever it was saying before: the input never wrote there, it stood in
// front of it, so a refusal or an error the line was already carrying is still
// the truth once the name is gone.
func (m model) leaveNaming() model {
	m.naming = nil
	return m.syncEscScope()
}

// line is the input as the notice row draws it: what is being named, what has
// been typed, and the block where the next character lands. A refused ⏎ adds its
// reason after the name rather than in place of it — the name has to stay
// readable to be corrected.
func (n *naming) line() string {
	typed := labelStyle.Render(n.prompt+": ") + factStyle.Render(n.text) + noticeStyle.Render(nameCursor)
	if n.refusal == "" {
		return typed
	}
	return typed + alarmStyle.Render(nameRefusal+n.refusal)
}

// withoutLastRune is what backspace leaves behind: one character off the end,
// counted in runes rather than bytes, so an accented letter goes in one press
// instead of leaving half of itself on the line.
func withoutLastRune(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	return string(runes[:len(runes)-1])
}
