// Package tui is cria's program frame: the screen bare `cria` opens, and the
// three things that are true in every view of it — the status box at the top,
// the grouped keybar at the bottom, and the preferences the two of them
// remember between launches (docs/specs/TUI.md).
//
// The frame holds no serving state of its own. Everything the status box shows
// is one observation of what serve reports, taken on a ticker and thrown away
// on the next one; the only state cria writes from here is the UI's own memory,
// and it goes to the state directory rather than the config tree.
//
// The screens themselves — the entry list, the cache table, the tools pane —
// hang on this frame and are added around it. What lives here is only what does
// not belong to any one of them.
package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/procs"
	"cria/internal/serve"
)

// refreshInterval is how often the status box re-reads the world. An
// observation costs one health probe and one `ps` per record (docs/specs/SERVE.md),
// and two seconds is fast enough that a phase change is seen as it happens
// while leaving the host alone in between. Nothing pushes here: a detached
// server has no channel back to the TUI, so observation on a ticker is the
// correct mechanism rather than a substitute for one (CODING-RULES §6).
const refreshInterval = 2 * time.Second

// snapshotter is the part of serve.Manager the frame drives: one observation of
// every record cria holds. Naming it on the consumer's side is what lets the
// whole frame — every display state, every contextual key — be exercised with
// no state directory, no process table and no server on the host.
type snapshotter interface {
	Snapshots() (serve.StatusListing, error)
}

// view names the screen the frame is routed to. Two of them in v1
// (docs/cria.md, v1 surface), switched by one global key.
type view int

const (
	viewServe view = iota
	viewCache
)

// other is where the view key goes from here.
func (v view) other() view {
	if v == viewServe {
		return viewCache
	}
	return viewServe
}

// alert is the one line under the status box that says what just happened: what
// a keypress did, or what it could not do. The next keypress replaces it — it
// reports an action, and only the newest action is still being asked about.
type alert struct {
	text string
	bad  bool // cria could not do the thing, rather than merely did it
}

// pending is what a key whose action is not built yet answers with. The keybar
// still offers it: the bar is the map of the frame, and a key that vanishes
// until its screen exists would make the map change under the user.
func pending(what string) alert { return alert{text: what + " is not wired yet"} }

// The messages the frame runs on: the refresh tick, and one observation coming
// back off the UI thread.
type (
	tickMsg     time.Time
	snapshotMsg struct {
		listing serve.StatusListing
		err     error
	}
)

// model is the whole program state.
type model struct {
	servers snapshotter
	root    string // the state root, where the preferences file is written back

	prefs prefs
	view  view

	listing serve.StatusListing
	failure error // the last refresh that failed, held until one succeeds
	alert   alert

	width  int
	height int

	keys keymap
	bar  progress.Model

	// The refresh interval, held rather than read from the constant so a test
	// can drive a whole tick without waiting one out.
	interval time.Duration
}

// Run opens the TUI on this host: the real state directory, the real process
// table, and whatever preferences a previous session left behind.
func Run() error {
	root, err := serve.Root()
	if err != nil {
		return err
	}

	// Preferences that could not be read do not stop the program — they are
	// cria's own memory, not its instructions. The frame starts on the defaults
	// and carries the reason on screen (loadPrefs).
	saved, prefsErr := loadPrefs(root)

	program := tea.NewProgram(newModel(serve.New(root, procs.System{}), root, saved, prefsErr))
	_, err = program.Run()
	return err
}

// newModel builds the frame around its one subsystem and its preferences.
func newModel(servers snapshotter, root string, saved prefs, prefsErr error) model {
	frame := model{
		servers:  servers,
		root:     root,
		prefs:    saved,
		width:    defaultWidth,
		keys:     newKeymap(),
		bar:      newProgressBar(),
		interval: refreshInterval,
	}
	if prefsErr != nil {
		frame.alert = alert{text: prefsErr.Error(), bad: true}
	}
	frame.keys.retarget(targetOf(frame.listing, frame.prefs))
	return frame
}

// Init starts the refresh loop: one observation now, so the box is true before
// the first tick, and the ticker that keeps it true after.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.refresh, m.scheduleRefresh())
}

// Update is the whole frame's response to the world.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.refresh, m.scheduleRefresh())
	case snapshotMsg:
		return m.observed(msg), nil
	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

// observed takes one refresh's answer.
//
// A failed observation keeps the last good listing on screen and states what
// could not be read: an empty box would say "nothing is running", which is a
// plausible-looking lie about a host cria could not ask (CODING-RULES §4). The
// failure is held until an observation succeeds — no timer takes it away, and
// every failing tick keeps it there — because it describes the box's own truth,
// and the next successful reading is the only thing that supersedes it.
func (m model) observed(msg snapshotMsg) model {
	if msg.err != nil {
		m.failure = msg.err
		return m
	}
	m.listing, m.failure = msg.listing, nil
	m.keys.retarget(targetOf(m.listing, m.prefs))
	return m
}

// press routes one keystroke. Only the frame's own keys are bound here; a key
// the screens will claim is not caught, so it reaches them unchanged when they
// arrive.
func (m model) press(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.backend):
		return m.switchBackend(), nil
	case key.Matches(pressed, m.keys.view):
		m.view = m.view.other()
		m.alert = alert{}
		return m, nil
	case key.Matches(pressed, m.keys.tools):
		m.alert = pending("the tools pane")
	case key.Matches(pressed, m.keys.stop):
		m.alert = pending("stop")
	case key.Matches(pressed, m.keys.log):
		m.alert = pending("the log tail")
	case key.Matches(pressed, m.keys.restart):
		m.alert = pending("restart")
	case key.Matches(pressed, m.keys.dismiss):
		m.alert = pending("dismiss")
	}
	return m, nil
}

