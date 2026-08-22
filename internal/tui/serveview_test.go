package tui

import (
	"reflect"
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

// groupedFrame is the serve view over the grouped fixture (groups_test.go): a
// tree with entries under both backends and a refused file, filed by
// preferences that leave a group per case the pane has to draw.
func groupedFrame(t *testing.T) model {
	t.Helper()
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree = groupedTree()
	frame.prefs.Groups = groupedPrefs()
	return load(t, frame)
}

// listWidth is the pane the list is drawn into by the assertions here: wide
// enough that no row is truncated, so what a test reads is the whole row.
const listWidth = 76

// runeColumn is the column a substring starts at, counted in runes — the cell
// arithmetic a reader's eye does, which byte indexes misstate around glyphs.
func runeColumn(line, substring string) int {
	at := strings.Index(line, substring)
	if at < 0 {
		return -1
	}
	return len([]rune(line[:at]))
}

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

// headings is the group names the pane draws, in the order it draws them: the
// lines that start at the pane's left edge, every row being indented past the
// column the cursor's marker keeps.
func headings(frame model, capacity int) []string {
	var names []string
	for _, line := range list(frame, capacity) {
		if !strings.HasPrefix(line, nothingHere) && !strings.HasPrefix(line, cursorMark) {
			names = append(names, line)
		}
	}
	return names
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
	if want := cachedMark + "  qwen"; !strings.Contains(lines[1], want) {
		t.Errorf("the qwen row reads %q, want it to open with %q", lines[1], want)
	}
	if strings.Contains(lines[1], ":8080") {
		t.Errorf("the qwen row repeats the tree's default port: %q", lines[1])
	}
	// The list is a table: the model column starts at one x for every row, the
	// widest id (gemma) setting the column that shorter ids pad out to. Runes,
	// not bytes — the cursor glyph is one cell but three bytes.
	if g, q := runeColumn(lines[0], "unsloth/"), runeColumn(lines[1], "unsloth/"); g != q || g < 0 {
		t.Errorf("the model column is not aligned: gemma at %d, qwen at %d\n%q\n%q", g, q, lines[0], lines[1])
	}

	frame, _ = press(t, frame, tea.KeyPressMsg{Code: tea.KeyTab})
	mlx := list(frame, 10)
	if !strings.Contains(mlx[0], "mlx-qwen") || strings.Contains(strings.Join(mlx, "\n"), "gemma") {
		t.Errorf("the mlx tab reads %q, want the mlx entry and no llama one", mlx)
	}
}

// With no groups defined the list is the drawing it always was: the rows
// themselves, in order, with nothing over them and no shift in their spacing.
// Groups are opt-in, so this is the pane most sessions see (docs/specs/TUI.md).
func TestWithoutGroupsTheListIsDrawnAsItWas(t *testing.T) {
	frame, _ := serveFrame(t)

	rows := frame.rows()
	column := idColumn(rows)
	want := make([]string, 0, len(rows))
	for i, listed := range rows {
		want = append(want, frame.rowLine(listed, i == frame.selected, listWidth, column))
	}

	// Byte for byte, escapes included: a heading line or a changed indent would
	// show up here rather than in a screenshot months later.
	if got := frame.listLines(listWidth, len(rows)); !reflect.DeepEqual(got, want) {
		t.Errorf("the ungrouped list draws\n%q\nwant the rows themselves\n%q", got, want)
	}
	if names := headings(frame, 10); len(names) != 0 {
		t.Errorf("a list with no groups drew the headings %q", names)
	}
}

// The list is drawn in sections: each group that has something to show here, in
// the order the preferences hold them, its entries under it, and the ungrouped
// tail last. A group whose only members are under the other backend draws no
// heading at all, while one standing empty keeps its heading so it stays
// findable (docs/specs/TUI.md).
func TestTheEntryListDrawsItsGroups(t *testing.T) {
	cases := []struct {
		name     string
		backend  config.Backend
		headings []string
		down     []string
	}{
		{
			name: "the llama tab", backend: config.BackendLlama,
			headings: []string{"daily", "emptied", "ghosts", "ungrouped"},
			down:     []string{"daily", "air", "dust", "emptied", "ghosts", "ungrouped", "bark", "typo"},
		},
		{
			name: "the mlx tab", backend: config.BackendMLX,
			headings: []string{"mlx only", "emptied", "ghosts", "ungrouped"},
			down:     []string{"mlx only", "cliff", "emptied", "ghosts", "ungrouped", "echo", "typo"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			frame := groupedFrame(t)
			frame.prefs.Backend = test.backend
			frame = frame.reselect(0)

			lines := list(frame, 12)
			if len(lines) != len(test.down) {
				t.Fatalf("the pane drew %d lines:\n%s\nwant\n%s", len(lines), strings.Join(lines, "\n"), strings.Join(test.down, "\n"))
			}
			for i, expected := range test.down {
				if !strings.Contains(lines[i], expected) {
					t.Errorf("line %d reads %q, want it to carry %q", i, lines[i], expected)
				}
			}
			if got := headings(frame, 12); !reflect.DeepEqual(got, test.headings) {
				t.Errorf("the pane's headings are %q, want %q", got, test.headings)
			}

			// A heading is drawn as the structure it is rather than as a row that
			// lost its dot.
			if drawn := frame.listLines(listWidth, 12)[0]; !strings.HasPrefix(drawn, opener(headingStyle)) {
				t.Errorf("the first heading is not drawn in the heading tone: %q", drawn)
			}
		})
	}
}

// The ungrouped heading separates the tail from the groups above it, so it is
// drawn only where there is something on both sides: no groups means one flat
// list with nothing over it, and everything filed means no tail to name.
func TestTheUngroupedHeadingIsDrawnOnlyWhenItSeparates(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	// No refused file: one always renders in the tail, since the key that would
	// file it is the key that could not be read (docs/specs/CONFIG.md).
	tree := groupedTree()
	tree.Broken = nil
	world.tree = tree

	for _, test := range []struct {
		name   string
		groups []entryGroup
		want   []string
	}{
		{
			name: "no groups at all",
			want: nil,
		},
		{
			name:   "every llama entry filed",
			groups: []entryGroup{{Name: "daily", Entries: []string{"air", "bark", "dust"}}},
			want:   []string{"daily"},
		},
		{
			name:   "one entry left out",
			groups: []entryGroup{{Name: "daily", Entries: []string{"air", "bark"}}},
			want:   []string{"daily", "ungrouped"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame.prefs.Groups = test.groups
			if got := headings(load(t, frame), 12); !reflect.DeepEqual(got, test.want) {
				t.Errorf("the pane's headings are %q, want %q", got, test.want)
			}
		})
	}
}

// The cursor counts entries, not lines: it never stops on a heading, so eight
// drawn lines are still four stops and the mark always sits on the row the
// detail pane is showing (docs/specs/TUI.md).
func TestTheCursorNeverLandsOnAHeading(t *testing.T) {
	frame := groupedFrame(t)

	rows := frame.rows()
	if got, want := rowIDs(rows), "air dust bark typo"; got != want {
		t.Fatalf("the grouped list holds the rows %q, want %q", got, want)
	}

	for at, listed := range rows {
		frame = frame.reselect(at)

		var marked []string
		for _, line := range list(frame, 12) {
			if strings.Contains(line, cursorMark) {
				marked = append(marked, line)
			}
		}
		if len(marked) != 1 {
			t.Fatalf("row %d is drawn with %d cursors: %q", at, len(marked), marked)
		}
		if !strings.Contains(marked[0], listed.id()) {
			t.Errorf("the cursor sits on %q, want the row for %q", marked[0], listed.id())
		}
	}

	// Moving down runs out of list at the last entry rather than at the last
	// drawn line, headings costing no presses on the way.
	frame = frame.reselect(0)
	for range len(rows) + 2 {
		frame, _ = press(t, frame, typed('j'))
	}
	if frame.selected != len(rows)-1 {
		t.Errorf("holding j left the cursor on row %d, want the last of %d entries", frame.selected, len(rows))
	}
}

// A pane too short for the list keeps the cursor's row on it and pays for the
// headings out of the same capacity: the window is taken over the drawn lines,
// so the heading a row sits under scrolls with it.
func TestAShortPaneWindowsOverTheHeadings(t *testing.T) {
	frame := groupedFrame(t)

	top := list(frame.reselect(0), 3)
	if want := []string{"daily", "air", "dust"}; !carries(top, want) {
		t.Errorf("a three-line pane at the top of the list reads %q, want %q", top, want)
	}

	bottom := list(frame.reselect(3), 3)
	if want := []string{"ungrouped", "bark", "typo"}; !carries(bottom, want) {
		t.Errorf("a three-line pane at the end of the list reads %q, want %q", bottom, want)
	}
	if !strings.Contains(bottom[len(bottom)-1], cursorMark) {
		t.Errorf("the pane scrolled the cursor's row off itself: %q", bottom)
	}
}

// carries says whether a run of drawn lines holds one expected fact each, in
// order — the way a list is read down the pane rather than matched exactly.
func carries(lines, facts []string) bool {
	if len(lines) != len(facts) {
		return false
	}
	for i, fact := range facts {
		if !strings.Contains(lines[i], fact) {
			return false
		}
	}
	return true
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
	drawn := frame.rowLine(rows[len(rows)-1], false, listWidth, idColumn(rows))
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
	column := idColumn(rows)
	if !strings.Contains(frame.rowLine(rows[0], true, listWidth, column), cursorMark) {
		t.Errorf("the selected row carries no cursor: %q", frame.rowLine(rows[0], true, listWidth, column))
	}
	if strings.Contains(frame.rowLine(rows[1], false, listWidth, column), cursorMark) {
		t.Errorf("an unselected row carries the cursor: %q", frame.rowLine(rows[1], false, listWidth, column))
	}
	assertBanded(t, frame.rowLine(rows[0], true, listWidth, column), frame.rowLine(rows[1], false, listWidth, column), listWidth)
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
	launch, err := config.Resolve(entry, config.DefaultSelection(entry))
	if err != nil {
		t.Fatalf("resolving the entry's picks: %v", err)
	}
	want, err := serve.ComposedCommand(entry, launch, world.report)
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
