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
	Running(entryID string) (serve.Server, bool, error)
	Start(entry config.Entry, report tools.Report) (serve.Record, error)
	Stop(record serve.Record) error
	Kill(record serve.Record) error
	Dismiss(record serve.Record) error
	Warm(record serve.Record) error
	Bench(record serve.Record, spec serve.BenchSpec, report func(serve.BenchStep)) serve.BenchResult
	PortUse(port int) (serve.PortUse, error)
	KillHolder(holder serve.Holder) error
}

// host is everything the frame reads this machine through: the lifecycle, the
// config tree it lists, the tool check that says what may be launched, and the
// hub cache that says what is already on disk — read by the walk, written by the
// surgery. One value, wired in Run and replaced wholesale by the tests.
type host struct {
	servers servers
	entries func() (*config.Tree, error)
	tools   func(config.Settings) tools.Report
	cache   func() (*hubcache.Cache, error)
	surgery surgery
}

// surgery is the deletion half of hubcache as the cache view drives it: one
// planner per unit docs/specs/CACHE.md makes deletable, and the execute that
// carries a plan out. Each is hubcache's own call rather than a dispatcher over
// them (CODING-RULES §1); naming them here is what lets the whole delete flow —
// the refusal, the confirmation, the drift — be exercised with no cache on disk.
type surgery struct {
	quant    func(repo *hubcache.Repo, quant string, served []hubcache.Served) (*hubcache.Plan, error)
	repo     func(repo *hubcache.Repo, served []hubcache.Served) (*hubcache.Plan, error)
	partials func(repo *hubcache.Repo, served []hubcache.Served) (*hubcache.Plan, error)
	execute  func(plan *hubcache.Plan, served []hubcache.Served) (int64, error)
}

// view names the screen the frame is routed to. Two of them in v1
// (docs/cria.md, v1 surface), switched by one global key.
type view int

const (
	viewServe view = iota
	viewCache
)

// alert is the one line under the status box, and it carries what the boxes
// cannot show: a refusal, an error, an outcome that leaves no trace on screen —
// bytes reclaimed, a port that has just come free — and the question a server
// key asks when several servers could answer it (pick.go). It never restates
// visible state. "started qwen", "stopped qwen", "backend mlx" are all readable
// from the status box and the pane titles a moment later, and a line repeating
// them is one more thing to read that is already true, still sitting there three
// keypresses on. An action cria is in the middle of is no different: what it
// will have changed is what the box says when it lands.
type alert struct {
	text string
	bad  bool // cria could not do the thing, rather than merely did it
}

// The messages the frame runs on: the refresh tick, and every answer that comes
// back off the UI thread.
type (
	tickMsg     time.Time
	snapshotMsg struct {
		listing serve.StatusListing
		err     error
	}
	// entriesMsg is one read of the config tree and — when a list that draws it
	// is on screen — one walk of the hub cache. The two travel together because
	// what either list says about a model is the two of them joined.
	entriesMsg struct {
		tree     *config.Tree
		err      error
		report   tools.Report
		reported bool // the tool check ran; it runs once, on the first tree read
		read     *hubcache.Cache
		walked   bool // the cache was read, so read is the whole answer
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

	// What cria is doing to an entry between the keypress and the answer, drawn
	// in the box where a server's state is read (lifecycle.go).
	pending pendingActions

	// What the config tree declares, and what the host holds for it. Each is
	// held with the reason it could not be read, which stays on screen until a
	// read succeeds — for the same reason a failed observation does.
	tree     *config.Tree
	treeErr  error
	report   tools.Report
	reported bool
	cache    *hubcache.Cache
	cacheErr error

	// Each list keeps its own cursor: the entry list and the cache list hold
	// different things, and coming back to a view lands where it was left.
	selected      int
	cacheSelected int

	modal     *modal    // the refusal a start came back with; nil when there is none
	confirm   *deletion // the delete waiting for its answer; nil when none is
	pick      *pick     // the server key waiting for the server it means; nil when none is
	toolsOpen bool      // the tools report is up, over whichever view is behind it
	log       logScreen

	// This session's benchmarks: the pane that shows them, every sweep that has
	// finished, and the one still running (benchpane.go). They live as long as
	// this cria does — a measurement is of this machine at this moment, and
	// nothing about it is written down.
	benchOpen bool
	benchLog  []serve.BenchResult
	benching  *benchRun

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
		surgery: surgery{
			quant:    hubcache.PlanQuant,
			repo:     hubcache.PlanRepo,
			partials: hubcache.PlanPartials,
			execute:  hubcache.Execute,
		},
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
		return m.started(msg)
	case actedMsg:
		return m.acted(msg)
	case warmedMsg:
		return m.warmed(msg), nil
	case holderKilledMsg:
		return m.holderKilled(msg), nil
	case plannedMsg:
		return m.planned(msg), nil
	case deletedMsg:
		return m.deleted(msg)
	case toolsMsg:
		m.report, m.reported = msg.report, true
		return m, nil
	case benchStepMsg:
		return m.benchStepped(msg)
	case benchedMsg:
		return m.benched(msg)
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
		m.cache, m.cacheErr = msg.read, nil
	}
	if msg.cacheErr != nil {
		m.cacheErr = msg.cacheErr
	}
	return m.reselect(m.cursor())
}

