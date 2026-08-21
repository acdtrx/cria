package tui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cria/internal/config"
)

// groupedTree is a tree with entries under both backends and one refused file —
// the shapes the sections have to sort out. The entries are in the order a load
// yields them, alphabetical by id, which is the order a section renders in.
func groupedTree() *config.Tree {
	root := "/home/u/.config/cria"
	entry := func(id string, backend config.Backend) config.Entry {
		return config.Entry{
			ID: id, Path: root + "/models/" + id + ".toml", Backend: backend,
			Repo: "org/" + id, Port: 8080, Host: "0.0.0.0", Name: id,
		}
	}
	return &config.Tree{
		Root:     root,
		Settings: config.Settings{DefaultPort: 8080},
		Entries: []config.Entry{
			entry("air", config.BackendLlama),
			entry("bark", config.BackendLlama),
			entry("cliff", config.BackendMLX),
			entry("dust", config.BackendLlama),
			entry("echo", config.BackendMLX),
		},
		Broken: []config.BrokenEntry{{
			ID:   "typo",
			Path: root + "/models/typo.toml",
			Err:  &config.KeyError{Key: "prot", Reason: "unknown key"},
		}},
	}
}

// groupedPrefs files that tree: a group holding llama entries out of tree order
// plus an id whose file is gone, a group whose only member is an mlx entry, a
// group standing empty, and one holding nothing but a dangling id.
func groupedPrefs() []entryGroup {
	return []entryGroup{
		{Name: "daily", Entries: []string{"dust", "air", "missing"}},
		{Name: "mlx only", Entries: []string{"cliff"}},
		{Name: "emptied", Entries: []string{}},
		{Name: "ghosts", Entries: []string{"gone"}},
	}
}

// layout is the sections as a person reads them down the pane: each section's
// name, whether its heading is drawn (+ or -), and the ids under it.
func layout(sections []section) []string {
	lines := make([]string, 0, len(sections))
	for _, listed := range sections {
		name := listed.name
		if name == "" {
			name = "ungrouped"
		}
		mark := "-"
		if listed.heading {
			mark = "+"
		}
		lines = append(lines, fmt.Sprintf("%s%s [%s]", name, mark, rowIDs(listed.rows)))
	}
	return lines
}

// rowIDs is a run of rows read across, ids only.
func rowIDs(rows []row) string {
	var ids []string
	for _, listed := range rows {
		ids = append(ids, listed.id())
	}
	return strings.Join(ids, " ")
}

// The list's whole shape: groups in the order the preferences hold them and the
// ungrouped entries last; each backend showing only its own members; entries in
// tree order inside a section whatever order they were filed in; ids with no
// entry behind them skipped; refused files last under both backends; and the
// headings drawn only where they mean something (docs/specs/TUI.md).
func TestSectionsLayOutTheEntryList(t *testing.T) {
	cases := []struct {
		name    string
		groups  []entryGroup
		backend config.Backend
		want    []string
	}{
		{
			name:    "no groups is the flat list, with no heading over it",
			backend: config.BackendLlama,
			want:    []string{"ungrouped- [air bark dust typo]"},
		},
		{
			name:    "no groups, the other backend",
			backend: config.BackendMLX,
			want:    []string{"ungrouped- [cliff echo typo]"},
		},
		{
			name:    "groups in their own order, ungrouped trailing",
			groups:  groupedPrefs(),
			backend: config.BackendLlama,
			// daily was filed dust-then-air and holds an id whose file is gone;
			// "mlx only" has a member, just not one this backend can show.
			want: []string{"daily+ [air dust]", "mlx only- []", "emptied+ []", "ghosts+ []", "ungrouped+ [bark typo]"},
		},
		{
			name:    "the same headings over the other backend's members",
			groups:  groupedPrefs(),
			backend: config.BackendMLX,
			want:    []string{"daily- []", "mlx only+ [cliff]", "emptied+ []", "ghosts+ []", "ungrouped+ [echo typo]"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := layout(entrySections(groupedTree(), test.groups, test.backend))
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("the list lays out as\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(test.want, "\n"))
			}
		})
	}
}

