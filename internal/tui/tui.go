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
// The screens hang on this frame: the serve view — the entry list, its detail
// pane and the log tail — and the cache view. What lives here is only what does
// not belong to any one of them.
package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/hubcache"
	"cria/internal/procs"
	"cria/internal/serve"
	"cria/internal/tools"
)

// refreshInterval is how often the frame re-reads the world. An observation
// costs one health probe and one `ps` per record (docs/specs/SERVE.md), plus one
// read of the config tree and — only while the entry list is on screen or a
// download is running — one cache walk, and two seconds is fast enough that a
// phase change is seen as it happens while leaving the host alone in between.
// Nothing pushes here: a detached server has no channel back to the TUI, so
// observation on a ticker is the correct mechanism rather than a substitute for
// one (CODING-RULES §6).
const refreshInterval = 2 * time.Second

// servers is the part of serve.Manager the frame drives: one observation of
// every record cria holds, the lifecycle actions the keys fire, and the port
// question a start has to ask first. Naming it on the consumer's side is what
// lets the whole frame — every display state, every contextual key, every
// refusal — be exercised with no state directory, no process table and no
// server on the host.
type servers interface {
	Snapshots() (serve.StatusListing, error)
	Start(entry config.Entry, report tools.Report) (serve.Record, error)
	Stop(record serve.Record) error
	Kill(record serve.Record) error
	Dismiss(record serve.Record) error
	PortUse(port int) (serve.PortUse, error)
	KillHolder(holder serve.Holder) error
}

