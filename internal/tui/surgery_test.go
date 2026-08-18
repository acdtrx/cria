package tui

import (
	"errors"
	"strings"
	"testing"

	"cria/internal/hubcache"
	"cria/internal/serve"
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
}

func (f *fakeSurgery) surgery() surgery {
	return surgery{quant: f.planQuant, repo: f.planRepo, partials: f.planPartials, execute: f.execute}
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

func (f *fakeSurgery) execute(plan *hubcache.Plan, served []hubcache.Served) (int64, error) {
	f.executed = append(f.executed, plan)
	f.guarded = append(f.guarded, served)
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
		if strings.Contains(plain(rowFacts(listed, false)), what) {
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

	back, _ := press(t, frame, typed('v'))
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
