package hubcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Deleting a single quant out of a multi-quant repo is the one cache operation
// the ecosystem lacks (docs/cria.md, principle 2), and it happens in two steps by
// construction: a plan states exactly what would go, what comes back and what it
// leaves behind, and Execute removes exactly that set and reports what really
// came back. The confirmation a user answers renders a plan and nothing else
// computes one, so what was shown and what is removed cannot be two different
// things (docs/specs/CACHE.md).

// TargetKind names what a plan deletes: the units docs/specs/CACHE.md makes
// deletable.
type TargetKind int

const (
	TargetQuant    TargetKind = iota // one quantization of a GGUF repo, its shards folded in
	TargetRepo                       // a whole repository directory
	TargetPartial                    // one unfinished download
	TargetPartials                   // every unfinished download of one repository
)

// String names the kind the way a confirmation spells it.
func (k TargetKind) String() string {
	switch k {
	case TargetQuant:
		return "quant"
	case TargetRepo:
		return "repo"
	case TargetPartial:
		return "partial"
	case TargetPartials:
		return "partials"
	default:
		return fmt.Sprintf("TargetKind(%d)", int(k))
	}
}

// Target is what a plan deletes. It travels with the plan because Execute
// re-derives it against the cache as it stands at that moment: a plan is a
// description of a tree, and the tree is allowed to change under it.
type Target struct {
	Kind  TargetKind
	Repo  string // the repository, as the Hub spells it
	Quant string // the item's label, for a quant target
	Path  string // the unfinished download, for a single-partial target
}

// String names the target the way a refusal and a confirmation name it.
func (t Target) String() string {
	switch t.Kind {
	case TargetQuant:
		return t.Repo + ":" + t.Quant
	case TargetPartial:
		return t.Path
	case TargetPartials:
		return "the unfinished downloads of " + t.Repo
	default:
		return t.Repo
	}
}

// Plan is one delete, described before anything is touched: every path that
// goes, every directory those removals leave empty, the blobs the delete cannot
// take, and the bytes it reclaims.
type Plan struct {
	Target  Target
	Removes []Removal // every file and snapshot entry that goes, ordered by path
	Dirs    []string  // the directories those removals leave empty, deepest first
	Shared  []Shared  // blobs left in place because something else still reaches them
	Bytes   int64     // what the removals return to the disk

	dir      string   // the repository directory, for Execute's re-derivation
	repoType RepoType // likewise: readRepo needs the namespace back
}

// Removal is one path a plan removes and what removing it returns to the disk.
// A snapshot entry is a symlink and returns nothing of its own — the bytes are
// the blob's, counted where they live, exactly as the walk counts them.
type Removal struct {
	Path  string
	Bytes int64
}

// Shared is a blob a plan leaves behind: another snapshot entry still reaches it
// once this delete's links are gone, so removing it would take bytes the cache
// still needs (docs/specs/CACHE.md).
type Shared struct {
	Blob  string   // the blob that stays
	Bytes int64    // what staying costs: bytes this delete does not reclaim
	Links []string // the entries that keep it, the ones this plan does not remove
}

// Served is one model a running server has open right now. The caller assembles
// the list — surgery does not import the serve layer, so the dependency graph
// stays acyclic (CODING-RULES §7).
type Served struct {
	Entry string // the entry id, so a refusal names the server to stop
	Repo  string // the repository the server was launched against
	Quant string // llama only; empty when the server serves a whole repo or picks the repo's own default
}

// ServedError refuses a delete because a running server has those bytes mapped.
// Deleting under it frees no space and invites confusion, so the refusal names
// the server to stop first (docs/specs/CACHE.md).
type ServedError struct {
	Target Target
	Served Served
}

func (e *ServedError) Error() string {
	model := e.Served.Repo
	if e.Served.Quant != "" {
		model += ":" + e.Served.Quant
	}
	return fmt.Sprintf("%s cannot be deleted: %s is serving %s; stop it first", e.Target, e.Served.Entry, model)
}

