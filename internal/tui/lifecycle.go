package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"cria/internal/config"
	"cria/internal/serve"
	"cria/internal/tools"
)

// This file is where a keypress becomes something happening on the host. Every
// action here runs as a command rather than inside Update: a start execs the
// tool check and asks `lsof` who holds the port, a stop blocks for as long as a
// SIGTERM grace period lasts (docs/specs/SERVE.md), and the frame has to keep
// redrawing while either does.
//
// The frame decides nothing about lifecycle. The order a start asks its
// questions in, what refuses it and what a refusal says are serve's and the
// CLI's alike; what is here is which key asks, and how the answer is drawn.

// The answers an action comes back with.
type (
	// startedMsg is one start attempt: the server it launched, the refusal that
	// stopped it, or the foreign processes holding its port — the one refusal
	// the user can act on from here.
	startedMsg struct {
		entry   config.Entry
		record  serve.Record
		holders []serve.Holder
		err     error
	}
	// actedMsg is what a stop, a kill or a dismiss had to report — which is
	// nothing at all when it worked: the status box shows the result on the
	// next refresh, and the alert line is for what the box cannot show (tui.go).
	// A failure is exactly that, so it travels here.
	actedMsg struct {
		text string
		bad  bool
	}
	// holderKilledMsg is the port asked again after a foreign holder was killed.
	holderKilledMsg struct {
		entry config.Entry
		use   serve.PortUse
		err   error
	}
)

// modal is a refusal the user can answer: the processes holding the port an
// entry asked for, with the kill cria offers here and nowhere else
// (docs/specs/SERVE.md, docs/specs/CLI.md).
type modal struct {
	entry   config.Entry
	holders []serve.Holder
	note    string // what the last answer in the modal did; empty until one has
}

// startSelected is ⏎: start the highlighted entry.
//
// A row with nothing to launch — an entry file cria refused — never reaches
// here: the key is not offered for one, and a key the bar does not draw does
// nothing when pressed. What that row needs is in the detail pane beside it,
// which is the file, the offending key and the fix (docs/specs/CONFIG.md).
func (m model) startSelected() (tea.Model, tea.Cmd) {
	selected, ok := m.selectedRow()
	if !ok || selected.broken != nil {
		return m, nil
	}

	m.alert = alert{text: "starting " + selected.entry.ID + "…"}
	return m, m.launch(selected.entry)
}

// launch is the start sequence as a command. The sequence itself is
// docs/specs/SERVE.md's, in the order `cria start` asks it: the tool gate before
// the port check — a host without llama-server has to hear about llama-server,
// not about a busy port — and the port before anything is spawned.
func (m model) launch(entry config.Entry) tea.Cmd {
	settings := m.settings()
	check, servers := m.host.tools, m.host.servers
	return func() tea.Msg { return startEntry(entry, settings, check, servers) }
}

// startEntry runs that sequence off the UI thread.
func startEntry(entry config.Entry, settings config.Settings, check func(config.Settings) tools.Report, servers servers) startedMsg {
	// Already running comes first (docs/specs/SERVE.md): it costs a record
	// read, where the gates behind it exec programs — pressing ⏎ on the server
	// that is already up must answer instantly and run nothing.
	if held, running, err := servers.Running(entry.ID); err != nil {
		return startedMsg{entry: entry, err: err}
	} else if running {
		return startedMsg{entry: entry, err: managedRefusal(entry, held)}
	}

	report := check(settings)
	if _, err := serve.LaunchTool(entry.Backend, report); err != nil {
		return startedMsg{entry: entry, err: err}
	}

	use, err := servers.PortUse(entry.Port)
	if err != nil {
		return startedMsg{entry: entry, err: err}
	}
	if held := use.Managed; held != nil {
		return startedMsg{entry: entry, err: managedRefusal(entry, *held)}
	}
	if len(use.Holders) > 0 {
		return startedMsg{entry: entry, holders: use.Holders}
	}

	record, err := servers.Start(entry, report)
	if err != nil {
		return startedMsg{entry: entry, err: err}
	}
	return startedMsg{entry: entry, record: record}
}

// managedRefusal is a port held by a server cria started. It needs no modal: the
// fix is one keypress on something already on screen — stop that entry — and the
// status box is where it is (docs/specs/SERVE.md).
func managedRefusal(entry config.Entry, held serve.Server) error {
	if held.EntryID == entry.ID {
		return fmt.Errorf("%s is already running as pid %d on port %d; stop it first",
			entry.ID, held.PID, held.Port)
	}
	return fmt.Errorf("port %d is serving %s (pid %d); stop %s first",
		entry.Port, held.EntryID, held.PID, held.EntryID)
}

