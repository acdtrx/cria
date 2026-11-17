package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"cria/internal/format"
	"cria/internal/hubcache"
	"cria/internal/serve"
)

// This file is the cache view's one write: deleting what the cursor is on.
//
// The two steps are hubcache's, and the frame keeps them apart exactly as it
// received them (docs/specs/CACHE.md). A plan states what would go before
// anything is touched; the confirmation renders that plan and nothing else, so
// what was shown and what is removed cannot be two different things. Both run
// off the UI thread: planning reads every snapshot of a repository, and an
// execute can unlink tens of gigabytes.

// deletion is a delete waiting for its answer: the plan the user is being shown,
// and what the last keypress in it did.
type deletion struct {
	plan *hubcache.Plan
	note string // empty until the delete is running
}

// The answers the two steps come back with.
type (
	// plannedMsg is one attempt to describe a delete: the plan, or the refusal
	// that stopped it — a running server holding those bytes, above all.
	plannedMsg struct {
		plan *hubcache.Plan
		err  error
	}
	// deletedMsg is one execute: what actually came back to the disk, and what
	// stopped it if anything did.
	deletedMsg struct {
		target    hubcache.Target
		reclaimed int64
		err       error
	}
)

// deleteSelected is x: describe deleting the highlighted row.
//
// Nothing is removed here — this is the plan the confirmation renders. The
// serving guard runs inside it, against the servers running right now, and a
// refusal is one line rather than a modal: what it asks for is a stop, and the
// key that does that is already on the bar (docs/specs/CACHE.md).
func (m model) deleteSelected() (tea.Model, tea.Cmd) {
	selected, ok := m.selectedCacheRow()
	if !ok {
		return m, nil
	}

	plan := m.planner(selected)
	served := m.servedNow()
	return m, func() tea.Msg {
		made, err := plan(served)
		return plannedMsg{plan: made, err: err}
	}
}

// planner is the delete the highlighted row calls for: the unit
// docs/specs/CACHE.md makes deletable, and the hubcache call that describes
// taking it. A GGUF repo's header row plans the whole repository — the row is
// the repository, and deleting it takes every quant under it.
func (m model) planner(selected cacheRow) func([]hubcache.Served) (*hubcache.Plan, error) {
	switch selected.kind {
	case itemRow:
		return func(served []hubcache.Served) (*hubcache.Plan, error) {
			return m.host.surgery.quant(selected.repo, selected.item.Label, served)
		}
	case partialsRow:
		return func(served []hubcache.Served) (*hubcache.Plan, error) {
			return m.host.surgery.partials(selected.repo, served)
		}
	case supersededRow:
		// The row selects one item's old copies where it sits under an item, and
		// the whole repository's where the repository is the unit.
		label := ""
		if selected.item != nil {
			label = selected.item.Label
		}
		return func(served []hubcache.Served) (*hubcache.Plan, error) {
			return m.host.surgery.superseded(selected.repo, label, served)
		}
	}
	return func(served []hubcache.Served) (*hubcache.Plan, error) {
		return m.host.surgery.repo(selected.repo, served)
	}
}

// servedNow is what the running servers have open, as the guard takes it. It is
// built fresh at every step that asks — once for the plan, once again for the
// execute — because the whole point of the second guard is that a server may
// have started in between (docs/specs/CACHE.md).
//
// A downloading server counts: those are exactly the bytes it is writing.
func (m model) servedNow() []hubcache.Served {
	var served []hubcache.Served
	for _, status := range m.listing.Servers {
		if status.Phase == serve.PhaseExited {
			continue
		}
		served = append(served, hubcache.Served{Entry: status.EntryID, Repo: status.Repo, Quant: status.Quant})
	}
	return served
}

// planned takes a plan's answer: the confirmation, or the reason there is
// nothing to confirm.
func (m model) planned(msg plannedMsg) model {
	if msg.err != nil {
		m.alert = alert{text: msg.err.Error(), bad: true}
		return m
	}
	m.confirm = &deletion{plan: msg.plan}
	m.alert = alert{}
	return m
}

