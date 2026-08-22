package cli

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"cria/internal/config"
	"cria/internal/format"
	"cria/internal/picks"
)

// entriesDirName is the directory the config tree keeps entries in, named here
// only to point someone at an empty one (docs/specs/CONFIG.md).
const entriesDirName = "models"

// list runs `cria list [--paths]`: what the config tree declares, one line per
// entry.
//
// It asks the tree and the picks store, and nothing else. Whether an entry could
// start right now depends on a tool check, a port and a process table — three
// questions with their own commands — and answering them here would turn a
// listing into a slow, partial `cria status` (docs/specs/CLI.md). Which option
// is picked on each axis is not one of them: it is a file cria wrote itself, and
// it is half of what an entry with choices declares.
//
// The exit code is about the read, not the contents: a tree that loaded exits
// zero however little it holds. An empty tree is a true answer to "what is
// declared", and a broken entry disables only itself (docs/specs/CONFIG.md) —
// it is listed with its reason rather than costing the whole listing its code.
func (a *app) list(args []string) int {
	rest, withPaths, unknown := splitFlag(args, pathsFlag)
	if unknown != "" {
		return a.usage("list: unknown flag %s; usage: cria list [%s]", unknown, pathsFlag)
	}
	if len(rest) > 0 {
		return a.usage("list: takes no arguments (got %s); usage: cria list [%s]",
			strings.Join(rest, ", "), pathsFlag)
	}

	tree, err := a.tree()
	if err != nil {
		return a.fail("list: %v", err)
	}
	if len(tree.Entries) == 0 && len(tree.Broken) == 0 {
		a.printf("no entries: write one in %s; `cria docs` prints the schema and a complete example\n",
			filepath.Join(tree.Root, entriesDirName))
		return exitOK
	}

	rows := make([][]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		row := []string{entry.ID, string(entry.Backend), format.HubReference(entry.Repo, entry.Quant), strconv.Itoa(entry.Port)}
		if withPaths {
			row = append(row, entry.Path)
		}
		rows = append(rows, row)
	}
	// The store is read only when there is an axis to show a pick on: a tree of
	// flat entries is listed exactly as it always was, without going near cria's
	// state.
	var stored picks.Picks
	if declaresChoices(tree) {
		stored = a.storedPicks()
	}
	for i, line := range aligned(rows) {
		a.printf("%s\n", line)
		for _, axis := range choiceLines(tree.Entries[i], stored) {
			a.printf("  %s\n", axis)
		}
	}

	// The refused files come last, under the entries that loaded, with their ids
	// in the same column: a listing is read down the ids, and a broken entry is
	// still an entry someone declared (docs/specs/CONFIG.md).
	for _, broken := range tree.Broken {
		line := pad(broken.ID, idColumn(tree)) + "  refused: " + broken.Err.Error()
		if withPaths {
			line += "  " + broken.Path
		}
		a.printf("%s\n", line)
	}
	return exitOK
}

// declaresChoices answers whether any entry in the tree has an axis to pick
// along.
func declaresChoices(tree *config.Tree) bool {
	return slices.ContainsFunc(tree.Entries, func(entry config.Entry) bool { return len(entry.Choices) > 0 })
}

// choiceLines is what an entry's axes read as under its row: one line per
// choice, indented under the id, naming the choice and its options in file order
// with the current pick marked — `quant: q4* q6 q8`. A flat entry has none, and
// its listing is the one line it always was.
//
// The line is two answers in one. Its names are the vocabulary
// `cria start <id> choice=option` takes, and its mark is what a bare
// `cria start <id>` would compose with (docs/specs/CLI.md).
//
// Which option is current is picks.Merge's rule and nobody else's: the stored
// pick while the entry still holds it, the config default — the first option —
// once the entry has moved on. With no explicit pick to refuse there is nothing
// here the merge can turn down, so its refusal is not a case this has to
// answer; an axis it somehow declined to resolve is listed unmarked rather than
// marked wrongly.
func choiceLines(entry config.Entry, stored picks.Picks) []string {
	if len(entry.Choices) == 0 {
		return nil
	}
	current, _ := picks.Merge(entry, stored[entry.ID], nil)

	lines := make([]string, 0, len(entry.Choices))
	for _, choice := range entry.Choices {
		var line strings.Builder
		line.WriteString(choice.Name + ":")
		for _, option := range choice.Options {
			line.WriteString(" " + option.Name)
			if option.Name == current[choice.Name] {
				line.WriteString("*")
			}
		}
		lines = append(lines, line.String())
	}
	return lines
}

// aligned pads every cell to its column's widest, so ids, backends and models
// line up under each other and a row is read across rather than parsed. The last
// cell of a row is left as it is: it has nothing to line up against, and padding
// it would only put invisible spaces at the end of every line.
func aligned(rows [][]string) []string {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			for i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			if i == len(row)-1 {
				line.WriteString(cell)
				continue
			}
			line.WriteString(pad(cell, widths[i]))
		}
		lines = append(lines, line.String())
	}
	return lines
}

// pad widens one cell to its column.
func pad(cell string, width int) string {
	return cell + strings.Repeat(" ", max(width-utf8.RuneCountInString(cell), 0))
}

// idColumn is how wide the id column is across the whole listing, the entries
// that loaded and the files that were refused alike.
func idColumn(tree *config.Tree) int {
	width := 0
	for _, entry := range tree.Entries {
		width = max(width, utf8.RuneCountInString(entry.ID))
	}
	for _, broken := range tree.Broken {
		width = max(width, utf8.RuneCountInString(broken.ID))
	}
	return width
}