// started takes a start's answer. A launched server is recorded as the
// last-started entry, which is what the status box falls back to when nothing is
// running and what restart-last acts on across sessions (docs/specs/TUI.md).
//
// A preferences write that fails does not unstart the server: what was lost is
// cria's memory of it, and that is what the alert says.
func (m model) started(msg startedMsg) model {
	if len(msg.holders) > 0 {
		m.modal = &modal{entry: msg.entry, holders: msg.holders}
		m.alert = alert{}
		return m
	}
	if msg.err != nil {
		m.alert = alert{text: msg.err.Error(), bad: true}
		return m
	}

	// The server is up and the status box says so on the next tick; the line
	// that reported the start would only repeat it (tui.go).
	m.prefs.LastStarted = msg.record.EntryID
	m.alert = alert{}
	if err := savePrefs(m.root, m.prefs); err != nil {
		m.alert = alert{text: err.Error(), bad: true}
	}
	m.keys.retarget(targetOf(m.listing, m.prefs))
	return m
}

// stopShownServer is s: stop what the status box shows. Stop is global — it acts
// on the running server whatever the list selection is (docs/specs/TUI.md).
func (m model) stopShownServer() (tea.Model, tea.Cmd) {
	record, live := m.liveRecord()
	if !live {
		return m, nil
	}

	// Stop blocks for as long as the grace period plus the kill confirmation
	// (docs/specs/SERVE.md), so the line says what cria is doing while it does
	// it — and the ticker keeps redrawing behind it.
	m.alert = alert{text: "stopping " + record.EntryID + "…"}
	servers := m.host.servers
	return m, func() tea.Msg {
		if err := servers.Stop(record); err != nil {
			return actedMsg{text: err.Error(), bad: true}
		}
		return actedMsg{}
	}
}

// killShownServer is K: the same stop without the grace period, for a server
// that is wedged or that the user does not want to wait for
// (docs/specs/SERVE.md).
func (m model) killShownServer() (tea.Model, tea.Cmd) {
	record, live := m.liveRecord()
	if !live {
		return m, nil
	}

	m.alert = alert{text: "killing " + record.EntryID + "…"}
	servers := m.host.servers
	return m, func() tea.Msg {
		if err := servers.Kill(record); err != nil {
			return actedMsg{text: err.Error(), bad: true}
		}
		return actedMsg{}
	}
}

// dismissShownRecord is d: clear the crash report the box is showing, once the
// user is done reading it (docs/specs/SERVE.md).
func (m model) dismissShownRecord() (tea.Model, tea.Cmd) {
	record, exited := m.exitedRecord()
	if !exited {
		return m, nil
	}

	servers := m.host.servers
	return m, func() tea.Msg {
		if err := servers.Dismiss(record); err != nil {
			return actedMsg{text: err.Error(), bad: true}
		}
		return actedMsg{}
	}
}

// restartShownEntry is r: stop what the box shows and start it again. It works
// from the stopped state too — the box names the last-started entry there, which
// is the whole reason that fallback exists (docs/specs/TUI.md) — and it is the
// one-keypress swap-back after a stop.
func (m model) restartShownEntry() (tea.Model, tea.Cmd) {
	id, shown := m.shownEntryID()
	if !shown {
		return m, nil
	}
	entry, found := m.entryNamed(id)
	if !found {
		m.alert = alert{text: fmt.Sprintf("%s is not an entry cria can read any more; nothing to restart", id), bad: true}
		return m, nil
	}

	record, live := m.liveRecord()
	m.alert = alert{text: "restarting " + id + "…"}
	settings := m.settings()
	check, servers := m.host.tools, m.host.servers
	return m, func() tea.Msg {
		if live {
			if err := servers.Stop(record); err != nil {
				return startedMsg{entry: entry, err: err}
			}
		}
		return startEntry(entry, settings, check, servers)
	}
}

// pressInModal is the keyboard while a refusal is up: kill what holds the port,
// or leave it alone. Nothing else acts — a start refused for a busy port must
// not become a stop of something else because a key underneath was still live.
func (m model) pressInModal(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.leaveModal):
		m.modal = nil
		return m, nil
	case key.Matches(pressed, m.keys.killHolder):
		return m.killHolders()
	}
	return m, nil
}

