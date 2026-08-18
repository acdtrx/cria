package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"cria/internal/format"
	"cria/internal/hubcache"
	"cria/internal/serve"
)

// landed is when the fixture's bytes arrived, and stale how long an abandoned
// download has been sitting there.
var (
	landed = time.Date(2026, 8, 12, 9, 30, 0, 0, time.UTC)
	stale  = time.Now().Add(-49 * time.Hour)
)

// fullCache is one walk of a hub cache holding every shape the view has to draw:
// a GGUF repo with two quants, one of them sharded; a GGUF repo an interrupted
// download left partials in; an MLX repo, which is one unit; and something cria
// cannot serve at all. Repos come back in the order the walk sorts them.
func fullCache() *hubcache.Cache {
	gemma := hubcache.Repo{
		ID: "unsloth/gemma-3-27b-it-GGUF", Type: hubcache.RepoModel, Kind: hubcache.KindGGUF,
		Dir: "/hub/models--unsloth--gemma-3-27b-it-GGUF", Revision: "b2b2b2b2",
		Items: []hubcache.Item{{
			Label: "Q4_K_M", Bytes: 16 << 30, Complete: false, Modified: landed,
			Files: []hubcache.File{{Name: "gemma-3-27b-it-Q4_K_M-00001-of-00002.gguf", Bytes: 16 << 30, Modified: landed}},
		}},
		Files: []hubcache.File{{Name: "gemma-3-27b-it-Q4_K_M-00001-of-00002.gguf", Bytes: 16 << 30, Modified: landed}},
		Partials: []hubcache.Partial{
			{Path: "/hub/models--unsloth--gemma-3-27b-it-GGUF/blobs/dddd.downloadInProgress", Bytes: 911 << 20, Modified: stale},
			{Path: "/hub/models--unsloth--gemma-3-27b-it-GGUF/blobs/eeee.incomplete", Bytes: 89 << 20, Modified: stale},
		},
		Bytes: (16 << 30) + (1000 << 20), PartialBytes: 1000 << 20, Modified: stale,
	}
	qwen := hubcache.Repo{
		ID: "unsloth/Qwen3-30B-A3B-GGUF", Type: hubcache.RepoModel, Kind: hubcache.KindGGUF,
		Dir: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF", Revision: "a1a1a1a1",
		Items: []hubcache.Item{
			{
				Label: "Q8_0", Bytes: 32 << 30, Complete: true, Modified: landed,
				Files: []hubcache.File{{Name: "Qwen3-30B-A3B-Q8_0.gguf", Bytes: 32 << 30, Modified: landed}},
			},
			{
				Label: "UD-Q4_K_XL", Bytes: 18 << 30, Complete: true, Modified: landed,
				Files: []hubcache.File{
					{Name: "Qwen3-UD-Q4_K_XL-00001-of-00002.gguf", Bytes: 9 << 30, Modified: landed},
					{Name: "Qwen3-UD-Q4_K_XL-00002-of-00002.gguf", Bytes: 9 << 30, Modified: landed},
				},
			},
		},
		Files: []hubcache.File{
			{Name: "Qwen3-30B-A3B-Q8_0.gguf", Bytes: 32 << 30, Modified: landed},
			{Name: "Qwen3-UD-Q4_K_XL-00001-of-00002.gguf", Bytes: 9 << 30, Modified: landed},
			{Name: "Qwen3-UD-Q4_K_XL-00002-of-00002.gguf", Bytes: 9 << 30, Modified: landed},
		},
		Bytes: 50 << 30, Complete: true, Modified: landed,
	}
	mlx := hubcache.Repo{
		ID: "mlx-community/Qwen3-30B-A3B-4bit", Type: hubcache.RepoModel, Kind: hubcache.KindMLX,
		Dir: "/hub/models--mlx-community--Qwen3-30B-A3B-4bit", Revision: "c3c3c3c3",
		Files: []hubcache.File{
			{Name: "model-00001-of-00002.safetensors", Bytes: 8 << 30, Modified: landed},
			{Name: "model-00002-of-00002.safetensors", Bytes: 8 << 30, Modified: landed},
		},
		Bytes: 16 << 30, Complete: true, Modified: landed,
	}
	other := hubcache.Repo{
		ID: "HuggingFaceTB/smoltalk", Type: hubcache.RepoDataset, Kind: hubcache.KindOther,
		Dir: "/hub/datasets--HuggingFaceTB--smoltalk", Revision: "d4d4d4d4",
		Files:    []hubcache.File{{Name: "data/train.parquet", Bytes: 2 << 30, Modified: landed}},
		Bytes:    2 << 30,
		Complete: true, Modified: landed,
	}

	cache := &hubcache.Cache{Root: "/home/u/.cache/huggingface/hub", Repos: []hubcache.Repo{other, mlx, qwen, gemma}}
	for _, repo := range cache.Repos {
		cache.Bytes += repo.Bytes
		cache.PartialBytes += repo.PartialBytes
	}
	return cache
}