// DriftError refuses an execute because the cache is no longer the one the plan
// was made against. A plan names exact paths and exact bytes; acting on a tree
// that has moved on would delete something nobody was shown.
type DriftError struct {
	Target Target
	Change string // what no longer matches
}

func (e *DriftError) Error() string {
	return fmt.Sprintf("the cache changed since the plan for %s was made — re-plan: %s", e.Target, e.Change)
}

// PlanQuant describes deleting one quantization of a GGUF repo: its snapshot
// entries in every snapshot that names it, and every blob behind them that
// nothing else still reaches. The quant is found the way a config entry finds
// it — exactly, case aside (docs/specs/CACHE.md).
//
// Deleting the repo's last quant takes the skeleton with it: a repository
// directory holding nothing but refs, empty snapshots and an empty blobs
// directory is disk with no model in it.
func PlanQuant(repo *Repo, quant string, served []Served) (*Plan, error) {
	item, found := repo.Item(quant)
	if !found {
		return nil, fmt.Errorf("%s holds no %q; it holds %s", repo.ID, quant, strings.Join(itemLabels(repo), ", "))
	}
	target := Target{Kind: TargetQuant, Repo: repo.ID, Quant: item.Label}
	if err := guard(target, served); err != nil {
		return nil, err
	}

	entries, err := repoLinks(repo)
	if err != nil {
		return nil, err
	}
	ours, others := splitByItem(entries, item.Label)
	if len(ours) == 0 {
		return nil, fmt.Errorf("%s: no snapshot entry names %s any more", repo.ID, item.Label)
	}

	// Last means the repository is left with nothing: no other item, no file
	// that is not one, and no download still in flight. Anything remaining —
	// even a README — keeps the skeleton, because the repo still holds it.
	if len(others) == 0 && len(repo.Partials) == 0 {
		return repoPlan(repo, target)
	}

	removes, shared := quantRemovals(ours, others)
	dirs, err := emptiedDirs(filepath.Join(repo.Dir, "snapshots"), pathsOf(removes))
	if err != nil {
		return nil, err
	}
	return newPlan(repo, target, removes, dirs, shared), nil
}

// PlanRepo describes deleting a whole repository directory — how an MLX model
// and anything else the cache holds are deleted, each being one unit
// (docs/specs/CACHE.md). Everything under the directory goes: snapshots, blobs,
// refs, the unfinished downloads, and the bookkeeping the cache keeps beside
// them.
func PlanRepo(repo *Repo, served []Served) (*Plan, error) {
	target := Target{Kind: TargetRepo, Repo: repo.ID}
	// No quant is named, so every server on this repository is serving
	// something the delete would take.
	if err := guard(target, served); err != nil {
		return nil, err
	}
	return repoPlan(repo, target)
}

// PlanPartial describes deleting one unfinished download.
func PlanPartial(repo *Repo, path string, served []Served) (*Plan, error) {
	var partial *Partial
	for i := range repo.Partials {
		if repo.Partials[i].Path == path {
			partial = &repo.Partials[i]
			break
		}
	}
	if partial == nil {
		return nil, fmt.Errorf("%s holds no unfinished download at %s", repo.ID, path)
	}
	target := Target{Kind: TargetPartial, Repo: repo.ID, Path: path}
	// A partial has no identity beyond the repository it is landing in — its name
	// is a hash and no snapshot reaches it — so cria cannot tell a leftover from
	// the bytes a running server is fetching right now. The repository is what is
	// guarded, and the refusal names the server to stop.
	if err := guard(target, served); err != nil {
		return nil, err
	}
	return newPlan(repo, target, []Removal{{Path: partial.Path, Bytes: partial.Bytes}}, nil, nil), nil
}