// killHolders is the kill the modal offers: every process holding the port, then
// the port asked again. The re-check is what clears the modal — cria does not
// start the entry off the back of a kill, so the user presses ⏎ again once the
// port is free, deliberately (docs/specs/SERVE.md).
func (m model) killHolders() (tea.Model, tea.Cmd) {
	held := *m.modal
	killing := held
	killing.note = "killing…"
	m.modal = &killing

	servers := m.host.servers
	return m, func() tea.Msg {
		for _, holder := range held.holders {
			if err := servers.KillHolder(holder); err != nil {
				return holderKilledMsg{entry: held.entry, err: err}
			}
		}
		use, err := servers.PortUse(held.entry.Port)
		return holderKilledMsg{entry: held.entry, use: use, err: err}
	}
}

// holderKilled takes the answer: a port that is free closes the modal and says
// so, and a port something still holds keeps it up with whoever holds it now.
func (m model) holderKilled(msg holderKilledMsg) model {
	if msg.err != nil {
		if m.modal != nil {
			m.modal.note = msg.err.Error()
		}
		m.alert = alert{text: msg.err.Error(), bad: true}
		return m
	}
	if msg.use.Free() {
		m.modal = nil
		m.alert = alert{text: fmt.Sprintf("port %d is free; ⏎ starts %s", msg.entry.Port, msg.entry.ID)}
		return m
	}

	m.modal = &modal{entry: msg.entry, holders: msg.use.Holders, note: "still held"}
	if held := msg.use.Managed; held != nil {
		m.modal = nil
		m.alert = alert{text: managedRefusal(msg.entry, *held).Error(), bad: true}
	}
	return m
}

// panel draws the refusal: which port, who holds it, what each one is running
// and where — the facts docs/specs/SERVE.md requires of this refusal — and the
// two answers to it.
func (h modal) panel(width, rows int) string {
	lines := []string{alarmStyle.Render(fmt.Sprintf(
		"port %d is held by a process cria did not start", h.entry.Port))}

	inner := width - 4
	for _, holder := range h.holders {
		lines = append(lines, detailField("pid", strconv.Itoa(holder.PID), inner, factStyle)...)
		lines = append(lines, detailField("command", orUnreadable(holder.Command), inner, factStyle)...)
		lines = append(lines, detailField("cwd", orUnreadable(holder.WorkingDir), inner, quietStyle)...)
	}
	lines = append(lines, "", quietStyle.Render(fmt.Sprintf(
		"k kills it and asks the port again; esc leaves it alone. %s keeps its port either way.", h.entry.ID)))
	if h.note != "" {
		lines = append(lines, noticeStyle.Render(h.note))
	}
	return pane(paneTitle(modalScope), width, sizeLines(lines, rows-2))
}

// orUnreadable keeps the refusal readable when one of a holder's two details
// could not be read: the pid is the part that matters, and it is already there.
func orUnreadable(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(unreadable)"
	}
	return value
}

// liveRecord is the server the server keys act on: the first one the box shows
// that cria can still see. Several at once is entries declaring different ports
// (docs/cria.md, v1 surface), and the box lists them in entry order, so the
// first is the one being read at the top.
func (m model) liveRecord() (serve.Record, bool) {
	for _, status := range m.listing.Servers {
		if status.Phase != serve.PhaseExited {
			return status.Record, true
		}
	}
	return serve.Record{}, false
}

// exitedRecord is the crash report the box is showing, if it is showing one.
func (m model) exitedRecord() (serve.Record, bool) {
	for _, status := range m.listing.Servers {
		if status.Phase == serve.PhaseExited {
			return status.Record, true
		}
	}
	return serve.Record{}, false
}

// shownEntryID is the entry the status box names: what is running, else what
// crashed, else what was started last (docs/specs/TUI.md).
func (m model) shownEntryID() (string, bool) {
	if record, live := m.liveRecord(); live {
		return record.EntryID, true
	}
	if record, exited := m.exitedRecord(); exited {
		return record.EntryID, true
	}
	return m.prefs.LastStarted, m.prefs.LastStarted != ""
}

// settings is the tree-wide configuration an action resolves against — the tool
// overrides, in particular. A tree that has not been read yet has none, and the
// tool check falls back to PATH, which is what an unconfigured host uses anyway.
func (m model) settings() config.Settings {
	if m.tree == nil {
		return config.Settings{}
	}
	return m.tree.Settings
}
