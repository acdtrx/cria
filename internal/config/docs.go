package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The reference `cria docs` prints. Every key, rule and example value on the page
// is read out of the schema definitions in schema.go — the same ones the parser
// checks a file against — so a key cannot exist without appearing here
// (docs/specs/CONFIG.md, docs/cria.md principle 5).

const (
	// docsWidth is the column the page wraps at: wide enough for a rules column
	// that says something, narrow enough to read in a terminal.
	docsWidth = 88
	// docsIndent is the left margin of everything under a section heading.
	docsIndent = 2
	// columnGap separates two columns of a key table.
	columnGap = 2
)

// Docs renders the config reference: the tree layout, one table per schema, a
// complete example for each backend and for config.toml, and the one command
// that proves a freshly written entry actually serves. Plain text — it reads in
// a terminal and pastes into a coding agent's context.
func Docs() string {
	return fmt.Sprintf(docsPage,
		keyTable(entrySchema),
		keyTable(treeSchema),
		ExampleEntry(BackendLlama),
		ExampleEntry(BackendMLX),
		exampleSettings(),
	)
}

const docsPage = `cria config — the tree at ~/.config/cria

You write this tree; cria reads it and drives what it declares. cria never edits a
file here: its only writes are creating the root, models/ and AGENTS.md when they
are missing.

LAYOUT

  ~/.config/cria/
  ├── AGENTS.md      created on first run when missing
  ├── config.toml    tree-wide settings; the file itself is optional
  └── models/
      └── <id>.toml  one launchable entry per file

  An entry's id is its filename minus .toml — the name "cria start <id>" takes. An
  id holds letters, digits, '-', '_' and '.'; anything else is refused. One file is
  one launchable thing: another model is another entry, while one model run in
  variations declares them as [[choice]] axes inside its own file.

ENTRY KEYS — models/<id>.toml

%s
TREE KEYS — config.toml

%s
HOW THE TREE IS READ

  - Unknown keys and wrong types are errors, never silent defaults: a typo fails
    loudly instead of behaving like something you did not write.
  - A file cria refuses disables only itself; the report names the file and the
    offending key.
  - cria composes the model, port and host flags itself: "-hf repo:quant" for
    llama, "--model repo" for mlx, plus "--port" and "--host". An args list
    restating one of them is refused, never a silent override.
  - Everything else belongs in args, passed to the server verbatim. cria types no
    server flags of its own, so read the server's own --help for what goes there.

EXAMPLE — models/<id>.toml, backend "llama"

%s
EXAMPLE — models/<id>.toml, backend "mlx"

%s
EXAMPLE — config.toml

%s
VALIDATE WHAT YOU WROTE

  cria validate <id> [choice=option ...]

  One blocking command, and the machine ends as it began: cria stops whatever server
  holds the entry's port, starts the entry, waits until it serves, asks it for one
  real completion, stops it, and puts the displaced server back under its own picks.
  Nothing on another port is touched.

  0  it serves
  1  it does not; the last line says what failed
  2  cria refused and touched nothing — unknown entry or pick, a missing tool, or a
     port held by something it must not stop
  3  the swap was left half done; the last line says what is serving now

  The manual verbs are still there: cria start <id> [--wait] starts one and leaves it
  running, cria status --json reports pid, port, phase, health and log path, and
  cria stop <id> stops it.
`

// keyTable renders one schema as the reference table: name, type, whether the key
// is required, and the rules that govern it. Sub-table keys follow their table
// under their dotted names.
func keyTable(s schema) string {
	rows := append([]docsRow{{name: "key", kind: "type", required: "required", rules: "rules"}}, schemaRows(s, "")...)

	nameWidth, kindWidth, requiredWidth := 0, 0, 0
	for _, row := range rows {
		nameWidth = max(nameWidth, len(row.name))
		kindWidth = max(kindWidth, len(row.kind))
		requiredWidth = max(requiredWidth, len(row.required))
	}
	margin := docsIndent + nameWidth + columnGap + kindWidth + columnGap + requiredWidth + columnGap
	rulesWidth := docsWidth - margin

	var table strings.Builder
	for i, row := range rows {
		if i == 1 {
			table.WriteString(strings.Repeat(" ", docsIndent) +
				dashes(nameWidth) + strings.Repeat(" ", columnGap) +
				dashes(kindWidth) + strings.Repeat(" ", columnGap) +
				dashes(requiredWidth) + strings.Repeat(" ", columnGap) +
				dashes(rulesWidth) + "\n")
		}
		lines := wrapWords(row.rules, rulesWidth)
		table.WriteString(strings.Repeat(" ", docsIndent) +
			pad(row.name, nameWidth) + strings.Repeat(" ", columnGap) +
			pad(row.kind, kindWidth) + strings.Repeat(" ", columnGap) +
			pad(row.required, requiredWidth) + strings.Repeat(" ", columnGap) +
			lines[0] + "\n")
		for _, line := range lines[1:] {
			table.WriteString(strings.Repeat(" ", margin) + line + "\n")
		}
	}
	return table.String()
}

// docsRow is one line of a key table, already flattened: a sub-table's keys carry
// their dotted names, so the table reads as the file does.
type docsRow struct {
	name     string
	kind     string
	required string
	rules    string
}

