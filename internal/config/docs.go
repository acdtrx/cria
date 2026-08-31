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
// complete example for each backend, for each engine and for config.toml, and
// the one command that proves a freshly written entry actually serves. Plain
// text — it reads in a terminal and pastes into a coding agent's context.
func Docs() string {
	return fmt.Sprintf(docsPage,
		keyTable(entrySchema),
		keyTable(engineSchema),
		keyTable(treeSchema),
		composedKeysNote(),
		entryExamples(),
		engineExamples(),
		exampleSettings(),
	)
}

// entryExamples is one complete entry file per backend, each under its own
// heading. It walks the registry: a backend cria serves is a backend the page
// teaches, without this file knowing which they are.
func entryExamples() string {
	var sections strings.Builder
	for _, backend := range Backends() {
		sections.WriteString(fmt.Sprintf(exampleEntrySection, backend, ExampleEntry(backend)))
	}
	return sections.String()
}

// engineExamples is one complete engine file per backend, walked from the same
// registry.
func engineExamples() string {
	var sections strings.Builder
	for _, backend := range Backends() {
		sections.WriteString(fmt.Sprintf(exampleEngineSection, backend, exampleEngine(backend)))
	}
	return sections.String()
}

// The headings each example file sits under.
const (
	exampleEntrySection  = "EXAMPLE — models/<id>.toml, backend %q\n\n%s\n"
	exampleEngineSection = "EXAMPLE — engines/%s.toml\n\n%s\n"
)

// composedKeysNote lists the args keys cria composes itself, each with the
// backend it belongs to. It reads the same registry the parser refuses them
// from, so the page cannot name a different set than the one a file is held to.
func composedKeysNote() string {
	named := make([]string, 0, len(backends)+2)
	for _, backend := range backends {
		named = append(named, fmt.Sprintf("%s (%s)", backend.modelKey, backend.id))
	}
	return strings.Join(append(named, "host", "port"), ", ")
}

const docsPage = `cria config — the tree at ~/.config/cria

You write this tree; cria reads it and drives what it declares. cria never edits a
file here: its only writes are creating the root, models/ and AGENTS.md when they
are missing.

LAYOUT

  ~/.config/cria/
  ├── AGENTS.md            created on first run when missing
  ├── config.toml          tree-wide settings; the file itself is optional
  ├── engines/
  │   └── <engine>.toml    what this machine serves every entry of one backend
  │                        with; optional, one file per backend
  └── models/
      └── <id>.toml        one launchable entry per file

  An entry's id is its filename minus .toml — the name "cria start <id>" takes. An
  id holds letters, digits, '-', '_' and '.'; anything else is refused. One file is
  one launchable thing: another model is another entry, while one model run in
  variations declares them as [[choice]] axes inside its own file.

ENTRY KEYS — models/<id>.toml

%s
ENGINE KEYS — engines/<engine>.toml

%s
TREE KEYS — config.toml

%s
ARGS ARE KEYS

  - One element of an args list is one flag: "key = value", where the key is the
    server's own long option written without its dashes. cria splits on the first
    '=' and passes both halves on untouched.
  - A one-letter key is spelled with one dash and anything longer with two, so
    write the long option: "gpu-layers = 99" rather than the "-ngl" alias, which
    would reach the server as "--ngl" and be refused by name at startup.
  - "key = true" is a flag that takes no value. Every other value is passed
    exactly as written, so a flag with an off switch takes the word the server
    itself takes for it: "flash-attn = off".
  - Write the list one line to a key when a value deserves a comment:

      args = [
        # 262144 tokens, the whole window for a single slot
        "c = 262144",
        "parallel = 1",
      ]

  - One list may not set a key twice. Across levels the same key is an override
    and the more specific level wins: engines/<engine>.toml first, then the
    entry, then the options its picks land on. Two options of different choices
    may not set one key — both are picked at once, so there is no winner.
  - cria composes the model reference, the host and the port itself from the keys
    above, so args may not set them: %s.

HOW THE TREE IS READ

  - Unknown keys and wrong types are errors, never silent defaults: a typo fails
    loudly instead of behaving like something you did not write.
  - An entry file cria refuses disables only itself; the report names the file and
    the offending key. config.toml and the engine files are read by every entry
    they govern, so a broken one is reported and nothing loads until it is fixed.
  - Everything a server takes beyond the composed keys belongs in args. cria types
    no server flags of its own, so read the server's own --help for what goes
    there.

%s%sEXAMPLE — config.toml

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
		rules := k.backendNote() + k.rules
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
		if k.kind.holdsKeys() || !k.takenBy(backend) {
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
		if sub.kind.holdsKeys() || !sub.takenBy(backend) {
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

// exampleEngine renders a complete engines/<engine>.toml for one backend: what
// this machine would serve every entry of that backend with. Built from the same
// definitions the parser checks the file against, like every other example here.
func exampleEngine(backend Backend) string {
	var file strings.Builder
	writeComment(&file, fmt.Sprintf(exampleEnginePreamble, backend, backend))
	for _, k := range engineSchema {
		writeExampleKey(&file, k, backend)
	}
	return file.String()
}

const exampleEnginePreamble = "A complete engines/%s.toml: what this machine serves every %q entry with. " +
	"The file is optional and so is every key in it — without one, entries carry their own args alone."

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