// cacheList is the cache view's list as a person reads it, one line per row.
func cacheList(frame model, capacity int) []string {
	var lines []string
	for _, line := range frame.cacheListLines(76, capacity) {
		if trimmed := strings.TrimRight(plain(line), " "); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// cacheDetail is the pane beside the list, as plain text.
func cacheDetail(frame model) string {
	return plain(strings.Join(frame.cacheDetailLines(200, 40), "\n"))
}

// The list is everything the cache holds: repos with their quants nested under
// them, each tagged by kind, sizes right-aligned, and the header the whole thing
// is read against (docs/specs/CACHE.md).
func TestCacheListDrawsEverythingTheCacheHolds(t *testing.T) {
	frame, _ := cacheFrame(t)
	lines := cacheList(frame, 20)

	if lines[0] != "/home/u/.cache/huggingface/hub" {
		t.Errorf("the header reads %q, want the cache root", lines[0])
	}
	for _, fact := range []string{"85.0 GiB", "4 repos", "⚠ 1000.0 MiB unfinished"} {
		if !strings.Contains(lines[1], fact) {
			t.Errorf("the totals read %q, want %q", lines[1], fact)
		}
	}

	want := []string{
		"HuggingFaceTB/smoltalk  other  dataset",
		"mlx-community/Qwen3-30B-A3B-4bit  mlx",
		"unsloth/Qwen3-30B-A3B-GGUF  gguf",
		"  Q8_0",
		"  UD-Q4_K_XL",
		"unsloth/gemma-3-27b-it-GGUF  gguf",
		"  Q4_K_M  ⚠ incomplete",
		"  ⚠ 2 unfinished downloads",
	}
	rows := lines[2:]
	if len(rows) != len(want) {
		t.Fatalf("the list drew %d rows, want %d: %q", len(rows), len(want), rows)
	}
	for i, facts := range want {
		if !strings.HasPrefix(strings.TrimLeft(rows[i], " ▸"), strings.TrimLeft(facts, " ")) {
			t.Errorf("row %d reads %q, want it to start with %q", i, rows[i], facts)
		}
	}

	// Every size sits in the same column, right-aligned, so the list reads as
	// the table "where did my disk go" is answered from.
	for _, row := range rows {
		if !strings.HasSuffix(row, "iB") && !strings.HasSuffix(row, " B") {
			t.Errorf("row %q does not end in its size", row)
		}
	}
	if !strings.HasSuffix(rows[2], "50.0 GiB") || !strings.HasSuffix(rows[4], "18.0 GiB") {
		t.Errorf("the sizes are not the walk's own: %q", rows)
	}
}

// The row under the cursor is a band across the pane, the size column included,
// and the row beside it is drawn plain.
func TestCacheCursorRowIsABand(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = frame.reselect(1)

	rows := frame.cacheRows()
	column := sizeColumn(rows)
	assertBanded(t,
		frame.cacheRowLine(rows[1], true, listWidth, column),
		frame.cacheRowLine(rows[0], false, listWidth, column), listWidth)

	// The size is inside the band rather than beyond its end: the row is one
	// object, and what it occupies is part of it.
	banded := frame.cacheRowLine(rows[1], true, listWidth, column)
	if !strings.HasSuffix(plain(banded), format.Bytes(rowBytes(rows[1]))) {
		t.Errorf("the banded row does not end in its size: %q", plain(banded))
	}
}

// Names are the provider's, exactly: unsloth's UD- prefix is part of the tag the
// repo publishes, and cria never prettifies it (docs/specs/CACHE.md).
func TestCacheListSpellsNamesTheWayTheHubDoes(t *testing.T) {
	frame, _ := cacheFrame(t)
	drawn := strings.Join(cacheList(frame, 20), "\n")

	if !strings.Contains(drawn, "UD-Q4_K_XL") {
		t.Errorf("the list does not spell the tag the repo publishes:\n%s", drawn)
	}
	if strings.Contains(drawn, " Q4_K_XL") {
		t.Errorf("the list stripped a provider's prefix:\n%s", drawn)
	}
}

// The cursor moves over every row and stops at both ends, and each row is one of
// the units docs/specs/CACHE.md makes selectable.
func TestCacheSelectionWalksTheUnits(t *testing.T) {
	frame, _ := cacheFrame(t)

	kinds := []cacheRowKind{repoRow, repoRow, repoRow, itemRow, itemRow, repoRow, itemRow, partialsRow}
	for i, want := range kinds {
		frame = frame.reselect(i)
		selected, ok := frame.selectedCacheRow()
		if !ok {
			t.Fatalf("row %d has nothing under the cursor", i)
		}
		if selected.kind != want {
			t.Errorf("row %d is a %v, want %v", i, selected.kind, want)
		}
		if (selected.item != nil) != (want == itemRow) {
			t.Errorf("row %d carries item %v for a %v row", i, selected.item, want)
		}
	}

	frame = frame.reselect(len(kinds) + 5)
	if frame.cacheSelected != len(kinds)-1 {
		t.Errorf("the cursor ran past the end of the list to row %d", frame.cacheSelected)
	}
	frame = frame.reselect(-3)
	if frame.cacheSelected != 0 {
		t.Errorf("the cursor ran past the top of the list to row %d", frame.cacheSelected)
	}

	// j and k move it, and each list keeps its own cursor across a view switch.
	frame, _ = press(t, frame, typed('j'))
	frame, _ = press(t, frame, typed('j'))
	if frame.cacheSelected != 2 || frame.selected != 0 {
		t.Errorf("the cache cursor is on %d and the entry cursor on %d, want 2 and 0", frame.cacheSelected, frame.selected)
	}
	back, _ := press(t, frame, escape)
	again, _ := press(t, back, typed('c'))
	if again.cacheSelected != 2 {
		t.Errorf("the cache view came back on row %d, want the row it was left on", again.cacheSelected)
	}
}

// A list longer than the pane scrolls with the cursor rather than losing it, and
// the header stays whatever the list does.
func TestCacheListKeepsTheCursorOnScreen(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = frame.reselect(len(frame.cacheRows()) - 1)

	lines := cacheList(frame, 5)
	if len(lines) != 5 {
		t.Fatalf("a five-row pane drew %d rows: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "/home/u/.cache/huggingface/hub") {
		t.Errorf("the header scrolled away: %q", lines)
	}
	if !strings.Contains(lines[len(lines)-1], "unfinished downloads") {
		t.Errorf("the pane reads %q, want the row the cursor is on", lines)
	}
}

// The details pane is what the filesystem knows about the highlighted quant, the
// config entries that name it, and whether a server has it open right now
// (docs/specs/CACHE.md).
func TestQuantDetailsCarryTheCrossReference(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame.listing = serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}
	frame = onRow(t, frame, "UD-Q4_K_XL")

	detail := cacheDetail(frame)
	for _, fact := range []string{
		"unsloth/Qwen3-30B-A3B-GGUF",
		"UD-Q4_K_XL",
		"18.0 GiB",
		"a1a1a1a1",
		"2026-08-12 09:30:00",
		"Qwen3-UD-Q4_K_XL-00001-of-00002.gguf · 9.0 GiB",
		"Qwen3-UD-Q4_K_XL-00002-of-00002.gguf · 9.0 GiB",
		"entry qwen",
		"qwen (pid 4242) — stop it before deleting",
	} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the details pane does not carry %q:\n%s", fact, detail)
		}
	}
}

