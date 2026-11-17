package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"cria/internal/hubcache"
	"cria/internal/serve"
	"cria/internal/tools"
)

// fakeSurgery is hubcache's deletion half as the cache view drives it: what a
// plan comes back with, what an execute reclaims, and — the half the flow is
// judged on — exactly what each step was asked and what serving state it was
// handed (docs/specs/CACHE.md).
type fakeSurgery struct {
	plan       *hubcache.Plan
	planErr    error
	reclaimed  int64
	executeErr error

	asked    []string            // the units a plan was asked for, in order
	planned  [][]hubcache.Served // the serving state each plan was made against
	executed []*hubcache.Plan    // the plans carried out
	guarded  [][]hubcache.Served // the serving state each execute was handed
	through  []string            // the hf each execute was handed, empty where none was found
}

func (f *fakeSurgery) surgery() surgery {
	return surgery{
		quant:      f.planQuant,
		repo:       f.planRepo,
		partials:   f.planPartials,
		superseded: f.planSuperseded,
		execute:    f.execute,
	}
}

func (f *fakeSurgery) planQuant(repo *hubcache.Repo, quant string, served []hubcache.Served) (*hubcache.Plan, error) {
	f.asked = append(f.asked, "quant "+repo.ID+":"+quant)
	f.planned = append(f.planned, served)
	return f.plan, f.planErr
}

func (f *fakeSurgery) planRepo(repo *hubcache.Repo, served []hubcache.Served) (*hubcache.Plan, error) {
	f.asked = append(f.asked, "repo "+repo.ID)
	f.planned = append(f.planned, served)
	return f.plan, f.planErr
}

func (f *fakeSurgery) planPartials(repo *hubcache.Repo, served []hubcache.Served) (*hubcache.Plan, error) {
	f.asked = append(f.asked, "partials "+repo.ID)
	f.planned = append(f.planned, served)
	return f.plan, f.planErr
}

func (f *fakeSurgery) planSuperseded(repo *hubcache.Repo, quant string, served []hubcache.Served) (*hubcache.Plan, error) {
	f.asked = append(f.asked, "superseded "+repo.ID+":"+quant)
	f.planned = append(f.planned, served)
	return f.plan, f.planErr
}

func (f *fakeSurgery) execute(_ context.Context, plan *hubcache.Plan, served []hubcache.Served, hf string) (int64, error) {
	f.executed = append(f.executed, plan)
	f.guarded = append(f.guarded, served)
	f.through = append(f.through, hf)
	return f.reclaimed, f.executeErr
}

// quantPlan is a plan as hubcache describes deleting one quantization: the
// blobs and the snapshot entries pointing at them, and a blob it cannot take
// because another file still reaches it.
func quantPlan() *hubcache.Plan {
	return &hubcache.Plan{
		Target: hubcache.Target{Kind: hubcache.TargetQuant, Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
		Removes: []hubcache.Removal{
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/aaaa", Bytes: 9 << 30},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/bbbb", Bytes: 9 << 30},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/rev/Qwen3-UD-Q4_K_XL-00001-of-00002.gguf"},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/rev/Qwen3-UD-Q4_K_XL-00002-of-00002.gguf"},
		},
		Shared: []hubcache.Shared{{
			Blob:  "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/cccc",
			Bytes: 700 << 20,
			Links: []string{"/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/rev/mmproj-BF16.gguf"},
		}},
		Bytes: 18 << 30,
	}
}

// cacheFrame is a frame standing in the cache view over the walked cache, with
// the config tree loaded — the state a delete is pressed from.
func cacheFrame(t *testing.T) (model, *testHost) {
	t.Helper()
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	world.tree, world.cache = testTree(), fullCache()
	world.surgery.plan = quantPlan()
	frame = load(t, frame)
	frame.view = viewCache
	return frame.reselect(0), world
}

// onRow moves the cache cursor onto the row whose plain text carries what, and
// fails the test when no row does.
func onRow(t *testing.T, frame model, what string) model {
	t.Helper()
	for i, listed := range frame.cacheRows() {
		if strings.Contains(plain(rowFacts(listed, paintFor(false))), what) {
			return frame.reselect(i)
		}
	}
	t.Fatalf("no cache row reads %q: %q", what, cacheList(frame, 40))
	return frame
}