// switchBackend flips the active backend and records it: the choice is sticky
// across launches, which is the whole reason it is written down
// (docs/specs/TUI.md).
//
// A write that fails still switches the session's backend. The user asked for
// this backend and cria can show it; what was lost is only the memory of it, and
// that is what the alert says.
func (m model) switchBackend() model {
	m.prefs.Backend = m.prefs.other()
	m.alert = alert{text: "backend " + string(m.prefs.Backend)}
	if err := savePrefs(m.root, m.prefs); err != nil {
		m.alert = alert{text: err.Error(), bad: true}
	}
	return m
}

// refresh is one observation, taken as a command so the probes and the `ps`
// calls it costs run off the UI thread.
func (m model) refresh() tea.Msg {
	listing, err := m.servers.Snapshots()
	return snapshotMsg{listing: listing, err: err}
}

// scheduleRefresh arms the next tick.
func (m model) scheduleRefresh() tea.Cmd {
	return tea.Tick(m.interval, func(at time.Time) tea.Msg { return tickMsg(at) })
}

// View draws the frame. The alternate screen is the whole point of a program
// that is entered and left: the terminal it was opened from comes back
// untouched.
func (m model) View() tea.View {
	drawn := tea.NewView(m.frame())
	drawn.AltScreen = true
	return drawn
}

// frame composes the screen: the status box, whatever the last refresh and the
// last keypress had to say, the current view, and the keybar.
//
// A terminal too short for all of that loses the view pane. The status box and
// the keybar are true everywhere — one says what is running, the other says what
// works right now — and a frame missing either is unreadable rather than merely
// smaller.
func (m model) frame() string {
	width := m.frameWidth()
	top := append([]string{pane(statusTitle, width, statusLines(m.listing, m.prefs, m.bar))}, m.notes(width)...)
	screen, bar := m.screen(width), renderKeybar(width, m.groups()...)

	whole := append(append([]string{}, top...), screen, bar)
	if m.height <= 0 || lipgloss.Height(strings.Join(whole, "\n")) <= m.height {
		return strings.Join(whole, "\n")
	}
	return strings.Join(append(top, bar), "\n")
}

// notes are the lines between the box and the view: what could not be observed,
// and what the last keypress did. Both are one line and neither is a box — they
// are commentary on the box above them.
func (m model) notes(width int) []string {
	var notes []string
	if m.failure != nil {
		notes = append(notes, fit(alarmStyle.Render("cannot read the servers: "+m.failure.Error()), width))
	}
	if m.alert.text != "" {
		style := noticeStyle
		if m.alert.bad {
			style = alarmStyle
		}
		notes = append(notes, fit(style.Render(m.alert.text), width))
	}
	return notes
}

// screen is the view the frame is routed to. Both are placeholders: the entry
// list and the cache table are their own steps, and this is the frame they hang
// on. The serve pane carries the active backend in its title, because the list
// it will hold is that backend's — one backend at a time, never a mixed list
// (docs/specs/TUI.md).
func (m model) screen(width int) string {
	if m.view == viewCache {
		return pane("cache", width, []string{quietStyle.Render("under construction — the model table and its surgery")})
	}
	return pane("serve · "+string(m.prefs.Backend), width,
		[]string{quietStyle.Render("under construction — the entry list, the detail pane and the log tail")})
}

// groups is the keybar's content: the three scopes, in the order they are read
// — what the highlighted item does, what the running server does, where to go
// (docs/specs/TUI.md).
func (m model) groups() []keyGroup {
	return []keyGroup{
		{label: selectionScope, bindings: m.selectionKeys()},
		{label: serverScope, bindings: []key.Binding{m.keys.stop, m.keys.log, m.keys.restart, m.keys.dismiss}},
		{label: globalScope, bindings: []key.Binding{m.keys.backend, m.keys.view, m.keys.tools, m.keys.quit}},
	}
}

// selectionKeys are the highlighted item's, and they belong to the screen that
// holds the selection. Neither screen has one yet, so the scope draws nothing.
func (m model) selectionKeys() []key.Binding { return nil }

// frameWidth is what the frame draws at: the terminal's width, once it has said
// what that is, and never narrower than a box can be drawn in.
func (m model) frameWidth() int {
	switch {
	case m.width <= 0:
		return defaultWidth
	case m.width < minWidth:
		return minWidth
	}
	return m.width
}

// keymap is every key the frame binds. The screens bind their own selection
// keys; these are the ones that work from anywhere.
type keymap struct {
	stop    key.Binding
	log     key.Binding
	restart key.Binding
	dismiss key.Binding

	backend key.Binding
	view    key.Binding
	tools   key.Binding
	quit    key.Binding
}

// newKeymap is the frame's keys as they are bound and as the bar spells them.
func newKeymap() keymap {
	return keymap{
		stop:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stop")),
		log:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
		restart: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
		dismiss: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dismiss")),
		backend: key.NewBinding(key.WithKeys("tab"), key.WithHelp("⇥", "backend")),
		view:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view")),
		tools:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tools")),
		quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// retarget matches the server keys to what the status box is showing. A
// disabled binding is neither drawn nor fired, so the bar and the keyboard say
// the same thing about what works right now.
//
// Restart is the one that survives an empty state directory: it acts on what
// the box shows, and the box shows the last-started entry when nothing is
// running (docs/specs/TUI.md).
func (k *keymap) retarget(target boxTarget) {
	k.stop.SetEnabled(target.live)
	k.log.SetEnabled(target.live || target.exited)
	k.restart.SetEnabled(target.shown)
	k.dismiss.SetEnabled(target.exited)
}