// pressInConfirm is the keyboard while a delete is being confirmed: do it, or
// leave it alone. Nothing else acts — every other key would act on the cache
// this plan describes, and it is about to describe it differently.
func (m model) pressInConfirm(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.cancelDelete):
		m.confirm = nil
		return m, nil
	case key.Matches(pressed, m.keys.confirmDelete):
		return m.executeConfirmed()
	}
	return m, nil
}

// executeConfirmed is y: carry out the plan on screen.
//
// The serving state handed to Execute is read at this moment rather than
// reused from the plan: the plan was made before the confirmation and is being
// carried out after it, and a server started in between would otherwise have
// its bytes deleted under it (docs/specs/CACHE.md).
func (m model) executeConfirmed() (tea.Model, tea.Cmd) {
	if m.confirm == nil {
		return m, nil
	}

	deleting := *m.confirm
	deleting.note = "deleting…"
	m.confirm = &deleting

	plan, served := deleting.plan, m.servedNow()
	execute := m.host.surgery.execute
	return m, func() tea.Msg {
		reclaimed, err := execute(plan, served)
		return deletedMsg{target: plan.Target, reclaimed: reclaimed, err: err}
	}
}

// deleted takes the execute's answer and re-reads the cache either way: what is
// on screen described a tree that has just changed, or a tree that had already
// changed, and both are answered by walking it again.
func (m model) deleted(msg deletedMsg) (model, tea.Cmd) {
	m.confirm = nil

	var drift *hubcache.DriftError
	switch {
	case errors.As(msg.err, &drift):
		m.alert = alert{text: "the cache changed since the plan — showing it fresh"}
	case msg.err != nil:
		text := msg.err.Error()
		// A failure part-way still freed what it had already unlinked, and
		// reporting nothing would be a lie about the disk.
		if msg.reclaimed > 0 {
			text += fmt.Sprintf(" (%s came back before it stopped)", format.Bytes(msg.reclaimed))
		}
		m.alert = alert{text: text, bad: true}
	default:
		m.alert = alert{text: fmt.Sprintf("reclaimed %s from %s", format.Bytes(msg.reclaimed), msg.target)}
	}
	return m, m.readEntries
}

// confirmPanel renders the plan, and only the plan: the bytes it returns, what
// it leaves in hf's shared store, the paths it removes, the blobs it has to
// leave behind, and the entries that name what is about to go
// (docs/specs/CACHE.md).
//
// The blobs left behind are the one part that can run long, so they are what
// gives way when the pane is short: the entries a delete costs a download and
// the question it asks always stay on screen.
func (m model) confirmPanel(width, rows int) string {
	plan := m.confirm.plan
	inner := width - 4

	head := &details{inner: inner}
	head.lines = []string{noticeStyle.Render("delete " + plan.Target.String() + "?")}
	head.add("reclaims", format.Bytes(plan.Bytes), factStyle)
	if plan.Stored > 0 {
		// cria unlinks only the repository's own entries; the bytes they led to
		// in hf's store are hf's to free (docs/specs/CACHE.md, Deletion).
		head.add("store", fmt.Sprintf("%s stays in hf's shared store; `hf cache prune` frees it "+
			"(it also removes old revisions and unfinished downloads).", format.Bytes(plan.Stored)), noticeStyle)
	}
	head.add("removes", removalWords(plan), factStyle)

	kept := &details{inner: inner}
	for i, words := range sharedWords(plan.Shared) {
		label := "shared"
		if i > 0 {
			label = ""
		}
		kept.add(label, words, quietStyle)
	}

	used := &details{inner: inner}
	for i, id := range m.entriesUsing(plan.Target.Repo, plan.Target.Quant) {
		label := "used by"
		if i > 0 {
			label = ""
		}
		used.add(label, fmt.Sprintf("referenced by entry %s — %s", id, entryFate(plan.Target)), noticeStyle)
	}

	tail := []string{"", quietStyle.Render("y deletes it; esc leaves it alone.")}
	if m.confirm.note != "" {
		tail = append(tail, noticeStyle.Render(m.confirm.note))
	}

	capacity := rows - 2
	room := capacity - len(head.lines) - len(used.lines) - len(tail)
	body := slices.Concat(head.lines, fitLines(kept.lines, room, inner), used.lines)
	if over := len(body) + len(tail) - capacity; over > 0 {
		body = body[:max(len(body)-over, 0)]
	}
	return pane(paneTitle(deleteScope), width, sizeLines(append(body, tail...), capacity))
}

