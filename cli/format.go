package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/unixsuperhero/ochazuke"
)

// Formats are the values of the shell's --format: json (the default, the
// shell's JSON output unchanged), table (plain-text columns, read from the
// same column declarations a list draws) and markdown (a GitHub table).
//
// markdown, exactly: per table a header line "| title | title |", a rule
// line with "---" per column ("---:" for a right-aligned one), then one
// line per row "| cell | cell |"; nothing is padded or cut. In a title or
// cell a backslash is written \\ and a | is \|, a line break (\n, \r\n
// or \r) is <br>, any other control character a space. With several
// tables each starts with "## NOUN" and a blank line, and a blank line
// separates them.
//
// toon is Token-Oriented Object Notation for uniform rows, the agent-facing
// format (the rule o-qbt's --format toon follows), exactly: per table a
// header "NOUN[N]{title,title,...}:" (N rows; a table with no noun has
// none, "[N]{...}:"), then one line per row, indented two spaces, its
// values joined by ",". A value, a title or the noun is bare unless it
// contains a ",", a double quote, a newline or a carriage return; then it
// is wrapped in double quotes with \ " newline and carriage return written
// \\ \" \n \r. Nothing else is escaped: a bare value is its text (a
// backslash in it is literal), a number-like string stays bare (a reader
// types values by column), an empty or missing value is empty. Several
// tables follow one another with no blank line.
//
//	pane[2]{pane,state,cwd}:
//	  work/claude,idle,~/proj
//	  "notes, old",done,~
var Formats = []string{"json", "table", "markdown", "toon"}

// A tabular result is an array (a JSON array, or Lines) whose elements are
// all JSON objects: a list verb's rows ({id, label, data}), a state verb's
// Rows, bindings list's records. Its rows are grouped by noun: a row's own
// noun field, else the noun the verb declares its rows belong to
// (NounBuilder.Rows), else none; each group is one table, in the order its
// first row came.
//
// A group's columns are its noun's declared columns (NounBuilder.Columns)
// when any of its rows carries cells (Row.Cells); a row without cells
// shows its label in the first column, as a List draws it. Otherwise the
// columns are the rows' fields in first-seen order: the noun field is the
// group and is left out, id and label lead, and a data object's fields
// stand in for data, last (named data.KEY when a top-level field has the
// same name). A string is
// its text, null and a missing field are empty, a number or boolean is its
// JSON literal, an object or array is its compact JSON. A field column of
// numbers only is right-aligned.

// table is one group of a tabular result, as --format table, markdown and
// toon print it: the shell output formats over a noun's declared columns.
type table struct {
	noun string
	cols []ochazuke.Column
	rows [][]string
}

// record is a JSON object with its keys in their written order.
type record struct {
	keys []string
	vals map[string]json.RawMessage
}

// tablesOf groups value's rows into tables, false when value is not
// tabular. rowsNoun is the noun the verb declares its rows belong to.
func tablesOf(r *ochazuke.Runtime, rowsNoun string, value any) ([]table, bool) {
	recs, ok := recordsOf(value)
	if !ok {
		return nil, false
	}
	var order []string
	groups := map[string][]record{}
	for _, rec := range recs {
		noun := rowsNoun
		if s, ok := stringOf(rec.vals["noun"]); ok {
			noun = s
		}
		if _, seen := groups[noun]; !seen {
			order = append(order, noun)
		}
		groups[noun] = append(groups[noun], rec)
	}
	if len(recs) == 0 {
		order = []string{rowsNoun}
	}
	tables := make([]table, 0, len(order))
	for _, noun := range order {
		var declared []ochazuke.Column
		if n := r.Nouns[noun]; n != nil {
			declared = n.Columns
		}
		tables = append(tables, tableOf(noun, declared, groups[noun]))
	}
	return tables, true
}

// recordsOf decodes value as an array of objects, false when it is not.
func recordsOf(value any) ([]record, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		return nil, false
	}
	recs := make([]record, len(items))
	for i, item := range items {
		rec, ok := objectOf(item)
		if !ok {
			return nil, false
		}
		recs[i] = rec
	}
	return recs, true
}