// schemaRows flattens a schema into table rows, a table's own keys following it.
func schemaRows(s schema, prefix string) []docsRow {
	var rows []docsRow
	for _, k := range s {
		required := "no"
		if k.required {
			required = "yes"
		}
		rules := k.rules
		if k.onlyBackend != "" {
			rules = string(k.onlyBackend) + " only — " + rules
		}
		rows = append(rows, docsRow{name: prefix + k.name, kind: k.kind.String(), required: required, rules: rules})
		if k.kind.holdsKeys() {
			rows = append(rows, schemaRows(k.keys, prefix+k.name+".")...)
		}
	}
	return rows
}

// ExampleEntry renders a complete entry file for one backend: every key that
// backend takes, each under the rules that govern it. It is the template an agent
// copies, and it is built from the schema, so a new key joins the template the
// moment it is declared.
//
// It is exported because `cria new` writes it into the tree as a fresh entry
// file: the template someone is handed and the template `cria docs` teaches are
// the same string, not two copies that drift (CLAUDE.md: schema and docs are one
// source).
func ExampleEntry(backend Backend) string {
	var file strings.Builder
	writeComment(&file, fmt.Sprintf(exampleEntryPreamble, backend, entrySchema.requiredNames()))
	for _, k := range entrySchema {
		if k.kind.holdsKeys() || (k.onlyBackend != "" && k.onlyBackend != backend) {
			continue
		}
		writeExampleKey(&file, k, backend)
	}
	for _, k := range entrySchema {
		if k.kind.holdsKeys() {
			writeExampleAxis(&file, k, backend)
		}
	}
	return file.String()
}

const exampleEntryPreamble = "A complete models/<id>.toml for the %q backend: every key cria understands, " +
	"each under the rules that govern it. Required: %s. Delete the keys you do not " +
	"need — cria never rewrites this file, so the comments you leave here stay."

// writeExampleAxis writes an axis the entry could declare, commented out. An
// entry needs none — the template someone is handed has to be launchable as it
// lands — so the block shows the shape and the keys at their example values,
// ready to have the comment markers taken off.
func writeExampleAxis(file *strings.Builder, k key, backend Backend) {
	file.WriteString("\n")
	writeComment(file, strings.TrimRight(k.rules, ".")+". "+exampleAxisNote)
	file.WriteString("#\n")
	for _, line := range exampleBlockLines(k, "", "", backend) {
		if line == "" {
			file.WriteString("#\n")
			continue
		}
		file.WriteString("# " + line + "\n")
	}
}

const exampleAxisNote = "Uncomment the block below to declare one, and repeat its option table for every " +
	"further pick."

// exampleBlockLines renders one [[table]] block and the blocks nested in it as
// the plain TOML an author would write, each key at its example value. Scalar
// keys come before the nested blocks because TOML gives every key after a header
// to that header's table.
func exampleBlockLines(k key, prefix, indent string, backend Backend) []string {
	lines := []string{indent + k.header(prefix)}
	for _, sub := range k.keys {
		if sub.kind.holdsKeys() || (sub.onlyBackend != "" && sub.onlyBackend != backend) {
			continue
		}
		lines = append(lines, indent+sub.name+" = "+sub.exampleFor(backend))
	}
	for _, sub := range k.keys {
		if !sub.kind.holdsKeys() {
			continue
		}
		lines = append(lines, "")
		lines = append(lines, exampleBlockLines(sub, prefix+k.name+".", indent+"  ", backend)...)
	}
	return lines
}

// exampleSettings renders a complete config.toml. Scalar keys come before any
// table because TOML gives every key after a [table] header to that table; the
// tree settings nest one level, and a deeper table would need this to recurse.
func exampleSettings() string {
	var file strings.Builder
	writeComment(&file, exampleSettingsPreamble)
	for _, k := range treeSchema {
		if k.kind != kindTable {
			writeExampleKey(&file, k, "")
		}
	}
	for _, k := range treeSchema {
		if k.kind != kindTable {
			continue
		}
		file.WriteString("\n")
		writeComment(&file, k.rules)
		file.WriteString(k.header("") + "\n")
		for _, sub := range k.keys {
			writeExampleKey(&file, sub, "")
		}
	}
	return file.String()
}

const exampleSettingsPreamble = "A complete config.toml: the tree-wide settings, every one of them optional — " +
	"and so is the file itself."

// writeExampleKey writes one key of an example file: the rules as a comment, then
// the key at its example value.
func writeExampleKey(file *strings.Builder, k key, backend Backend) {
	file.WriteString("\n")
	writeComment(file, k.rules)
	file.WriteString(k.name + " = " + k.exampleFor(backend) + "\n")
}

// writeComment writes text as TOML comment lines, wrapped to the page width.
func writeComment(file *strings.Builder, text string) {
	for _, line := range wrapWords(text, docsWidth-len("# ")) {
		file.WriteString("# " + line + "\n")
	}
}

// requiredNames lists the keys a file must set, for an example's preamble.
func (s schema) requiredNames() string {
	var names []string
	for _, k := range s {
		if k.required {
			names = append(names, k.name)
		}
	}
	return strings.Join(names, ", ")
}

// wrapWords breaks text into lines of at most width columns, splitting on spaces
// only: a rules line quotes values that must survive intact.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
}

// pad right-pads a column value; every padded column holds ascii names only.
func pad(value string, width int) string {
	return value + strings.Repeat(" ", width-len(value))
}

func dashes(width int) string {
	return strings.Repeat("-", width)
}