// A quant nothing names says so: the two questions a delete raises are answered
// either way, and "no entry references it" is the answer that makes a delete
// easy.
func TestQuantDetailsOfAnUnreferencedQuant(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = onRow(t, frame, "Q8_0")

	detail := cacheDetail(frame)
	for _, fact := range []string{"no config entry references it", "nothing is serving it right now"} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the details pane does not carry %q:\n%s", fact, detail)
		}
	}
	if strings.Contains(detail, "entry qwen") {
		t.Errorf("the details pane credits another quant's entry to this one:\n%s", detail)
	}
}

// An MLX repo is one unit — the quantization is the repo — so its details are
// the repository's, cross-referenced the same way.
func TestRepoDetailsOfAnMLXModel(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = onRow(t, frame, "mlx-community/Qwen3-30B-A3B-4bit")

	detail := cacheDetail(frame)
	for _, fact := range []string{
		"mlx-community/Qwen3-30B-A3B-4bit",
		"mlx",
		"16.0 GiB",
		"c3c3c3c3",
		"model-00001-of-00002.safetensors · 8.0 GiB",
		"entry mlx-qwen",
	} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the details pane does not carry %q:\n%s", fact, detail)
		}
	}
}

// A repo cria cannot serve is still disk, and the view says what it is rather
// than hiding it (docs/specs/CACHE.md).
func TestRepoDetailsOfSomethingCriaCannotServe(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = onRow(t, frame, "HuggingFaceTB/smoltalk")

	detail := cacheDetail(frame)
	for _, fact := range []string{"other", "dataset", "2.0 GiB", "data/train.parquet"} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the details pane does not carry %q:\n%s", fact, detail)
		}
	}
}