// The rows the cursor moves over are the sections concatenated: one order, so a
// selection index and the drawn list cannot disagree.
func TestEntryRowsAreTheSectionsConcatenated(t *testing.T) {
	tree, groups := groupedTree(), groupedPrefs()
	sequence := map[config.Backend]string{
		config.BackendLlama: "air dust bark typo",
		config.BackendMLX:   "cliff echo typo",
	}

	for backend, want := range sequence {
		rows := entryRows(tree, groups, backend)
		if got := rowIDs(rows); got != want {
			t.Errorf("the %s rows read %q, want %q", backend, got, want)
		}

		var concatenated []row
		for _, listed := range entrySections(tree, groups, backend) {
			concatenated = append(concatenated, listed.rows...)
		}
		if !reflect.DeepEqual(rows, concatenated) {
			t.Errorf("the %s rows are not the sections in order: %q against %q", backend, rowIDs(rows), rowIDs(concatenated))
		}
	}
}

// With no groups defined the list is exactly the list cria drew before groups
// existed — same entries, same order, the refused file last under either
// backend.
func TestWithoutGroupsTheRowsAreTheListAsItWas(t *testing.T) {
	frame, _ := serveFrame(t)

	for _, backend := range []config.Backend{config.BackendLlama, config.BackendMLX} {
		frame.prefs.Backend = backend
		want := frame.rows()
		got := entryRows(frame.tree, frame.prefs.Groups, backend)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the %s rows read %q, want the list's own %q", backend, rowIDs(got), rowIDs(want))
		}
	}
}

// A tree cria has not read yet has no list to lay out, and says so rather than
// answering with an empty one.
func TestAnUnreadTreeHasNoSections(t *testing.T) {
	if sections := entrySections(nil, groupedPrefs(), config.BackendLlama); sections != nil {
		t.Errorf("an unread tree laid out %v", sections)
	}
	if rows := entryRows(nil, groupedPrefs(), config.BackendLlama); rows != nil {
		t.Errorf("an unread tree drew %d rows", len(rows))
	}
}

// A write records the memberships that still mean something: an id whose entry
// file is gone goes, and the group it was in stays — an empty group is a thing
// the user can still file into.
func TestPruneDropsGoneIdsAndKeepsEveryGroup(t *testing.T) {
	pruned := pruneGroups(groupedPrefs(), groupedTree())
	want := []entryGroup{
		{Name: "daily", Entries: []string{"dust", "air"}},
		{Name: "mlx only", Entries: []string{"cliff"}},
		{Name: "emptied", Entries: []string{}},
		{Name: "ghosts", Entries: []string{}},
	}
	if !reflect.DeepEqual(pruned, want) {
		t.Errorf("the pruned groups are %+v, want %+v", pruned, want)
	}
}

// Pruning answers with lists of its own. The preferences are passed around by
// value, so a prune in place would edit a group list another copy is still
// reading.
func TestPruneLeavesItsArgumentAlone(t *testing.T) {
	// The first group loses nothing — the case where a filter in place would
	// hand back the argument's own array.
	groups := []entryGroup{
		{Name: "daily", Entries: []string{"air", "bark"}},
		{Name: "tests", Entries: []string{"dust", "gone"}},
	}
	before := []entryGroup{
		{Name: "daily", Entries: []string{"air", "bark"}},
		{Name: "tests", Entries: []string{"dust", "gone"}},
	}

	pruned := pruneGroups(groups, groupedTree())
	pruned[0].Entries[0] = "clobbered"
	pruned[1].Entries = append(pruned[1].Entries, "clobbered")

	if !reflect.DeepEqual(groups, before) {
		t.Errorf("pruning edited what it was handed: %+v, want %+v", groups, before)
	}
}

// A group left holding nothing is written as an empty list, not a null: the file
// says the group is empty, not that cria has no idea what is in it.
func TestPrunedEmptyGroupsAreWrittenAsEmptyLists(t *testing.T) {
	saved := prefs{Backend: config.BackendLlama, Groups: pruneGroups(groupedPrefs(), groupedTree())}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("encoding the preferences: %v", err)
	}
	if !strings.Contains(string(data), `{"name":"ghosts","entries":[]}`) {
		t.Errorf("the emptied group is written as %s", data)
	}
}

// Pruning against a tree cria has not read yet would empty every group, which is
// not what an unread tree means.
func TestPruneWithoutATreeDropsNothing(t *testing.T) {
	pruned := pruneGroups(groupedPrefs(), nil)
	if !reflect.DeepEqual(pruned, groupedPrefs()) {
		t.Errorf("pruning against an unread tree gave %+v, want the groups untouched", pruned)
	}
}