// press routes one keystroke. A modal, the tools report, the log screen and a
// server key waiting for its target each take the keyboard while they are up:
// what they offer is what the bar draws, and every other key would act on
// something the user is no longer looking at — or answer a question they are in
// the middle of.
func (m model) press(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// esc means what is on screen right now, whatever path set the alert.
	m = m.syncEscScope()
	switch {
	case m.modal != nil:
		return m.pressInModal(pressed)
	case m.confirm != nil:
		return m.pressInConfirm(pressed)
	case m.toolsOpen:
		return m.pressInTools(pressed)
	case m.log.open:
		return m.pressInLog(pressed)
	case m.pick != nil:
		// The pick comes before the bench pane rather than after it: ⏎ in that
		// pane is what arms the pick, and the question is answered in the status
		// box above the pane it was asked from.
		return m.pressInPick(pressed)
	case m.benchOpen:
		return m.pressInBench(pressed)
	}

	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.backend):
		return m.switchBackend(), nil
	case key.Matches(pressed, m.keys.cache):
		return m.show(viewCache), nil
	case key.Matches(pressed, m.keys.clearAlert):
		// esc answers the alert before it leads anywhere: the line is the
		// newest thing on screen, and dismissing it is §5's user-dismiss. The
		// sticky observation failures re-state themselves every failing tick,
		// so esc has nothing lasting to say to them.
		m.alert = alert{}
		return m.syncEscScope(), nil
	case key.Matches(pressed, m.keys.back):
		return m.show(viewServe), nil
	case key.Matches(pressed, m.keys.tools):
		return m.openTools()
	case key.Matches(pressed, m.keys.bench):
		return m.openBench()
	case key.Matches(pressed, m.keys.up):
		return m.reselect(m.cursor() - 1), nil
	case key.Matches(pressed, m.keys.down):
		return m.reselect(m.cursor() + 1), nil
	case key.Matches(pressed, m.keys.start):
		return m.startSelected()
	case key.Matches(pressed, m.keys.remove):
		return m.deleteSelected()
	case key.Matches(pressed, m.keys.stop):
		return m.aim(pickStop)
	case key.Matches(pressed, m.keys.forceKill):
		return m.aim(pickKill)
	case key.Matches(pressed, m.keys.log):
		return m.aim(pickLog)
	case key.Matches(pressed, m.keys.restart):
		return m.aimRestart()
	case key.Matches(pressed, m.keys.dismiss):
		return m.aim(pickDismiss)
	}
	return m, nil
}

// switchBackend flips the active backend and records it: the choice is sticky
// across launches, which is the whole reason it is written down
// (docs/specs/TUI.md). The list is another backend's now, so the cursor starts
// at its top.
//
// The toggle reports nothing: the serve pane's title already names the backend
// it is showing, in that backend's own colour, and a line under the status box
// would say the same thing while sitting there long after the change
// (docs/specs/TUI.md). A write that fails is not the toggle succeeding, so that
// one still speaks — the session has the backend the user asked for, and what
// was lost is cria's memory of it.
func (m model) switchBackend() model {
	m.prefs.Backend = m.prefs.other()
	m.alert = alert{}
	if err := savePrefs(m.root, m.prefs); err != nil {
		m.alert = alert{text: err.Error(), bad: true}
	}
	// The entry list is another backend's now, so its cursor starts at the top.
	// The cache list holds the same models either way and keeps its place.
	m.selected = 0
	return m.reselect(m.cursor())
}

// show routes the frame to one screen: c goes to the cache, esc comes back
// (docs/specs/TUI.md). The alert goes with the old screen — it reported what a
// keypress did there, and the answer to it is no longer on screen — and the
// cursor is re-matched to the list that is, each view keeping its own place.
func (m model) show(showing view) model {
	m.view = showing
	m.alert = alert{}
	return m.reselect(m.cursor())
}