// host is everything the frame reads this machine through: the lifecycle, the
// config tree it lists, the tool check that says what may be launched, and the
// hub cache that says what is already on disk. One value, wired in Run and
// replaced wholesale by the tests.
type host struct {
	servers servers
	entries func() (*config.Tree, error)
	tools   func(config.Settings) tools.Report
	cache   func() (*hubcache.Cache, error)
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

// The messages the frame runs on: the refresh tick, and every answer that comes
// back off the UI thread.
type (
	tickMsg     time.Time
	snapshotMsg struct {
		listing serve.StatusListing
		err     error
	}
	// entriesMsg is one read of the config tree, and — when the entry list is
	// what the user is looking at — the cache walk behind the cached dots. The
	// two travel together because the walk is asked entry by entry.
	entriesMsg struct {
		tree     *config.Tree
		err      error
		report   tools.Report
		reported bool // the tool check ran; it runs once, on the first tree read
		cached   map[string]bool
		walked   bool // the cache was read, so cached is the whole answer
		cacheErr error
	}
)

// model is the whole program state.
type model struct {
	host host
	root string // the state root, where the preferences file is written back

	prefs prefs
	view  view

	listing serve.StatusListing
	failure error // the last refresh that failed, held until one succeeds
	alert   alert

	// What the config tree declares, and what the host holds for it. Each is
	// held with the reason it could not be read, which stays on screen until a
	// read succeeds — for the same reason a failed observation does.
	tree     *config.Tree
	treeErr  error
	report   tools.Report
	reported bool
	cached   map[string]bool
	cacheErr error

	selected int    // the highlighted row of the active backend's list
	modal    *modal // the refusal a start came back with; nil when there is none
	log      logScreen

	width  int
	height int

	keys keymap
	bar  progress.Model

	// The refresh interval, held rather than read from the constant so a test
	// can drive a whole tick without waiting one out.
	interval time.Duration
}

// Run opens the TUI on this host: the real state directory, the real process
// table, the real config tree, and whatever preferences a previous session left
// behind.
func Run() error {
	root, err := serve.Root()
	if err != nil {
		return err
	}

	// Preferences that could not be read do not stop the program — they are
	// cria's own memory, not its instructions. The frame starts on the defaults
	// and carries the reason on screen (loadPrefs).
	saved, prefsErr := loadPrefs(root)

	program := tea.NewProgram(newModel(machine(root), root, saved, prefsErr))
	_, err = program.Run()
	return err
}

// machine wires the frame to this host. Each subsystem is reached through its
// own entry point and nothing else (CODING-RULES §7); composing the two-call
// lookups — where the config tree lives, where the hub cache lives — is this
// wiring's job rather than a subsystem's.
func machine(root string) host {
	return host{
		servers: serve.New(root, procs.System{}),
		entries: readTree,
		tools:   tools.Check,
		cache:   readCache,
	}
}

// readTree reads the config tree from its one location (docs/specs/CONFIG.md).
func readTree() (*config.Tree, error) {
	root, err := config.Root()
	if err != nil {
		return nil, err
	}
	return config.Load(root)
}

// readCache reads the hub cache where huggingface_hub itself puts it — the
// single source of truth for what is already on disk (docs/cria.md, principle 2).
func readCache() (*hubcache.Cache, error) {
	root, err := hubcache.Root()
	if err != nil {
		return nil, err
	}
	return hubcache.Read(root)
}

// newModel builds the frame around the host it drives and its preferences.
func newModel(driven host, root string, saved prefs, prefsErr error) model {
	frame := model{
		host:     driven,
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
	return frame.reselect(0)
}

// Init starts the refresh loop: one observation and one read of the tree now, so
// the box and the list are true before the first tick, and the ticker that keeps
// them true after.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.refresh, m.readEntries, m.scheduleRefresh())
}

// Update is the whole frame's response to the world.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.tickWork()...)
	case snapshotMsg:
		return m.observed(msg), nil
	case entriesMsg:
		return m.loaded(msg), nil
	case startedMsg:
		return m.started(msg), nil
	case actedMsg:
		m.alert = alert{text: msg.text, bad: msg.bad}
		return m, nil
	case holderKilledMsg:
		return m.holderKilled(msg), nil
	case logMsg:
		m.log = m.log.read(msg)
		return m, nil
	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

// tickWork is what one tick asks the host: the servers, the config tree, the
// log file while it is on screen — and the next tick.
func (m model) tickWork() []tea.Cmd {
	work := []tea.Cmd{m.refresh, m.readEntries, m.scheduleRefresh()}
	if m.log.open {
		work = append(work, m.readLog)
	}
	return work
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

// loaded takes one read of the config tree, and the cache walk when the read
// carried one. A tree that could not be read is held on screen the same way a
// failed observation is, and the last good listing stays: the entries cria knew
// about are still the entries the host declares.
func (m model) loaded(msg entriesMsg) model {
	if msg.err != nil {
		m.treeErr = msg.err
		return m
	}
	m.tree, m.treeErr = msg.tree, nil
	if msg.reported {
		m.report, m.reported = msg.report, true
	}
	if msg.walked {
		m.cached, m.cacheErr = msg.cached, nil
	}
	if msg.cacheErr != nil {
		m.cacheErr = msg.cacheErr
	}
	return m.reselect(m.selected)
}

// press routes one keystroke. A modal and the log screen each take the keyboard
// while they are up: what they offer is what the bar draws, and every other key
// would act on something the user is no longer looking at.
func (m model) press(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.modal != nil {
		return m.pressInModal(pressed)
	}
	if m.log.open {
		return m.pressInLog(pressed)
	}

	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.backend):
		return m.switchBackend(), nil
	case key.Matches(pressed, m.keys.view):
		m.view = m.view.other()
		m.alert = alert{}
		return m.reselect(m.selected), nil
	case key.Matches(pressed, m.keys.tools):
		m.alert = pending("the tools pane")
	case key.Matches(pressed, m.keys.up):
		return m.reselect(m.selected - 1), nil
	case key.Matches(pressed, m.keys.down):
		return m.reselect(m.selected + 1), nil
	case key.Matches(pressed, m.keys.start):
		return m.startSelected()
	case key.Matches(pressed, m.keys.stop):
		return m.stopShownServer()
	case key.Matches(pressed, m.keys.forceKill):
		return m.killShownServer()
	case key.Matches(pressed, m.keys.log):
		return m.openLog()
	case key.Matches(pressed, m.keys.restart):
		return m.restartShownEntry()
	case key.Matches(pressed, m.keys.dismiss):
		return m.dismissShownRecord()
	}
	return m, nil
}

