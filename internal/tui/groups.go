package tui

import "cria/internal/config"

// This file is the entry list's shape: which heading each entry renders under,
// in what order, and which headings are drawn at all. It is pure — a tree, the
// groups the preferences hold and the active backend go in, sections come out —
// so the list the cursor moves over and the list the pane draws are computed
// once, from the same call (docs/specs/TUI.md).
//
// Groups span backends. Membership is a UI preference and the tree knows nothing
// about it, so a group holds ids: ones the tree no longer has an entry for are
// skipped here and dropped on the next preferences write.

// section is one heading's worth of the list: the group it stands for — the
// empty name is the ungrouped tail — the rows that render under it, and whether
// its heading is drawn at all.
type section struct {
	name    string
	rows    []row
	heading bool
}

// entrySections is the whole entry list, laid out: one section per group in the
// order the preferences put them in, the ungrouped entries last. Only groups are
// ordered by hand; inside a section the entries keep the tree's own order, which
// is alphabetical by id.
func entrySections(tree *config.Tree, groups []entryGroup, backend config.Backend) []section {
	if tree == nil {
		return nil
	}

	filed := make(map[string]bool)
	for _, group := range groups {
		for _, id := range group.Entries {
			filed[id] = true
		}
	}

	sections := make([]section, 0, len(groups)+1)
	for _, group := range groups {
		sections = append(sections, groupSection(tree, group, backend))
	}
	return append(sections, ungroupedSection(tree, filed, len(groups) > 0, backend))
}

// entryRows is the sections concatenated: the flat list the cursor indexes into.
// Selection is a position in this sequence and the drawn list is these same rows
// with headings woven between them, so there is one order, computed once.
func entryRows(tree *config.Tree, groups []entryGroup, backend config.Backend) []row {
	var rows []row
	for _, listed := range entrySections(tree, groups, backend) {
		rows = append(rows, listed.rows...)
	}
	return rows
}

// groupSection is one group as this backend sees it.
//
// A heading with nothing under it is noise, so a group whose members are all in
// the other backend hides — except when the group holds no entry the tree knows
// at all, where the heading is the only trace of it left: a just-emptied group
// stays findable rather than vanishing the moment its last entry moves out
// (docs/specs/TUI.md).
func groupSection(tree *config.Tree, group entryGroup, backend config.Backend) section {
	members := make(map[string]bool, len(group.Entries))
	for _, id := range group.Entries {
		members[id] = true
	}

	listed := section{name: group.Name}
	present := false
	for _, entry := range tree.Entries {
		if !members[entry.ID] {
			continue
		}
		present = true
		if entry.Backend != backend {
			continue
		}
		listed.rows = append(listed.rows, row{entry: entry})
	}
	listed.heading = len(listed.rows) > 0 || !present
	return listed
}

// ungroupedSection is everything no group named, and it is where the refused
// entry files land: filing one is impossible — the key that would sort it under
// a group is exactly the key that could not be read — so they stay last, under
// both backends, as they always have (docs/specs/CONFIG.md).
//
// Its heading only appears once there is a group to tell it apart from: with no
// groups defined the list is one flat list with nothing above it.
func ungroupedSection(tree *config.Tree, filed map[string]bool, grouped bool, backend config.Backend) section {
	listed := section{heading: grouped}
	for _, entry := range tree.Entries {
		if filed[entry.ID] || entry.Backend != backend {
			continue
		}
		listed.rows = append(listed.rows, row{entry: entry})
	}
	for i := range tree.Broken {
		listed.rows = append(listed.rows, row{broken: &tree.Broken[i]})
	}
	return listed
}

// pruneGroups is the group list as the preferences should record it now: ids the
// tree no longer has an entry for are dropped, and no group is ever dropped with
// them — an empty group is a legal thing, findable until it is filed into or
// disbanded.
//
// Nothing of the argument is kept: preferences travel by value through this
// package, so several copies share one backing array and a prune in place would
// edit lists still being read elsewhere. An emptied group keeps an empty list
// rather than a nil one, so the file records it as [] and not null.
func pruneGroups(groups []entryGroup, tree *config.Tree) []entryGroup {
	if len(groups) == 0 {
		return nil
	}

	// A tree cria has not read yet has no entry to match against, and pruning
	// every membership away is not what "not read yet" means.
	var known map[string]bool
	if tree != nil {
		known = make(map[string]bool, len(tree.Entries))
		for _, entry := range tree.Entries {
			known[entry.ID] = true
		}
	}

	pruned := make([]entryGroup, 0, len(groups))
	for _, group := range groups {
		kept := make([]string, 0, len(group.Entries))
		for _, id := range group.Entries {
			if tree == nil || known[id] {
				kept = append(kept, id)
			}
		}
		pruned = append(pruned, entryGroup{Name: group.Name, Entries: kept})
	}
	return pruned
}
