package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"cria/internal/config"
	"cria/internal/hubcache"
	"cria/internal/serve"
)

// cachedQwen is a hub cache holding all of the qwen entry's model and none of
// gemma's — the two answers the entry list's dots stand for.
func cachedQwen() *hubcache.Cache {
	return &hubcache.Cache{
		Root: "/home/u/.cache/huggingface/hub",
		Repos: []hubcache.Repo{{
			ID:   "unsloth/Qwen3-30B-A3B-GGUF",
			Type: hubcache.RepoModel,
			Kind: hubcache.KindGGUF,
			Items: []hubcache.Item{
				{Label: "UD-Q4_K_XL", Bytes: 18 << 30, Complete: true},
			},
		}},
	}
}

// serveFrame is a frame over a host with the test tree and that cache, already
// read once — the state the serve view is normally looked at in.
func serveFrame(t *testing.T) (model, *testHost) {
	t.Helper()
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree, world.cache = testTree(), cachedQwen()
	return load(t, frame), world
}

// listWidth is the pane the list is drawn into by the assertions here: wide
// enough that no row is truncated, so what a test reads is the whole row.
const listWidth = 76

// list is the entry list as a person reads it, one line per row.
func list(frame model, capacity int) []string {
	var lines []string
	for _, line := range frame.listLines(listWidth, capacity) {
		if trimmed := strings.TrimRight(plain(line), " "); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// The list is the active backend's, never a mixed one, and every row carries
// what a start would do: the entry, the model, the port when it is the entry's
// own, and whether starting it serves or downloads (docs/specs/TUI.md).
func TestEntryListShowsTheActiveBackend(t *testing.T) {
	frame, _ := serveFrame(t)

	lines := list(frame, 10)
	if len(lines) != 3 {
		t.Fatalf("the llama list drew %d rows, want two entries and the refused file: %q", len(lines), lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "mlx-qwen") {
		t.Errorf("the llama list holds an mlx entry: %q", lines)
	}

	if want := absentMark + "  gemma  unsloth/gemma-3-27b-it-GGUF:Q4_K_M  :8081"; !strings.Contains(lines[0], want) {
		t.Errorf("the gemma row reads %q, want %q", lines[0], want)
	}
	// qwen sits on the tree's default port, so its row does not repeat it: a
	// port is worth a column exactly when it is the entry's own choice.
	if want := cachedMark + "  qwen  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL"; !strings.Contains(lines[1], want) {
		t.Errorf("the qwen row reads %q, want %q", lines[1], want)
	}
	if strings.Contains(lines[1], ":8080") {
		t.Errorf("the qwen row repeats the tree's default port: %q", lines[1])
	}

	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	mlx := list(frame, 10)
	if !strings.Contains(mlx[0], "mlx-qwen") || strings.Contains(strings.Join(mlx, "\n"), "gemma") {
		t.Errorf("the mlx tab reads %q, want the mlx entry and no llama one", mlx)
	}
}

// An entry file cria refused stays on the list with the key that failed: one
// broken file disables only itself, and a file nobody can see is one nobody
// fixes (docs/specs/CONFIG.md).
func TestRefusedEntryFilesStayVisible(t *testing.T) {
	frame, _ := serveFrame(t)

	lines := list(frame, 10)
	refused := lines[len(lines)-1]
	if !strings.Contains(refused, "typo") || !strings.Contains(refused, `key "prot"`) {
		t.Errorf("the refused row reads %q, want the file and its offending key", refused)
	}

	rows := frame.rows()
	drawn := frame.rowLine(rows[len(rows)-1], false, listWidth)
	if !strings.HasPrefix(strings.TrimLeft(drawn, " "), opener(brokenStyle)) {
		t.Errorf("the refused row is not drawn as one cria cannot act on: %q", drawn)
	}
}

// The cursor moves with either set of keys and stops at both ends, and the row
// it is on is the one the detail pane and ⏎ act on.
func TestSelectionMoves(t *testing.T) {
	frame, _ := serveFrame(t)
	if frame.selected != 0 {
		t.Fatalf("the cursor starts on row %d, want the first", frame.selected)
	}

	for _, pressed := range []tea.KeyPressMsg{typed('j'), {Code: tea.KeyDown}} {
		frame, _ = press(t, frame, pressed)
	}
	if frame.selected != 2 {
		t.Fatalf("two moves down left the cursor on row %d, want row 2", frame.selected)
	}
	frame, _ = press(t, frame, typed('j'))
	if frame.selected != 2 {
		t.Errorf("the cursor ran past the end of the list to row %d", frame.selected)
	}

	for range 4 {
		frame, _ = press(t, frame, typed('k'))
	}
	if frame.selected != 0 {
		t.Errorf("the cursor ran past the top of the list to row %d", frame.selected)
	}

	// The highlighted row is the one drawn as highlighted.
	rows := frame.rows()
	if !strings.Contains(frame.rowLine(rows[0], true, listWidth), cursorMark) {
		t.Errorf("the selected row carries no cursor: %q", frame.rowLine(rows[0], true, listWidth))
	}
	if strings.Contains(frame.rowLine(rows[1], false, listWidth), cursorMark) {
		t.Errorf("an unselected row carries the cursor: %q", frame.rowLine(rows[1], false, listWidth))
	}
	assertBanded(t, frame.rowLine(rows[0], true, listWidth), frame.rowLine(rows[1], false, listWidth), listWidth)
}

// A list longer than the pane scrolls with the cursor rather than losing it.
func TestLongListKeepsTheCursorOnScreen(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	tree := &config.Tree{Root: "/home/u/.config/cria", Settings: config.Settings{DefaultPort: 8080}}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		tree.Entries = append(tree.Entries, config.Entry{
			ID: id, Path: "/home/u/.config/cria/models/" + id + ".toml", Backend: config.BackendLlama,
			Repo: "org/" + id, Port: 8080, Host: "0.0.0.0", Name: id,
		})
	}
	world.tree = tree
	frame = load(t, frame)

	frame = frame.reselect(5)
	lines := list(frame, 3)
	if len(lines) != 3 {
		t.Fatalf("a three-row pane drew %d rows: %q", len(lines), lines)
	}
	if !strings.Contains(lines[len(lines)-1], "f") {
		t.Errorf("the pane reads %q, want the row the cursor is on", lines)
	}
}

// The detail pane is the entry's documentation: every key it sets, its args as
// the file wrote them, and the exact command line a start would run
// (docs/specs/CONFIG.md).
func TestDetailPaneCarriesTheWholeEntry(t *testing.T) {
	frame, _ := serveFrame(t)
	frame = frame.reselect(1) // qwen

	detail := plain(strings.Join(frame.detailLines(200, 30), "\n"))
	for _, fact := range []string{
		"qwen 30b",
		"/home/u/.config/cria/models/qwen.toml",
		"llama",
		"unsloth/Qwen3-30B-A3B-GGUF",
		"UD-Q4_K_XL",
		"8080",
		"0.0.0.0",
		"--ctx-size 16384 --jinja",
		"yes — starting it serves what is on disk",
		"/opt/homebrew/bin/llama-server -hf unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL --host 0.0.0.0 --port 8080 --ctx-size 16384 --jinja",
	} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the detail pane does not carry %q:\n%s", fact, detail)
		}
	}
}