// A partials row is the unfinished downloads themselves: what they are, what
// they hold, and how long they have been holding it.
func TestPartialsDetailsCarryTheFilesAndTheirAges(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = onRow(t, frame, "unfinished downloads")

	detail := cacheDetail(frame)
	for _, fact := range []string{
		"unsloth/gemma-3-27b-it-GGUF",
		"2 unfinished downloads",
		"1000.0 MiB",
		"dddd.downloadInProgress · 911.0 MiB · 2d ago",
		"eeee.incomplete · 89.0 MiB · 2d ago",
	} {
		if !strings.Contains(detail, fact) {
			t.Errorf("the partials pane does not carry %q:\n%s", fact, detail)
		}
	}
}

// An empty cache says so, and says where it looked: a fresh host has nothing
// downloaded yet, which is a state rather than a failure.
func TestEmptyCacheNamesItsRoot(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.cache = &hubcache.Cache{Root: "/home/u/.cache/huggingface/hub"}
	frame = load(t, frame)
	frame.view = viewCache
	frame = frame.reselect(0)

	drawn := plain(frame.View().Content)
	for _, fact := range []string{"the hub cache holds nothing yet", "/home/u/.cache/huggingface/hub"} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the empty cache view does not say %q:\n%s", fact, drawn)
		}
	}
	if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "x delete") {
		t.Errorf("the keybar offers a delete with nothing to delete: %q", bar)
	}
}

// A cache cria has not walked yet claims nothing about what is on disk.
func TestUnwalkedCacheClaimsNothing(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.cacheErr = errUnreadableCache
	frame = load(t, frame)
	frame.view = viewCache

	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "reading the hub cache…") {
		t.Errorf("the cache view claims to know what is on disk:\n%s", drawn)
	}
	if !strings.Contains(drawn, "cannot read the model cache") {
		t.Errorf("the frame does not report the cache it could not read:\n%s", drawn)
	}
}

// The details pane sits beside the list where there is width for both and under
// it where there is not, exactly as the serve view does.
func TestCacheViewStacksOnANarrowTerminal(t *testing.T) {
	frame, _ := cacheFrame(t)

	wide := plain(frame.cacheScreen(120, 14))
	if !strings.Contains(strings.Split(wide, "\n")[0], cacheDetailTitle) {
		t.Errorf("a 120-cell terminal does not draw the details pane beside the list:\n%s", wide)
	}
	for _, line := range strings.Split(wide, "\n") {
		if lipgloss.Width(line) != 120 {
			t.Errorf("a side-by-side line is %d cells wide, want 120: %q", lipgloss.Width(line), line)
		}
	}

	narrow := plain(frame.cacheScreen(70, 14))
	if strings.Contains(strings.Split(narrow, "\n")[0], cacheDetailTitle) {
		t.Errorf("a 70-cell terminal kept the details pane beside the list:\n%s", narrow)
	}
	if !strings.Contains(narrow, cacheDetailTitle) || !strings.Contains(narrow, "unsloth/Qwen3-30B-A3B-GGUF") {
		t.Errorf("a 70-cell terminal lost half the cache view:\n%s", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if lipgloss.Width(line) != 70 {
			t.Errorf("a stacked line is %d cells wide, want 70: %q", lipgloss.Width(line), line)
		}
	}
}

// The status box is in this view too, and the walk behind both lists is one
// walk: the cache view is the entry list's dots drawn out (docs/specs/TUI.md).
func TestCacheViewKeepsTheStatusBox(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}})

	drawn := plain(frame.View().Content)
	for _, fact := range []string{statusTitle, "qwen", "running", cacheTitle, "unsloth/Qwen3-30B-A3B-GGUF"} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the cache view does not draw %q:\n%s", fact, drawn)
		}
	}
}