// PlanPartials describes reclaiming every unfinished download of one repository
// at once.
func PlanPartials(repo *Repo, served []Served) (*Plan, error) {
	if len(repo.Partials) == 0 {
		return nil, fmt.Errorf("%s holds no unfinished downloads", repo.ID)
	}
	target := Target{Kind: TargetPartials, Repo: repo.ID}
	// Guarded by repository, for the reason PlanPartial gives.
	if err := guard(target, served); err != nil {
		return nil, err
	}
	removes := make([]Removal, 0, len(repo.Partials))
	for _, partial := range repo.Partials {
		removes = append(removes, Removal{Path: partial.Path, Bytes: partial.Bytes})
	}
	return newPlan(repo, target, removes, nil, nil), nil
}

// Execute carries out a plan and reports the bytes that actually came back.
//
// The plan is re-derived against the cache first and the two must describe the
// same removal: a plan is a promise about exact paths, and the confirmation the
// user answered was that promise. Anything else — a snapshot that appeared and
// now shares one of the blobs, a link already gone, a file that changed size —
// is refused rather than acted on, because the plan no longer describes what is
// there.
//
// The serving guard runs again here, against the servers running at this
// moment: a plan is made before a confirmation and carried out after it, and a
// server started in between would otherwise have its bytes deleted under it.
// The caller holds fresh serving state at the moment it executes, so passing it
// closes that window with no machinery of its own.
//
// Directories go last and only through os.Remove, which refuses a directory
// that is not empty: a delete can never take something the plan did not name.
// A failure part-way returns the bytes reclaimed so far with the error — those
// bytes are gone, and reporting zero would be a lie about the disk.
func Execute(plan *Plan, served []Served) (int64, error) {
	current, err := replan(plan)
	if err != nil {
		return 0, err
	}
	if change := difference(plan, current); change != "" {
		return 0, &DriftError{Target: plan.Target, Change: change}
	}
	if err := guard(plan.Target, served); err != nil {
		return 0, err
	}

	var reclaimed int64
	for _, removal := range plan.Removes {
		if err := os.Remove(removal.Path); err != nil {
			return reclaimed, fmt.Errorf("cannot remove %s: %w", removal.Path, err)
		}
		reclaimed += removal.Bytes
	}
	for _, dir := range plan.Dirs {
		if err := os.Remove(dir); err != nil {
			return reclaimed, fmt.Errorf("cannot remove the emptied directory %s: %w", dir, err)
		}
	}
	return reclaimed, nil
}

// guard refuses a delete whose bytes a running server has open. The match is the
// one docs/specs/CACHE.md settles for every name in this package: exact, case
// aside — llama.cpp's own `-hf repo:TAG` resolution ignores case, and nothing
// else about a provider's spelling is reinterpreted.
//
// An empty quant on either side widens the guard to the whole repository. A
// target that names none is taking everything the repository holds; a server
// that named none is serving the file llama-server chose, and cria cannot know
// which one that is — guarding the repository is the only honest answer, and
// guessing a quant would be the dishonest one.
//
// The target is the whole question, which is what lets Execute ask it again
// against fresher serving state than the plan was made with.
func guard(target Target, served []Served) error {
	for _, server := range served {
		if !strings.EqualFold(server.Repo, target.Repo) {
			continue
		}
		if target.Quant == "" || server.Quant == "" || strings.EqualFold(server.Quant, target.Quant) {
			return &ServedError{Target: target, Served: server}
		}
	}
	return nil
}

// repoPlan describes removing a whole repository directory: every entry under
// it, and every directory they leave behind — the repository's own included.
func repoPlan(repo *Repo, target Target) (*Plan, error) {
	removes, err := treeRemovals(repo.Dir)
	if err != nil {
		return nil, err
	}
	dirs, err := emptiedDirs(repo.Dir, pathsOf(removes))
	if err != nil {
		return nil, err
	}
	return newPlan(repo, target, removes, dirs, nil), nil
}