// switchBackend flips the active backend and records it: the choice is sticky
// across launches, which is the whole reason it is written down
// (docs/specs/TUI.md). The list is another backend's now, so the cursor starts
// at its top.
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
	return m.reselect(0)
}

// reselect moves the cursor to a row that exists and matches the selection keys
// to what it now points at. Every change that can move the list under the cursor
// — a keypress, a re-read of the tree, a backend switch, a view switch — goes
// through here, so the cursor never points past the end of a list and the bar
// never offers a start there is no entry for.
func (m model) reselect(to int) model {
	rows := m.rows()
	switch {
	case to < 0 || len(rows) == 0:
		m.selected = 0
	case to >= len(rows):
		m.selected = len(rows) - 1
	default:
		m.selected = to
	}

	// The selection belongs to the entry list, so its keys are the serve view's
	// alone: the cache view has a selection of its own (docs/specs/CACHE.md).
	onList := m.view == viewServe && len(rows) > 0
	m.keys.up.SetEnabled(onList)
	m.keys.down.SetEnabled(onList)
	m.keys.start.SetEnabled(onList && rows[m.selected].broken == nil)
	return m
}

// refresh is one observation, taken as a command so the probes and the `ps`
// calls it costs run off the UI thread.
func (m model) refresh() tea.Msg {
	listing, err := m.host.servers.Snapshots()
	return snapshotMsg{listing: listing, err: err}
}

// readEntries re-reads what the config tree declares — an agent writing an entry
// while the TUI is open is the expected way one arrives (docs/cria.md, principle
// 5) — and asks the cache which of those entries could start without
// downloading.
//
// The tool check runs once: it execs `llama-server --version`, and asking again
// every two seconds would put an exec between the display and every frame. A
// start asks for its own fresh report, because that is the moment the answer has
// to be current (docs/specs/SERVE.md).
func (m model) readEntries() tea.Msg {
	tree, err := m.host.entries()
	if err != nil {
		return entriesMsg{err: err}
	}

	msg := entriesMsg{tree: tree}
	if !m.reported {
		msg.report, msg.reported = m.host.tools(tree.Settings), true
	}
	if !m.walksTheCache() {
		return msg
	}

	read, err := m.host.cache()
	if err != nil {
		msg.cacheErr = err
		return msg
	}
	msg.cached, msg.walked = presenceOf(tree, read), true
	return msg
}

// walksTheCache reports whether this tick has a reason to walk the cache. The
// walk sizes every blob on disk, so it is paid only where its answer is read:
// the entry list's cached dots while that list is on screen, and a download's
// progress wherever the user is standing (docs/specs/SERVE.md).
func (m model) walksTheCache() bool {
	if m.view == viewServe && !m.log.open && m.modal == nil {
		return true
	}
	for _, status := range m.listing.Servers {
		if status.Phase == serve.PhaseDownloading {
			return true
		}
	}
	return false
}