// reselect moves the visible list's cursor to a row that exists and matches the
// selection keys to what it now points at. Every change that can move a list
// under its cursor — a keypress, a re-read of the tree, a cache walk, a backend
// switch, a view switch — goes through here, so a cursor never points past the
// end of its list and the bar never offers an action there is no row for.
func (m model) reselect(to int) model {
	if m.view == viewCache {
		m.cacheSelected = clamped(to, len(m.cacheRows()))
	} else {
		m.selected = clamped(to, len(m.rows()))
	}
	return m.rebindContext()
}

// clamped keeps a cursor on a row that exists.
func clamped(to, rows int) int {
	switch {
	case to < 0 || rows == 0:
		return 0
	case to >= rows:
		return rows - 1
	}
	return to
}

// cursor is where the visible list's cursor sits.
func (m model) cursor() int {
	if m.view == viewCache {
		return m.cacheSelected
	}
	return m.selected
}

// rebindContext matches every key that depends on where the user is standing to
// the screen in front of them: the selection keys to the row the cursor is on,
// and the two navigation keys to the view they lead out of. Each view has its
// own selection and its own key — the entry list starts what it highlights, the
// cache list deletes it (docs/specs/CACHE.md) — and a key the bar does not draw
// does nothing when pressed.
func (m model) rebindContext() model {
	entry, hasEntry := m.selectedRow()
	_, hasCached := m.selectedCacheRow()
	onEntryList := m.view == viewServe && hasEntry
	onCacheList := m.view == viewCache && hasCached

	m.keys.up.SetEnabled(onEntryList || onCacheList)
	m.keys.down.SetEnabled(onEntryList || onCacheList)
	m.keys.start.SetEnabled(onEntryList && entry.broken == nil)
	m.keys.remove.SetEnabled(onCacheList)

	// One of the two navigation keys is live at a time: c goes to the cache, esc
	// comes back. esc closes whatever has the keyboard first — a screen over the
	// view is what the key is reached for there (press) — then a visible alert,
	// then the way back (syncEscScope).
	m.keys.cache.SetEnabled(m.view == viewServe)
	return m.syncEscScope()
}

// syncEscScope points esc at what it answers right now, in its settled order:
// an overlay is press's to close, a visible alert is dismissed next, and only
// then does esc lead back out of the cache view. The bar draws whichever of the
// two is live, so the key's next meaning is always the one on screen.
func (m model) syncEscScope() model {
	m.keys.clearAlert.SetEnabled(m.alert.text != "")
	m.keys.back.SetEnabled(m.view == viewCache && m.alert.text == "")
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
// 5) — and walks the cache the two lists are drawn from.
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
	msg.read, msg.walked = read, true
	return msg
}

// walksTheCache reports whether this tick has a reason to walk the cache. The
// walk sizes every blob on disk, so it is paid only where its answer is read:
// whichever list is on screen — the entry list's cached dots, the cache view
// itself — and a download's progress wherever the user is standing
// (docs/specs/SERVE.md, docs/specs/CACHE.md).
//
// A screen that has taken the keyboard is drawn over both lists, so nothing on
// it reads a walk. The delete confirmation is one of those, and doubly so: it
// renders a plan made against the cache as it was, and the plan's own
// re-derivation is what checks that against the cache as it is.
func (m model) walksTheCache() bool {
	if !m.log.open && !m.toolsOpen && !m.benchOpen && m.modal == nil && m.confirm == nil {
		return true
	}
	for _, status := range m.listing.Servers {
		if status.Phase == serve.PhaseDownloading {
			return true
		}
	}
	return false
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
	top := append([]string{pane(paneTitle(statusTitle), width, statusLines(m.listing, m.pending, m.prefs, m.bar, m.boxCursor()))}, m.notes(width)...)
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
	if len(notes) == 0 {
		// The row is reserved even when it has nothing to say: a line that
		// appears and disappears would shift everything under it on every
		// notice (docs/specs/TUI.md).
		notes = append(notes, fit("", width))
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
	case m.toolsOpen:
		return m.toolsPanel(width, rows)
	case m.benchOpen:
		return m.benchPanel(width, rows)
	case m.modal != nil:
		return m.modal.panel(width, rows)
	case m.confirm != nil:
		return m.confirmPanel(width, rows)
	case m.view == viewCache:
		return m.cacheScreen(width, rows)
	}
	return m.serveScreen(width, rows)
}