// newPlan assembles a plan and totals what it reclaims, so no caller sums the
// bytes a second time and gets a different number.
func newPlan(repo *Repo, target Target, removes []Removal, dirs []string, shared []Shared) *Plan {
	plan := &Plan{
		Target:   target,
		Removes:  removes,
		Dirs:     dirs,
		Shared:   shared,
		dir:      repo.Dir,
		repoType: repo.Type,
	}
	for _, removal := range removes {
		plan.Bytes += removal.Bytes
	}
	return plan
}

// quantRemovals works out what deleting one item's links takes and what it has
// to leave. Refcounting is done over the snapshot entries themselves rather than
// the walk's blob-deduplicated files: the same file linked from several
// snapshots is one blob whose links all go, while two different names sharing a
// blob are two files, and the second one's bytes are not this delete's to take.
func quantRemovals(ours, others []snapshotEntry) ([]Removal, []Shared) {
	keepers := map[string][]string{}
	for _, entry := range others {
		keepers[entry.blob] = append(keepers[entry.blob], entry.link)
	}

	blobs := map[string]int64{}
	links := map[string]bool{}
	kept := map[string]bool{}
	var shared []Shared
	for _, entry := range ours {
		if held := keepers[entry.blob]; len(held) > 0 {
			if !kept[entry.blob] {
				kept[entry.blob] = true
				shared = append(shared, Shared{Blob: entry.blob, Bytes: entry.bytes, Links: held})
			}
		} else {
			blobs[entry.blob] = entry.bytes
		}
		// Where the cache holds a copy instead of a symlink the entry is the
		// blob, and a blob that stays keeps its own entry with it.
		if entry.link != entry.blob {
			links[entry.link] = true
		}
	}

	removes := make([]Removal, 0, len(blobs)+len(links))
	for blob, bytes := range blobs {
		removes = append(removes, Removal{Path: blob, Bytes: bytes})
	}
	for link := range links {
		if _, isBlob := blobs[link]; isBlob {
			continue
		}
		removes = append(removes, Removal{Path: link})
	}
	sort.Slice(removes, func(i, j int) bool { return removes[i].Path < removes[j].Path })
	sort.Slice(shared, func(i, j int) bool { return shared[i].Blob < shared[j].Blob })
	return removes, shared
}

// treeRemovals lists every entry under a directory that a delete unlinks —
// files, symlinks and all — with the bytes each returns. Only regular files hold
// bytes, which is the same accounting the walk and du do (docs/specs/CACHE.md).
func treeRemovals(dir string) ([]Removal, error) {
	var removals []Removal
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		removal := Removal{Path: path}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			removal.Bytes = info.Size()
		}
		removals = append(removals, removal)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot read the cached repository at %s: %w", dir, err)
	}
	return removals, nil
}

// emptiedDirs lists the directories under root that a removal set leaves with
// nothing in them, deepest first — the order os.Remove needs, and the order that
// keeps every step of an execute a removal of something empty. A directory
// holding anything the plan does not remove — another quant, a dangling entry —
// keeps it and stays.
func emptiedDirs(root string, removed map[string]bool) ([]string, error) {
	var dirs []string
	var empties func(dir string) (bool, error)
	empties = func(dir string) (bool, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, fmt.Errorf("cannot read %s: %w", dir, err)
		}
		empty := true
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				gone, err := empties(path)
				if err != nil {
					return false, err
				}
				if !gone {
					empty = false
				}
				continue
			}
			if !removed[path] {
				empty = false
			}
		}
		if empty {
			dirs = append(dirs, dir)
		}
		return empty, nil
	}

	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if _, err := empties(root); err != nil {
		return nil, err
	}
	return dirs, nil
}