// x describes the delete of the highlighted row, and the confirmation renders
// that plan and nothing else: what comes back, what goes, what has to stay, and
// which entry named it (docs/specs/CACHE.md).
func TestDeleteConfirmRendersThePlan(t *testing.T) {
	frame, world := cacheFrame(t)
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	if want := []string{"quant unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL"}; len(world.surgery.asked) != 1 || world.surgery.asked[0] != want[0] {
		t.Fatalf("x asked for %q, want %q", world.surgery.asked, want)
	}
	if frame.confirm == nil {
		t.Fatal("the plan raised no confirmation")
	}

	drawn := plain(frame.View().Content)
	for _, fact := range []string{
		"delete unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL?",
		"18.0 GiB",   // what it reclaims
		"4 paths",    // what it removes
		"cccc stays", // the blob another file still reaches
		"700.0 MiB",  // which is bytes this delete does not get back
		"mmproj-BF16.gguf",
		"referenced by entry qwen — the entry stays; its next start re-downloads",
		"y deletes it; esc leaves it alone.",
	} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the confirmation does not carry %q:\n%s", fact, drawn)
		}
	}

	// The bar reads what the confirmation offers, and nothing underneath it.
	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, "delete y delete · esc cancel") {
		t.Errorf("the keybar reads %q, want the confirmation's own keys", bar)
	}
	if strings.Contains(bar, "x delete") || strings.Contains(bar, "s stop") {
		t.Errorf("the keybar offers keys from under the confirmation: %q", bar)
	}
}

// The row a delete acts on is the unit the cursor is on: a quant, a repository
// whole, or the unfinished downloads under it (docs/specs/CACHE.md).
func TestDeletePlansTheSelectedUnit(t *testing.T) {
	cases := map[string]string{
		"UD-Q4_K_XL":                       "quant unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"unsloth/Qwen3-30B-A3B-GGUF":       "repo unsloth/Qwen3-30B-A3B-GGUF",
		"mlx-community/Qwen3-30B-A3B-4bit": "repo mlx-community/Qwen3-30B-A3B-4bit",
		"unfinished downloads":             "partials unsloth/gemma-3-27b-it-GGUF",
		// The superseded row under a quant takes that quant's old copies; the
		// one under a repo with no quants takes the repository's.
		"superseded · 2026-08-05 · 2 files": "superseded unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"superseded · 2026-08-05":           "superseded mlx-community/Qwen3-30B-A3B-4bit:",
	}

	for row, want := range cases {
		t.Run(row, func(t *testing.T) {
			frame, world := cacheFrame(t)
			frame = onRow(t, frame, row)

			_, cmd := press(t, frame, typed('x'))
			if _, ok := run(t, cmd).(plannedMsg); !ok {
				t.Fatal("x did not describe a delete")
			}
			if len(world.surgery.asked) != 1 || world.surgery.asked[0] != want {
				t.Errorf("x asked for %q, want %q", world.surgery.asked, want)
			}
		})
	}
}

// The confirmation for the copies a re-upload replaced reads like any other —
// and says what is different about it: the entry that names those bytes loses
// nothing, because what it serves is the copy that stays (docs/specs/CACHE.md).
func TestDeleteConfirmOfSupersededCopies(t *testing.T) {
	frame, world := cacheFrame(t)
	world.surgery.plan = &hubcache.Plan{
		Target: hubcache.Target{
			Kind: hubcache.TargetSuperseded, Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL",
		},
		Removes: []hubcache.Removal{
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/3333", Bytes: 4 << 30},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/4444", Bytes: 4 << 30},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/old/Qwen3-UD-Q4_K_XL-00001-of-00002.gguf"},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/old/Qwen3-UD-Q4_K_XL-00002-of-00002.gguf"},
		},
		Dirs:  []string{"/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/old"},
		Bytes: 8 << 30,
	}
	frame = onRow(t, frame, "superseded · 2026-08-05 · 2 files")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	if frame.confirm == nil {
		t.Fatal("the plan raised no confirmation")
	}
	drawn := plain(frame.View().Content)
	for _, fact := range []string{
		"delete the superseded copies of unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL?",
		"8.0 GiB",
		"4 paths, and 1 emptied directories",
		"referenced by entry qwen — the entry stays; the copy it serves stays with it",
		"y deletes it; esc leaves it alone.",
	} {
		if !strings.Contains(drawn, fact) {
			t.Errorf("the confirmation does not carry %q:\n%s", fact, drawn)
		}
	}
	if strings.Contains(drawn, "its next start re-downloads") {
		t.Errorf("the confirmation threatens a re-download the delete does not cause:\n%s", drawn)
	}
}