// groups is the keybar's content: the three scopes, in the order they are read
// — what the highlighted item does, what the running server does, where to go
// (docs/specs/TUI.md).
//
// A modal, the log tail and an armed server key hold the keyboard, so the bar
// reads what they offer: the bar is the map of what works right now, and it must
// never draw a key the screen in front of the user does not answer to.
func (m model) groups() []keyGroup {
	// The bar is drawn from this frame's truth, whatever path set the alert.
	m = m.syncEscScope()
	global := keyGroup{label: globalScope, bindings: []key.Binding{m.keys.quit}}
	switch {
	case m.modal != nil:
		return []keyGroup{{label: modalScope, bindings: []key.Binding{m.keys.killHolder, m.keys.leaveModal}}, global}
	case m.confirm != nil:
		return []keyGroup{{label: deleteScope, bindings: []key.Binding{m.keys.confirmDelete, m.keys.cancelDelete}}, global}
	case m.toolsOpen:
		return []keyGroup{{label: toolsScope, bindings: []key.Binding{m.keys.leaveTools}}, global}
	case m.log.open:
		return []keyGroup{{label: logScope, bindings: []key.Binding{m.keys.leaveLog}}, global}
	case m.pick != nil:
		return []keyGroup{{label: m.pick.action.scope(), bindings: []key.Binding{m.keys.runPick, m.keys.cancelPick}}, global}
	case m.benchOpen:
		return []keyGroup{{label: benchScope, bindings: []key.Binding{m.keys.runBench, m.keys.leaveBench}}, global}
	}

	return []keyGroup{
		{label: selectionScope, bindings: []key.Binding{m.keys.start, m.keys.remove}},
		{label: serverScope, bindings: []key.Binding{m.keys.stop, m.keys.forceKill, m.keys.log, m.keys.restart, m.keys.dismiss}},
		{label: globalScope, bindings: []key.Binding{m.keys.backend, m.keys.cache, m.keys.clearAlert, m.keys.back, m.keys.tools, m.keys.bench, m.keys.quit}},
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
	start  key.Binding
	remove key.Binding
	up     key.Binding
	down   key.Binding

	stop      key.Binding
	forceKill key.Binding
	log       key.Binding
	restart   key.Binding
	dismiss   key.Binding

	backend    key.Binding
	cache      key.Binding
	back       key.Binding
	clearAlert key.Binding
	tools      key.Binding
	bench      key.Binding
	quit       key.Binding

	killHolder    key.Binding
	leaveModal    key.Binding
	confirmDelete key.Binding
	cancelDelete  key.Binding
	leaveTools    key.Binding
	leaveLog      key.Binding
	runBench      key.Binding
	leaveBench    key.Binding

	pickUp     key.Binding
	pickDown   key.Binding
	runPick    key.Binding
	cancelPick key.Binding
}

// newKeymap is the frame's keys as they are bound and as the bar spells them.
// The cursor keys are bound but never drawn: arrows and jk are what a list is
// moved with everywhere, and spelling them out would push the keys that are not
// obvious off a narrow bar. The status box has its own pair rather than sharing
// them: the list's pair is live only where a list has rows, and the box is a
// picker whether or not the view behind it is showing anything (pick.go).
//
// What ⏎ answers while a key is armed is that key's own word, so the binding is
// spelled when the action is armed rather than here.
func newKeymap() keymap {
	return keymap{
		start:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "start")),
		remove:        key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
		up:            key.NewBinding(key.WithKeys("up", "k")),
		down:          key.NewBinding(key.WithKeys("down", "j")),
		stop:          key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stop")),
		forceKill:     key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "kill")),
		log:           key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
		restart:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
		dismiss:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dismiss")),
		backend:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("⇥", "backend")),
		cache:         key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cache")),
		back:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		clearAlert:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "dismiss")),
		tools:         key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tools")),
		bench:         key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bench")),
		quit:          key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		killHolder:    key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "kill it")),
		leaveModal:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "leave it alone")),
		confirmDelete: key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "delete")),
		cancelDelete:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		leaveTools:    key.NewBinding(key.WithKeys("esc", "t"), key.WithHelp("esc", "close")),
		leaveLog:      key.NewBinding(key.WithKeys("esc", "l"), key.WithHelp("esc", "close")),
		runBench:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "bench")),
		leaveBench:    key.NewBinding(key.WithKeys("esc", "b"), key.WithHelp("esc", "close")),
		pickUp:        key.NewBinding(key.WithKeys("up", "k")),
		pickDown:      key.NewBinding(key.WithKeys("down", "j")),
		runPick:       key.NewBinding(key.WithKeys("enter")),
		cancelPick:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
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