// repoLinks lists every snapshot entry of a repository, across every revision:
// what a delete unlinks, and what it has to count references over. Entries whose
// blob is gone resolve to nothing and are left out — they reference no bytes,
// so they neither hold a blob back nor return any.
func repoLinks(repo *Repo) ([]snapshotEntry, error) {
	snapshots := filepath.Join(repo.Dir, "snapshots")
	revisions, err := snapshotRevisions(snapshots, repo.Revision)
	if err != nil {
		return nil, err
	}
	var entries []snapshotEntry
	for _, revision := range revisions {
		found, err := readSnapshot(filepath.Join(snapshots, revision))
		if err != nil {
			return nil, err
		}
		entries = append(entries, found...)
	}
	return entries, nil
}

// splitByItem sorts a repository's snapshot entries into the ones that make up
// one item and everything else. The rule is GGUFItem's — the same one the walk
// groups items by — so a plan removes exactly the entries the view shows under
// that row.
func splitByItem(entries []snapshotEntry, label string) (ours, others []snapshotEntry) {
	for _, entry := range entries {
		if name, isGGUF := GGUFItem(entry.name); isGGUF && name == label {
			ours = append(ours, entry)
			continue
		}
		others = append(others, entry)
	}
	return ours, others
}

// replan derives the plan again from the cache as it stands now, which is what
// Execute compares against. It is the one place a stored target is turned back
// into a plan, and every failure on the way is drift: the repository, the item
// or the partial the plan named is no longer there to remove.
func replan(plan *Plan) (*Plan, error) {
	repo, err := readRepo(plan.dir, plan.repoType, plan.Target.Repo)
	if err != nil {
		return nil, &DriftError{Target: plan.Target, Change: err.Error()}
	}

	var current *Plan
	switch plan.Target.Kind {
	case TargetQuant:
		current, err = PlanQuant(&repo, plan.Target.Quant, nil)
	case TargetRepo:
		current, err = PlanRepo(&repo, nil)
	case TargetPartial:
		current, err = PlanPartial(&repo, plan.Target.Path, nil)
	case TargetPartials:
		current, err = PlanPartials(&repo, nil)
	default:
		return nil, fmt.Errorf("cannot re-derive a %s plan", plan.Target.Kind)
	}
	if err != nil {
		return nil, &DriftError{Target: plan.Target, Change: err.Error()}
	}
	return current, nil
}

// difference names the first thing about a plan the cache no longer agrees
// with, or "" when the two describe the same delete. The name is what the
// refusal carries, so it says what changed rather than that something did.
func difference(planned, current *Plan) string {
	now := map[string]int64{}
	for _, removal := range current.Removes {
		now[removal.Path] = removal.Bytes
	}
	for _, removal := range planned.Removes {
		bytes, still := now[removal.Path]
		if !still {
			return fmt.Sprintf("%s is no longer part of the delete", removal.Path)
		}
		if bytes != removal.Bytes {
			return fmt.Sprintf("%s holds %d bytes, not the %d the plan counted", removal.Path, bytes, removal.Bytes)
		}
	}
	was := pathsOf(planned.Removes)
	for _, removal := range current.Removes {
		if !was[removal.Path] {
			return fmt.Sprintf("%s would now be removed too", removal.Path)
		}
	}

	keeping := map[string]bool{}
	for _, shared := range planned.Shared {
		keeping[shared.Blob] = true
	}
	for _, shared := range current.Shared {
		if !keeping[shared.Blob] {
			return fmt.Sprintf("%s is now shared with %s", shared.Blob, strings.Join(shared.Links, ", "))
		}
	}
	if len(current.Shared) != len(planned.Shared) {
		return "a blob the plan left behind is no longer shared"
	}

	if strings.Join(current.Dirs, "\n") != strings.Join(planned.Dirs, "\n") {
		return "the directories the delete would leave empty are not the ones it planned for"
	}
	return ""
}

// pathsOf is a removal set as a set, for the membership questions the directory
// walk and the drift check ask.
func pathsOf(removals []Removal) map[string]bool {
	paths := map[string]bool{}
	for _, removal := range removals {
		paths[removal.Path] = true
	}
	return paths
}