// The detail pane is a label column and a value column, and they are told apart
// by colour: the label says what the value is, so it is drawn as structure while
// the value is body text.
func TestDetailPaneColoursItsLabels(t *testing.T) {
	frame, _ := serveFrame(t)
	frame = frame.reselect(1) // qwen

	detail := strings.Join(frame.detailLines(200, 30), "\n")
	if !strings.Contains(detail, labelStyle.Render(fit("repo", detailLabelWidth))) {
		t.Errorf("the detail pane does not draw its labels in the label colour:\n%s", detail)
	}
	if !strings.Contains(detail, factStyle.Render("unsloth/Qwen3-30B-A3B-GGUF")) {
		t.Errorf("the detail pane does not draw its values as body text:\n%s", detail)
	}
	if labelStyle.GetForeground() == factStyle.GetForeground() {
		t.Error("labels and values are drawn in one colour; the label is what makes the value readable")
	}
	// The backend is a value that carries its own colour, the same one the pane
	// title spells it in.
	if !strings.Contains(detail, backendTone(config.BackendLlama).Render("llama")) {
		t.Errorf("the detail pane does not spell the backend in the backend's colour:\n%s", detail)
	}
}

// The command line the pane shows is the one Start would spawn — the same
// composition, so the two cannot drift apart.
func TestDetailCommandIsTheOneStartWouldRun(t *testing.T) {
	frame, world := serveFrame(t)
	frame = frame.reselect(1)

	entry := frame.tree.Entries[2]
	want, err := serve.ComposedCommand(entry, world.report)
	if err != nil {
		t.Fatalf("composing the entry's command: %v", err)
	}
	shown, refused := frame.composedCommand(entry)
	if refused || shown != strings.Join(want, " ") {
		t.Errorf("the pane shows %q, want %q", shown, strings.Join(want, " "))
	}
}