// objectOf decodes raw as an object keeping its key order, false when it
// is not one.
func objectOf(raw json.RawMessage) (record, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return record{}, false
	}
	rec := record{vals: map[string]json.RawMessage{}}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return record{}, false
		}
		key, _ := t.(string)
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return record{}, false
		}
		if _, seen := rec.vals[key]; !seen {
			rec.keys = append(rec.keys, key)
		}
		rec.vals[key] = v
	}
	return rec, true
}

// stringOf is raw as a string, false when it is not a JSON string.
func stringOf(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// textOf is a JSON value as a cell's text: a string's text, "" for null or
// nothing, the compact JSON of anything else (a number's literal).
func textOf(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if s, ok := stringOf(raw); ok {
		return s
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// isNumber reports whether raw is a JSON number.
func isNumber(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !(raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
		return false
	}
	var n json.Number
	return json.Unmarshal(raw, &n) == nil
}

// tableOf is one group's table: the declared columns over the rows' cells
// when a row has cells, else the rows' fields.
func tableOf(noun string, declared []ochazuke.Column, recs []record) table {
	t := table{noun: noun}
	hasCells := slices.ContainsFunc(recs, func(rec record) bool {
		var cells []json.RawMessage
		return json.Unmarshal(rec.vals["cells"], &cells) == nil && len(cells) > 0
	})
	if len(declared) > 0 && (hasCells || len(recs) == 0) {
		t.cols = slices.Clone(declared)
		for _, rec := range recs {
			var cells []json.RawMessage
			json.Unmarshal(rec.vals["cells"], &cells)
			row := make([]string, len(declared))
			if len(cells) == 0 {
				row[0] = textOf(rec.vals["label"])
			}
			for i := range min(len(cells), len(row)) {
				row[i] = textOf(cells[i])
			}
			t.rows = append(t.rows, row)
		}
		return t
	}
	// A field column reads key, or data's sub when sub is set.
	type field struct{ key, sub string }
	top := map[string]bool{}
	for _, rec := range recs {
		for _, k := range rec.keys {
			top[k] = true
		}
	}
	var fields []field
	var names []string
	add := func(f field, name string) {
		if !slices.Contains(names, name) {
			fields, names = append(fields, f), append(names, name)
		}
	}
	// id and label lead, data's fields come last, the rest keep their order.
	rank := func(k string) int {
		switch k {
		case "id":
			return 0
		case "label":
			return 1
		case "data":
			return 3
		}
		return 2
	}
	for _, rec := range recs {
		keys := slices.Clone(rec.keys)
		slices.SortStableFunc(keys, func(a, b string) int { return rank(a) - rank(b) })
		for _, k := range keys {
			if k == "noun" {
				if _, ok := stringOf(rec.vals[k]); ok {
					continue
				}
			}
			if k == "data" {
				if data, ok := objectOf(rec.vals[k]); ok {
					for _, sub := range data.keys {
						name := sub
						if top[sub] {
							name = "data." + sub
						}
						add(field{k, sub}, name)
					}
					continue
				}
			}
			add(field{key: k}, k)
		}
	}
	numeric := make([]bool, len(fields))
	for i := range numeric {
		numeric[i] = true
	}
	for _, rec := range recs {
		row := make([]string, len(fields))
		for i, f := range fields {
			raw := rec.vals[f.key]
			if f.sub != "" {
				data, ok := objectOf(raw)
				raw = nil
				if ok {
					raw = data.vals[f.sub]
				}
			}
			row[i] = textOf(raw)
			if row[i] != "" && !isNumber(raw) {
				numeric[i] = false
			}
		}
		t.rows = append(t.rows, row)
	}
	for i, name := range names {
		col := ochazuke.Column{Title: name}
		if numeric[i] {
			col.Align = ochazuke.AlignRight
		}
		t.cols = append(t.cols, col)
	}
	return t
}

// output prints value in format: JSON (print) for json, else the tables of
// a tabular result; a result that is not tabular prints as JSON with a
// note on Stderr.
func (s *Shell) output(q ochazuke.Request, format string, value any) error {
	if format == "" || format == "json" {
		return s.print(value)
	}
	rowsNoun := ""
	if op, ok := s.Runtime.Operations[q.Key()]; ok {
		rowsNoun = op.Rows
	}
	tables, ok := tablesOf(s.Runtime, rowsNoun, value)
	if !ok {
		fmt.Fprintf(s.stderr(), "note: %s does not answer rows; printing JSON instead of --format %s\n", q.Key(), format)
		return s.print(value)
	}
	var b strings.Builder
	switch format {
	case "table":
		writeTables(&b, tables, s.tableWidth())
	case "markdown":
		writeMarkdown(&b, tables)
	case "toon":
		writeTOON(&b, tables)
	}
	_, err := fmt.Fprint(s.stdout(), b.String())
	return err
}

// tableWidth is the width --format table cuts to: Width when positive,
// none when negative, else the terminal's width when Stdout is a terminal
// and none otherwise (a pipe or a file gets every cell whole).
func (s *Shell) tableWidth() int {
	if s.Width != 0 {
		return max(0, s.Width)
	}
	f, ok := s.stdout().(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return 0
	}
	if w, _, err := term.GetSize(f.Fd()); err == nil && w > 0 {
		return w
	}
	return 0
}

// writeTables writes each table as ochazuke.TableText lays it out at
// width; with several tables each is headed by its noun and a blank line
// separates them. A table with no columns writes nothing.
func writeTables(b *strings.Builder, tables []table, width int) {
	titled := len(tables) > 1
	for i, t := range tables {
		if i > 0 {
			b.WriteString("\n")
		}
		if titled && t.noun != "" {
			b.WriteString(t.noun + "\n")
		}
		for _, line := range ochazuke.TableText(t.cols, t.rows, width) {
			b.WriteString(line + "\n")
		}
	}
}

// markdownCell escapes a title or cell for a GitHub table row.
var markdownCell = strings.NewReplacer(`\`, `\\`, "|", `\|`, "\r\n", "<br>", "\n", "<br>", "\r", "<br>")

// writeMarkdown writes each table as a GitHub table (see Formats).
func writeMarkdown(b *strings.Builder, tables []table) {
	cell := func(text string) string {
		return strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, markdownCell.Replace(text))
	}
	line := func(cells []string) { b.WriteString("| " + strings.Join(cells, " | ") + " |\n") }
	titled := len(tables) > 1
	for i, t := range tables {
		if i > 0 {
			b.WriteString("\n")
		}
		if titled && t.noun != "" {
			b.WriteString("## " + t.noun + "\n\n")
		}
		if len(t.cols) == 0 {
			continue
		}
		head, rule := make([]string, len(t.cols)), make([]string, len(t.cols))
		for j, c := range t.cols {
			head[j], rule[j] = cell(c.Title), "---"
			if c.Align == ochazuke.AlignRight {
				rule[j] = "---:"
			}
		}
		line(head)
		line(rule)
		for _, row := range t.rows {
			cells := make([]string, len(t.cols))
			for j := range cells {
				cells[j] = cell(row[j])
			}
			line(cells)
		}
	}
}

// toonEscape writes the escapes inside a quoted TOON value.
var toonEscape = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)

// toonValue is text as a TOON value: bare, or quoted when it holds a
// comma, a double quote or a line break (see Formats).
func toonValue(text string) string {
	if !strings.ContainsAny(text, ",\"\n\r") {
		return text
	}
	return `"` + toonEscape.Replace(text) + `"`
}

// writeTOON writes each table as a TOON tabular array (see Formats).
func writeTOON(b *strings.Builder, tables []table) {
	for _, t := range tables {
		titles := make([]string, len(t.cols))
		for i, c := range t.cols {
			titles[i] = toonValue(c.Title)
		}
		fmt.Fprintf(b, "%s[%d]{%s}:\n", toonValue(t.noun), len(t.rows), strings.Join(titles, ","))
		if len(t.cols) == 0 {
			continue
		}
		for _, row := range t.rows {
			values := make([]string, len(t.cols))
			for i := range values {
				values[i] = toonValue(row[i])
			}
			b.WriteString("  " + strings.Join(values, ",") + "\n")
		}
	}
}