// fitLines keeps what fits of a run of detail lines in room lines, the last of
// them saying how many did not.
func fitLines(lines []string, room, inner int) []string {
	if len(lines) <= room {
		return lines
	}
	if room < 1 {
		return nil
	}
	cut := &details{inner: inner}
	cut.add("", fmt.Sprintf("… and %d more lines", len(lines)-room+1), quietStyle)
	return append(slices.Clone(lines[:room-1]), cut.lines[0])
}

// sharedWords says, of the blobs a plan leaves behind, what each costs and what
// keeps it. A blob of this repository is named with the snapshot entries that
// keep it. Payloads in hf's shared store are told by who keeps them — one line
// per set of other repositories still linking them, and one for those whose
// manifest hf cannot read, which it never frees — since a large repository can
// leave hundreds, and their hashes say nothing.
func sharedWords(shared []hubcache.Shared) []string {
	type group struct {
		files int
		bytes int64
	}
	var words, order []string
	stored := map[string]*group{}
	unlisted := &group{}
	for _, blob := range shared {
		switch {
		case !blob.Stored:
			words = append(words, fmt.Sprintf("%s stays: %s another file still reaches, kept by %s",
				filepath.Base(blob.Blob), format.Bytes(blob.Bytes), strings.Join(baseNames(blob.Links), ", ")))
		case len(blob.Links) == 0:
			unlisted.files++
			unlisted.bytes += blob.Bytes
		default:
			keepers := strings.Join(blob.Repos, ", ")
			if stored[keepers] == nil {
				stored[keepers] = &group{}
				order = append(order, keepers)
			}
			stored[keepers].files++
			stored[keepers].bytes += blob.Bytes
		}
	}
	for _, keepers := range order {
		kept := stored[keepers]
		words = append(words, fmt.Sprintf("%s in %s stays in hf's shared store, kept by %s",
			format.Bytes(kept.bytes), files(kept.files), keepers))
	}
	if unlisted.files > 0 {
		words = append(words, fmt.Sprintf("%s in %s stays in hf's shared store with no readable .refs manifest, which hf never frees",
			format.Bytes(unlisted.bytes), files(unlisted.files)))
	}
	return words
}

// files counts files the way a sentence does.
func files(count int) string {
	if count == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", count)
}

// entryFate is what a delete leaves the config entries that name those bytes.
// Taking a model costs the entry its next start a download; taking the copies a
// re-upload replaced costs it nothing at all — what it serves is the copy that
// stays (docs/specs/CACHE.md).
func entryFate(target hubcache.Target) string {
	if target.Kind == hubcache.TargetSuperseded {
		return "the entry stays; the copy it serves stays with it"
	}
	return "the entry stays; its next start re-downloads"
}

// removalWords is what a plan takes off the disk, counted the way it removes
// them: every path it unlinks — blobs and the snapshot entries pointing at them
// — and the directories those removals leave empty.
func removalWords(plan *hubcache.Plan) string {
	words := fmt.Sprintf("%d paths", len(plan.Removes))
	if len(plan.Dirs) > 0 {
		words += fmt.Sprintf(", and %d emptied directories", len(plan.Dirs))
	}
	return words
}

// baseNames is a set of paths as the names that identify them: a blob is kept by
// a snapshot entry, and the entry's file name is what says which file that is.
func baseNames(paths []string) []string {
	names := make([]string, len(paths))
	for i, path := range paths {
		names[i] = filepath.Base(path)
	}
	return names
}