// A tool the host cannot launch with costs the pane its command line and says
// why: the program cria would exec is half of that line (docs/specs/TOOLS.md).
func TestDetailReportsAToolThatCannotLaunch(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	world.report = usableTools()
	world.report.LlamaServer = missingLlamaServer()
	frame = load(t, frame)

	detail := plain(strings.Join(frame.detailLines(80, 30), "\n"))
	if !strings.Contains(detail, "llama-server is missing") {
		t.Errorf("the detail pane does not say why it cannot spell the command:\n%s", detail)
	}
}

// A refused entry file's detail is what fixes it: the file, the key, and where
// the schema is (docs/specs/CONFIG.md).
func TestDetailPaneOfARefusedFile(t *testing.T) {
	frame, _ := serveFrame(t)
	frame = frame.reselect(2)

	detail := plain(strings.Join(frame.detailLines(80, 30), "\n"))
	for _, fact := range []string{"/home/u/.config/cria/models/typo.toml", `key "prot"`, "cria docs"} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the refused file's detail does not carry %q:\n%s", fact, detail)
		}
	}
}

// A backend with nothing declared for it says where entries are written and what
// prints the schema — the tree is written by hand or by an agent
// (docs/cria.md, principle 5).
func TestEmptyBackendPointsAtTheDocs(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = &config.Tree{Root: "/home/u/.config/cria"}
	frame = load(t, frame)

	drawn := plain(strings.Join(frame.listLines(listWidth, 6), "\n"))
	for _, fact := range []string{"no llama entries", "/home/u/.config/cria/models", "cria docs"} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the empty list does not say %q:\n%s", fact, drawn)
		}
	}
}

// The detail pane sits beside the list where there is width for both and under
// it where there is not: the picker is never a list of names with the truth cut
// off (docs/specs/TUI.md).
func TestNarrowTerminalStacksTheDetailPane(t *testing.T) {
	frame, _ := serveFrame(t)

	wide := plain(frame.serveScreen(120, 12))
	if !strings.Contains(strings.Split(wide, "\n")[0], detailTitle) {
		t.Errorf("a 120-cell terminal does not draw the detail pane beside the list:\n%s", wide)
	}
	for _, line := range strings.Split(wide, "\n") {
		if lipgloss.Width(line) != 120 {
			t.Errorf("a side-by-side line is %d cells wide, want 120: %q", lipgloss.Width(line), line)
		}
	}

	narrow := plain(frame.serveScreen(70, 12))
	rows := strings.Split(narrow, "\n")
	if strings.Contains(rows[0], detailTitle) {
		t.Errorf("a 70-cell terminal kept the detail pane beside the list:\n%s", narrow)
	}
	if !strings.Contains(narrow, detailTitle) || !strings.Contains(narrow, "qwen") {
		t.Errorf("a 70-cell terminal lost half the serve view:\n%s", narrow)
	}
	for _, line := range rows {
		if lipgloss.Width(line) != 70 {
			t.Errorf("a stacked line is %d cells wide, want 70: %q", lipgloss.Width(line), line)
		}
	}
}