// confirmText is the confirmation as it reads: each line of the pane's body,
// border and padding taken off, blank lines dropped. Wide enough that no line
// wraps, so a test can hold the whole text to what it should say.
func confirmText(t *testing.T, frame model) []string {
	t.Helper()
	if frame.confirm == nil {
		t.Fatal("no confirmation is up")
	}
	drawn := strings.Split(plain(frame.confirmPanel(300, 30)), "\n")
	var text []string
	for _, line := range drawn[1 : len(drawn)-1] {
		line = strings.TrimSpace(strings.Trim(line, "│"))
		if line != "" {
			text = append(text, line)
		}
	}
	return text
}

// A delete that touches no stored file reads exactly as it always has: what it
// reclaims, what it removes, what it has to leave and who names it.
func TestDeleteConfirmOfAnUnstoredDelete(t *testing.T) {
	frame, _ := cacheFrame(t)
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	want := []string{
		"delete unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL?",
		"reclaims 18.0 GiB",
		"removes  4 paths",
		"shared   cccc stays: 700.0 MiB another file still reaches, kept by mmproj-BF16.gguf",
		"used by  referenced by entry qwen — the entry stays; its next start re-downloads",
		"y deletes it; esc leaves it alone.",
	}
	if got := confirmText(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the confirmation reads\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// storedPlan is a quant delete hf can take: its shards live in hf's shared
// store, 18 GiB of them left with no link, and two stored files another
// repository keeps and one whose manifest hf cannot read stay.
func storedPlan() *hubcache.Plan {
	return &hubcache.Plan{
		Target: hubcache.Target{Kind: hubcache.TargetQuant, Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
		Removes: []hubcache.Removal{
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/aaaa"},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/bbbb"},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/rev/Qwen3-UD-Q4_K_XL-00001-of-00002.gguf"},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/rev/Qwen3-UD-Q4_K_XL-00002-of-00002.gguf"},
		},
		Shared: []hubcache.Shared{
			{
				Blob:   "/hub/blobs/dd/dddd",
				Bytes:  700 << 20,
				Links:  []string{"/hub/models--unsloth--Qwen3-30B-A3B-Instruct-GGUF/blobs/eeee"},
				Stored: true,
				Repos:  []string{"unsloth/Qwen3-30B-A3B-Instruct-GGUF"},
			},
			{
				Blob:   "/hub/blobs/de/dede",
				Bytes:  324 << 20,
				Links:  []string{"/hub/models--unsloth--Qwen3-30B-A3B-Instruct-GGUF/blobs/efef"},
				Stored: true,
				Repos:  []string{"unsloth/Qwen3-30B-A3B-Instruct-GGUF"},
			},
			{Blob: "/hub/blobs/ff/ffff", Bytes: 1 << 30, Stored: true},
		},
		Stored: 18 << 30,
		Bytes:  2 << 20,
		HFTargets: []string{
			"hf://models/unsloth/Qwen3-30B-A3B-GGUF/Qwen3-UD-Q4_K_XL-00001-of-00002.gguf",
			"hf://models/unsloth/Qwen3-30B-A3B-GGUF/Qwen3-UD-Q4_K_XL-00002-of-00002.gguf",
		},
	}
}

// A delete hf can take runs through hf when the tool check found it: the
// confirmation counts what hf frees in its store as reclaimed and says the
// delete runs through hf. With hf not found, cria unlinks the repository's
// entries itself, and the confirmation says what that leaves in hf's shared
// store and what frees it. Either way it says what keeps each stored file it
// cannot free (docs/specs/CACHE.md, Deletion).
func TestDeleteConfirmOfAStoredDelete(t *testing.T) {
	cases := map[string]struct {
		hf      tools.Tool
		head    []string
		through string // the hf the execute is handed
	}{
		"hf found": {
			hf: tools.Tool{Name: tools.HF, Status: tools.StatusFound, Path: "/opt/homebrew/bin/hf"},
			head: []string{
				"reclaims 18.0 GiB",
				"through  `hf cache rm`, which frees what the delete leaves in hf's shared store",
			},
			through: "/opt/homebrew/bin/hf",
		},
		"hf not found": {
			hf: tools.Tool{Name: tools.HF, Status: tools.StatusMissing},
			head: []string{
				"reclaims 2.0 MiB",
				"store    18.0 GiB stays in hf's shared store; `hf cache prune` frees it (it also removes old revisions and unfinished downloads).",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			world := newTestHost(&fakeServers{})
			world.report.HF = tc.hf
			world.tree, world.cache = testTree(), fullCache()
			world.surgery.plan = storedPlan()
			frame, _, _ := testFrameOn(t, world)
			frame = load(t, frame)
			frame.view = viewCache
			frame = onRow(t, frame.reselect(0), "UD-Q4_K_XL")

			frame, cmd := press(t, frame, typed('x'))
			frame = answer(t, frame, cmd)

			want := slices.Concat([]string{"delete unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL?"}, tc.head, []string{
				"removes  4 paths",
				"shared   1.0 GiB in 2 files stays in hf's shared store, kept by unsloth/Qwen3-30B-A3B-Instruct-GGUF",
				"1.0 GiB in 1 file stays in hf's shared store with no readable .refs manifest, which hf never frees",
				"used by  referenced by entry qwen — the entry stays; its next start re-downloads",
				"y deletes it; esc leaves it alone.",
			})
			if got := confirmText(t, frame); !reflect.DeepEqual(got, want) {
				t.Errorf("the confirmation reads\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}

			_, cmd = press(t, frame, typed('y'))
			run(t, cmd)
			if want := []string{tc.through}; !reflect.DeepEqual(world.surgery.through, want) {
				t.Errorf("the execute was handed hf %q, want %q", world.surgery.through, want)
			}
		})
	}
}

// However many blobs a delete leaves behind, the confirmation still shows the
// entries it costs a download and the question it asks: the blobs give way,
// and say how many lines did.
func TestDeleteConfirmKeepsItsQuestionWithManyKeepers(t *testing.T) {
	frame, world := cacheFrame(t)
	plan := quantPlan()
	plan.Shared = nil
	for i := range 40 {
		plan.Shared = append(plan.Shared, hubcache.Shared{
			Blob:   fmt.Sprintf("/hub/blobs/%02x/%064x", i, i),
			Bytes:  1 << 30,
			Links:  []string{fmt.Sprintf("/hub/models--org--Mirror-%d/blobs/%d", i, i)},
			Stored: true,
			Repos:  []string{fmt.Sprintf("org/Mirror-%d", i)},
		})
	}
	world.surgery.plan = plan
	frame = onRow(t, frame, "UD-Q4_K_XL")
	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	// Ten rows of body: the head, the used-by line and the question take six.
	var text []string
	drawn := strings.Split(plain(frame.confirmPanel(300, 12)), "\n")
	for _, line := range drawn[1 : len(drawn)-1] {
		text = append(text, strings.TrimSpace(strings.Trim(line, "│")))
	}
	want := []string{
		"delete unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL?",
		"reclaims 18.0 GiB",
		"removes  4 paths",
		"shared   1.0 GiB in 1 file stays in hf's shared store, kept by org/Mirror-0",
		"1.0 GiB in 1 file stays in hf's shared store, kept by org/Mirror-1",
		"1.0 GiB in 1 file stays in hf's shared store, kept by org/Mirror-2",
		"… and 37 more lines",
		"used by  referenced by entry qwen — the entry stays; its next start re-downloads",
		"",
		"y deletes it; esc leaves it alone.",
	}
	if !reflect.DeepEqual(text, want) {
		t.Errorf("the confirmation reads\n%s\nwant\n%s", strings.Join(text, "\n"), strings.Join(want, "\n"))
	}

	// A pane too short for even the head still asks its question.
	short := strings.Split(plain(frame.confirmPanel(300, 6)), "\n")
	if last := strings.TrimSpace(strings.Trim(short[len(short)-2], "│")); last != "y deletes it; esc leaves it alone." {
		t.Errorf("a four-row confirmation ends %q, want the question", last)
	}

	// Blobs one repository keeps fold into one line, whatever their number.
	for i := range plan.Shared {
		plan.Shared[i].Repos = []string{"org/Mirror"}
	}
	if got := confirmText(t, frame); !slices.Contains(got, "shared   40.0 GiB in 40 files stays in hf's shared store, kept by org/Mirror") {
		t.Errorf("the confirmation reads\n%s\nwant one line for the 40 files org/Mirror keeps", strings.Join(got, "\n"))
	}
}

// The copies a re-upload replaced are cria's own delete even where they sit in
// hf's shared store, so their confirmation says what stays there — with hf
// found as without it (docs/specs/CACHE.md, Deletion).
func TestDeleteConfirmOfStoredSupersededCopies(t *testing.T) {
	frame, world := cacheFrame(t)
	world.surgery.plan = &hubcache.Plan{
		Target: hubcache.Target{Kind: hubcache.TargetSuperseded, Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"},
		Removes: []hubcache.Removal{
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/blobs/3333"},
			{Path: "/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/old/Qwen3-UD-Q4_K_XL.gguf"},
		},
		Dirs:   []string{"/hub/models--unsloth--Qwen3-30B-A3B-GGUF/snapshots/old"},
		Stored: 4 << 30,
	}
	frame = onRow(t, frame, "superseded · 2026-08-05 · 2 files")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	want := []string{
		"delete the superseded copies of unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL?",
		"reclaims 0 B",
		"store    4.0 GiB stays in hf's shared store; `hf cache prune` frees it (it also removes old revisions and unfinished downloads).",
		"removes  2 paths, and 1 emptied directories",
		"used by  referenced by entry qwen — the entry stays; the copy it serves stays with it",
		"y deletes it; esc leaves it alone.",
	}
	if got := confirmText(t, frame); !reflect.DeepEqual(got, want) {
		t.Errorf("the confirmation reads\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A model a server has open cannot be deleted, and the refusal names the server
// to stop: no confirmation is raised, because there is nothing to confirm
// (docs/specs/CACHE.md).
func TestDeleteRefusedWhileServed(t *testing.T) {
	frame, world := cacheFrame(t)
	served := hubcache.Served{Entry: "qwen", Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL"}
	world.surgery.planErr = &hubcache.ServedError{
		Target: hubcache.Target{Kind: hubcache.TargetQuant, Repo: served.Repo, Quant: served.Quant},
		Served: served,
	}
	frame.listing = serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	if frame.confirm != nil {
		t.Fatal("a refused delete raised a confirmation")
	}
	if !frame.alert.bad || !strings.Contains(frame.alert.text, "qwen is serving") || !strings.Contains(frame.alert.text, "stop it first") {
		t.Errorf("the refusal reads %q, want the running entry and the stop it asks for", frame.alert.text)
	}

	// The guard was asked against what is running right now, which is the whole
	// reason a running server refuses the delete.
	if len(world.surgery.planned) != 1 || len(world.surgery.planned[0]) != 1 || world.surgery.planned[0][0] != served {
		t.Errorf("the plan was made against %+v, want the live record %+v", world.surgery.planned, served)
	}
}

// y carries the plan out, guarded again against the servers running at that
// moment — a server started between the plan and the answer would otherwise
// have its bytes deleted under it (docs/specs/CACHE.md).
func TestConfirmedDeleteExecutesAgainstFreshServingState(t *testing.T) {
	frame, world := cacheFrame(t)
	world.surgery.reclaimed = 18 << 30
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	// Nothing was running when the plan was made; something is by the time it is
	// answered.
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}})

	frame, confirmed := press(t, frame, typed('y'))
	if frame.confirm == nil || frame.confirm.note != "deleting…" {
		t.Errorf("the confirmation says %+v while deleting, want it to name what it is doing", frame.confirm)
	}
	frame = answer(t, frame, confirmed)

	if len(world.surgery.executed) != 1 || world.surgery.executed[0] != world.surgery.plan {
		t.Fatalf("the execute carried out %v, want the plan that was shown", world.surgery.executed)
	}
	if len(world.surgery.guarded) != 1 || len(world.surgery.guarded[0]) != 1 || world.surgery.guarded[0][0].Entry != "qwen" {
		t.Errorf("the execute was guarded against %+v, want the server that started since the plan", world.surgery.guarded)
	}
	if frame.confirm != nil {
		t.Error("the confirmation stayed up after the delete")
	}
	if want := "reclaimed 18.0 GiB from unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL"; frame.alert.text != want {
		t.Errorf("the frame says %q, want %q", frame.alert.text, want)
	}
}

// A delete leaves the cache re-read: what was on screen described a tree that
// has just changed.
func TestDeleteRefreshesTheWalk(t *testing.T) {
	frame, world := cacheFrame(t)
	world.surgery.reclaimed = 1 << 20
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)
	frame, confirmed := press(t, frame, typed('y'))

	next, walk := frame.Update(run(t, confirmed))
	frame = next.(model)
	if walk == nil {
		t.Fatal("the delete did not re-read the cache")
	}
	if _, ok := walk().(entriesMsg); !ok {
		t.Errorf("the delete answered with a %T, want a fresh read of the tree and the cache", walk())
	}
}

// A cache that moved under the plan is not acted on: the plan named exact paths,
// and the answer is to show the tree as it is now (docs/specs/CACHE.md).
func TestDriftClosesTheConfirmationAndRefreshes(t *testing.T) {
	frame, world := cacheFrame(t)
	world.surgery.executeErr = &hubcache.DriftError{
		Target: quantPlan().Target,
		Change: "/hub/blobs/aaaa is no longer part of the delete",
	}
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)
	frame, confirmed := press(t, frame, typed('y'))
	next, walk := frame.Update(run(t, confirmed))
	frame = next.(model)

	if frame.confirm != nil {
		t.Error("a drifted plan stayed on screen")
	}
	if want := "the cache changed since the plan — showing it fresh"; frame.alert.text != want {
		t.Errorf("the frame says %q, want %q", frame.alert.text, want)
	}
	if walk == nil {
		t.Fatal("the drifted delete did not re-read the cache")
	}
}

// An execute that failed part-way says what stopped it and what still came back:
// those bytes are gone, and reporting nothing would be a lie about the disk.
func TestFailedDeleteReportsWhatCameBack(t *testing.T) {
	frame, world := cacheFrame(t)
	world.surgery.reclaimed = 9 << 30
	world.surgery.executeErr = errors.New("cannot remove /hub/blobs/bbbb: permission denied")
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)
	frame, confirmed := press(t, frame, typed('y'))
	frame = answer(t, frame, confirmed)

	if !frame.alert.bad || !strings.Contains(frame.alert.text, "permission denied") {
		t.Errorf("the frame says %q, want the reason the delete failed", frame.alert.text)
	}
	if !strings.Contains(frame.alert.text, "9.0 GiB came back") {
		t.Errorf("the frame says %q, want the bytes the failed delete still freed", frame.alert.text)
	}
}

// esc leaves the cache alone, and nothing underneath the confirmation acts while
// it is up.
func TestCancelledDeleteTouchesNothing(t *testing.T) {
	frame, world := cacheFrame(t)
	frame.listing = serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}
	frame = onRow(t, frame, "UD-Q4_K_XL")

	frame, cmd := press(t, frame, typed('x'))
	frame = answer(t, frame, cmd)

	frame, ignored := press(t, frame, typed('s'))
	if ignored != nil || len(world.servers.stopped) != 0 {
		t.Errorf("a stop fired from under the confirmation: %v", world.servers.stopped)
	}

	frame, _ = press(t, frame, escape)
	if frame.confirm != nil {
		t.Error("esc left the confirmation up")
	}
	if len(world.surgery.executed) != 0 {
		t.Errorf("a cancelled delete carried out %v", world.surgery.executed)
	}
}

// The delete key belongs to the cache view's selection and to nothing else: the
// entry list's selection starts a server, it does not remove one from disk.
func TestDeleteKeyIsTheCacheViewsAlone(t *testing.T) {
	frame, world := cacheFrame(t)
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, selectionScope+" x delete") {
		t.Errorf("the keybar reads %q, want the cache view's delete", bar)
	}
	if bar := plain(renderKeybar(200, frame.groups()...)); strings.Contains(bar, "⏎ start") {
		t.Errorf("the cache view offers the entry list's start: %q", bar)
	}

	// The server scope stays: stop, log and restart act on the running server
	// from anywhere (docs/specs/TUI.md).
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{Servers: []serve.Status{liveStatus(serve.PhaseRunning)}}})
	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "s stop") || !strings.Contains(bar, "l log") {
		t.Errorf("the cache view lost the server keys: %q", bar)
	}

	back, _ := press(t, frame, escape)
	if bar := plain(renderKeybar(200, back.groups()...)); strings.Contains(bar, "x delete") {
		t.Errorf("the entry list offers the cache view's delete: %q", bar)
	}
	pressed, cmd := press(t, back, typed('x'))
	if cmd != nil || len(world.surgery.asked) != 0 {
		t.Errorf("x planned %q from the entry list", world.surgery.asked)
	}
	if pressed.confirm != nil {
		t.Error("x raised a confirmation from the entry list")
	}
}

// A delete pressed on a cache cria has not walked yet does nothing: there is no
// row, so there is nothing to plan.
func TestDeleteWithNothingSelected(t *testing.T) {
	frame, world, _ := testFrameOn(t, newTestHost(&fakeServers{}))
	frame.view = viewCache
	frame = frame.reselect(0)

	if _, cmd := press(t, frame, typed('x')); cmd != nil {
		t.Error("x planned a delete with no row under the cursor")
	}
	if len(world.surgery.asked) != 0 {
		t.Errorf("x asked for %q with nothing selected", world.surgery.asked)
	}
}