// presenceOf answers, for every entry the tree declares, whether starting it
// would download anything (docs/specs/TUI.md).
func presenceOf(tree *config.Tree, read *hubcache.Cache) map[string]bool {
	cached := make(map[string]bool, len(tree.Entries))
	for _, entry := range tree.Entries {
		cached[entry.ID] = read.Presence(entry).Cached
	}
	return cached
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
// The view pane gets the room the other three leave. A terminal too short to
// leave it any loses it: the status box and the keybar are true everywhere — one
// says what is running, the other says what works right now — and a frame
// missing either is unreadable rather than merely smaller.
func (m model) frame() string {
	width := m.frameWidth()
	top := append([]string{pane(statusTitle, width, statusLines(m.listing, m.prefs, m.bar))}, m.notes(width)...)
	bar := renderKeybar(width, m.groups()...)

	rows := m.screenRows(top, bar)
	if rows < minPaneRows {
		return strings.Join(append(top, bar), "\n")
	}
	return strings.Join(append(append([]string{}, top...), m.screen(width, rows), bar), "\n")
}

// screenRows is how many terminal rows are left for the view pane once the box,
// the notes and the bar have had theirs. A terminal that has not said how tall
// it is gets the default: the first render happens before the first
// WindowSizeMsg arrives.
func (m model) screenRows(top []string, bar string) int {
	if m.height <= 0 {
		return defaultRows
	}
	return m.height - lipgloss.Height(strings.Join(append(append([]string{}, top...), bar), "\n"))
}

// notes are the lines between the box and the view: what could not be observed,
// and what the last keypress did. Each is one line and none is a box — they are
// commentary on the box above them.
func (m model) notes(width int) []string {
	var notes []string
	for _, failed := range []struct {
		what string
		err  error
	}{
		{"cannot read the servers: ", m.failure},
		{"cannot read the config tree: ", m.treeErr},
		{"cannot read the model cache: ", m.cacheErr},
	} {
		if failed.err != nil {
			notes = append(notes, fit(alarmStyle.Render(failed.what+failed.err.Error()), width))
		}
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

// screen is the view the frame is routed to, drawn into the rows it was left.
// A modal and the log tail are drawn in the view's place rather than over it:
// the status box above them keeps saying what is running, which is the fact
// either one is read against.
func (m model) screen(width, rows int) string {
	switch {
	case m.log.open:
		return m.log.panel(width, rows)
	case m.modal != nil:
		return m.modal.panel(width, rows)
	case m.view == viewCache:
		return pane("cache", width,
			sizeLines([]string{quietStyle.Render("under construction — the model table and its surgery")}, rows-2))
	}
	return m.serveScreen(width, rows)
}

// groups is the keybar's content: the three scopes, in the order they are read
// — what the highlighted item does, what the running server does, where to go
// (docs/specs/TUI.md).
//
// A modal and the log tail hold the keyboard, so the bar reads what they offer:
// the bar is the map of what works right now, and it must never draw a key the
// screen in front of the user does not answer to.
func (m model) groups() []keyGroup {
	global := keyGroup{label: globalScope, bindings: []key.Binding{m.keys.quit}}
	switch {
	case m.modal != nil:
		return []keyGroup{{label: modalScope, bindings: []key.Binding{m.keys.killHolder, m.keys.leaveModal}}, global}
	case m.log.open:
		return []keyGroup{{label: logScope, bindings: []key.Binding{m.keys.leaveLog}}, global}
	}

	return []keyGroup{
		{label: selectionScope, bindings: []key.Binding{m.keys.start}},
		{label: serverScope, bindings: []key.Binding{m.keys.stop, m.keys.forceKill, m.keys.log, m.keys.restart, m.keys.dismiss}},
		{label: globalScope, bindings: []key.Binding{m.keys.backend, m.keys.view, m.keys.tools, m.keys.quit}},
	}
}

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

// keymap is every key the frame binds, in the three scopes the bar groups them
// by plus the two a screen taking the keyboard offers.
type keymap struct {
	start key.Binding
	up    key.Binding
	down  key.Binding

	stop      key.Binding
	forceKill key.Binding
	log       key.Binding
	restart   key.Binding
	dismiss   key.Binding

	backend key.Binding
	view    key.Binding
	tools   key.Binding
	quit    key.Binding

	killHolder key.Binding
	leaveModal key.Binding
	leaveLog   key.Binding
}

// newKeymap is the frame's keys as they are bound and as the bar spells them.
// The cursor keys are bound but never drawn: arrows and jk are what a list is
// moved with everywhere, and spelling them out would push the keys that are not
// obvious off a narrow bar.
func newKeymap() keymap {
	return keymap{
		start:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "start")),
		up:         key.NewBinding(key.WithKeys("up", "k")),
		down:       key.NewBinding(key.WithKeys("down", "j")),
		stop:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stop")),
		forceKill:  key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "kill")),
		log:        key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
		restart:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
		dismiss:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dismiss")),
		backend:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("⇥", "backend")),
		view:       key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view")),
		tools:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tools")),
		quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		killHolder: key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "kill it")),
		leaveModal: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "leave it alone")),
		leaveLog:   key.NewBinding(key.WithKeys("esc", "l"), key.WithHelp("esc", "close")),
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
	k.forceKill.SetEnabled(target.live)
	k.log.SetEnabled(target.live || target.exited)
	k.restart.SetEnabled(target.shown)
	k.dismiss.SetEnabled(target.exited)
}