// The selection scope of the keybar carries the one key that reads the
// highlighted row, and only while there is a row it can act on
// (docs/specs/TUI.md).
func TestKeybarOffersStartForTheSelection(t *testing.T) {
	frame, _ := serveFrame(t)
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, selectionScope+" ⏎ start") {
		t.Errorf("the keybar reads %q, want the selection scope to offer the start", bar)
	}

	// A refused file is listed and readable, but there is nothing to launch.
	frame = frame.reselect(2)
	if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "⏎ start") {
		t.Errorf("the keybar offers a start for an entry file cria refused: %q", bar)
	}

	// The cache view has a selection of its own (docs/specs/CACHE.md); the
	// entry list's keys are not it.
	frame = frame.reselect(0)
	frame, _ = press(t, frame, typed('c'))
	if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "⏎ start") {
		t.Errorf("the cache view offers the entry list's start: %q", bar)
	}
}

// The cache walk is paid where its answer is read: whichever list is on screen —
// the entry list's dots, the cache view itself — and a download's progress
// wherever the user is standing. Nowhere else does a refresh walk every blob on
// disk (docs/specs/SERVE.md, docs/specs/CACHE.md).
func TestCacheIsWalkedOnlyWhereItIsRead(t *testing.T) {
	cases := []struct {
		name  string
		frame func(model) model
		want  bool
	}{
		{
			name:  "the entry list is on screen",
			frame: func(m model) model { return m },
			want:  true,
		},
		{
			name: "the cache view is: it is the walk drawn out",
			frame: func(m model) model {
				m.view = viewCache
				return m
			},
			want: true,
		},
		{
			name: "the log tail is over the entry list",
			frame: func(m model) model {
				m.log = logScreen{open: true}
				return m
			},
		},
		{
			name: "the tools report is over the cache view",
			frame: func(m model) model {
				m.view, m.toolsOpen = viewCache, true
				return m
			},
		},
		{
			name: "a delete is waiting for its answer",
			frame: func(m model) model {
				m.view = viewCache
				m.confirm = &deletion{plan: &hubcache.Plan{}}
				return m
			},
		},
		{
			name: "a download is running behind whatever has the keyboard",
			frame: func(m model) model {
				m.log = logScreen{open: true}
				m.listing = serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseDownloading)}}
				return m
			},
			want: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
			world.tree, world.cache = testTree(), cachedQwen()

			msg, ok := test.frame(frame).readEntries().(entriesMsg)
			if !ok {
				t.Fatal("reading the config tree did not answer with the tree")
			}
			if msg.walked != test.want {
				t.Errorf("the read walked the cache: %v, want %v", msg.walked, test.want)
			}
		})
	}
}

// The tool check execs a program, so it runs once rather than on every tick; a
// start asks for its own fresh report, which is the moment the answer has to be
// current.
func TestTheToolCheckRunsOnce(t *testing.T) {
	frame, world := serveFrame(t)
	for range 3 {
		frame = load(t, frame)
	}
	if world.checks != 1 {
		t.Errorf("the tool check ran %d times over four reads of the tree, want once", world.checks)
	}
}

// A dot is not a claim: before the cache has answered, the list says it does not
// know rather than showing an entry as absent (CODING-RULES §4).
func TestUnwalkedEntriesClaimNothing(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = testTree()
	world.cacheErr = errUnreadableCache
	frame = load(t, frame)

	qwen, found := frame.entryNamed("qwen")
	if !found {
		t.Fatal("the test tree holds no qwen entry")
	}
	if mark := plain(frame.presenceMark(qwen, paintFor(false))); mark != unknownMark {
		t.Errorf("an entry the cache could not be asked about is marked %q, want %q", mark, unknownMark)
	}
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "cannot read the model cache") {
		t.Errorf("the frame does not report the cache it could not read:\n%s", drawn)
	}
}
