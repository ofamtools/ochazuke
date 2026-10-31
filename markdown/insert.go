package markdown

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke"
)

// buffer is the open element's text, the cursor as a byte offset and the
// cursor's line (its byte range and the column in it).
type buffer struct {
	text                string
	at                  int
	lineStart, lineEnd  int
	line, before, after string
}

func (d *Doc) buffer() (buffer, bool) {
	s := d.edit
	if s == nil {
		return buffer{}, false
	}
	text := string(s.input.Text)
	at := len(string(s.input.Text[:min(max(0, s.input.Cursor), len(s.input.Text))]))
	ls, le := lineOf(text, at)
	return buffer{text: text, at: at, lineStart: ls, lineEnd: le, line: text[ls:le], before: text[ls:at], after: text[at:le]}, true
}

// replace sets the open element's text, the cursor at byte at.
func (d *Doc) replace(text string, at int) error {
	in := d.edit.input
	if err := in.Replace(0, len(in.Text), text); err != nil {
		return err
	}
	in.Cursor = utf8.RuneCountInString(text[:min(max(0, at), len(text))])
	return nil
}

// insertText types text at the cursor.
func (d *Doc) insertText(text string) error {
	in := d.edit.input
	cur := min(max(0, in.Cursor), len(in.Text))
	return in.Replace(cur, cur, text)
}

// isItemLine reports whether a buffer line starts with a list marker.
func isItemLine(line string) bool {
	return itemMarker.MatchString(line) && strings.TrimSpace(line) != ""
}

// emptyItem reports whether line is a list marker (and box) with no text.
func emptyItem(line string) bool {
	m := itemMarker.FindStringIndex(line)
	return m != nil && strings.TrimSpace(line[m[1]:]) == ""
}

// ContinueItem is enter on a list item in the block editor: the item
// ends at the cursor and the next starts below it with the same marker,
// the rest of the line in it.
func (d *Doc) ContinueItem() error {
	b, ok := d.buffer()
	if !ok {
		return errors.New("no block is open")
	}
	tail := strings.TrimLeft(b.after, " ")
	text := b.text[:b.at] + b.text[b.lineEnd:]
	if err := d.replace(strings.TrimRight(text[:b.at], " ")+text[b.at:], b.at); err != nil {
		return err
	}
	return d.continueWith(true, tail)
}

// EndList is enter on an empty list item: the item goes and a paragraph
// opens after the list.
func (d *Doc) EndList() error {
	b, ok := d.buffer()
	if !ok {
		return errors.New("no block is open")
	}
	return d.endList(b)
}

// NextRow is enter on a table row: the row is left and a new one opens
// below it.
func (d *Doc) NextRow() error {
	if d.edit == nil {
		return errors.New("no block is open")
	}
	return d.continueWith(true, "")
}

// AfterHeading is enter on a heading: it is left and a paragraph opens
// below it.
func (d *Doc) AfterHeading() error {
	if d.edit == nil {
		return errors.New("no block is open")
	}
	return d.continueParagraph()
}

// QuoteLine is enter in a quote or note: a line break and the next line's
// "> ".
func (d *Doc) QuoteLine() error {
	b, ok := d.buffer()
	if !ok {
		return errors.New("no block is open")
	}
	t := strings.TrimLeft(b.line, " ")
	return d.insertText("\n" + strings.Repeat(" ", len(b.line)-len(t)) + "> ")
}

// continueWith leaves the open element and opens the next one below it
// (an item, a row), tail after its marker.
func (d *Doc) continueWith(below bool, tail string) error {
	if err := d.leave(true); err != nil {
		return err
	}
	e := d.elementAt(d.leftAt)
	if e < 0 {
		return d.openAt(-1, true, tail)
	}
	return d.openAt(e, below, tail)
}

// continueParagraph leaves a heading and opens a paragraph below it.
func (d *Doc) continueParagraph() error {
	if err := d.leave(true); err != nil {
		return err
	}
	e := d.elementAt(d.leftAt)
	ins := opened(d.text, d.elems, -1, true, "", "")
	if e >= 0 {
		el := d.elems[e]
		// A paragraph after the heading, as o on any non-list element.
		tmp := el
		tmp.Kind, tmp.Item = KindParagraph, -1
		els := append([]Element(nil), d.elems...)
		els[e] = tmp
		ins = opened(d.text, els, e, true, "", "")
	}
	return d.openInsertion(e, ins, "")
}

// openInsertion opens ins drawn below element e.
func (d *Doc) openInsertion(e int, ins insertion, tail string) error {
	o := opening{elem: e, start: ins.at, end: ins.at, indent: ins.indent, pre: ins.pre, post: ins.post,
		buffer: ins.buffer + tail, cursor: ins.start(), insertion: true, note: ins.note, top: len(d.lay.lines)}
	if e >= 0 && d.drawn(e) {
		last := e
		for k := e; k <= subtreeEnd(d.elems, e); k++ {
			if d.drawn(k) {
				last = k
			}
		}
		o.top = d.lay.spans[last][1] + 1
	}
	return d.open(o)
}

// endList is enter on an empty item: the item goes and a paragraph opens
// after the list.
func (d *Doc) endList(b buffer) error {
	s := d.edit
	text := strings.TrimPrefix(b.text[b.lineEnd:], "\n")
	if b.lineStart > 0 {
		text = b.text[:b.lineStart-1] + b.text[b.lineEnd:]
	}
	if err := d.replace(text, min(b.lineStart, len(text))); err != nil {
		return err
	}
	at := s.start
	if err := d.leave(true); err != nil {
		return err
	}
	e := d.elementAt(max(0, at-1))
	if e < 0 {
		return d.openAt(-1, true, "")
	}
	root := d.elems[e].Root
	last := e
	if root >= 0 {
		for k, el := range d.elems {
			if el.Root == root {
				last = k
			}
		}
	}
	end := d.elems[last].End
	ins := insertion{at: end, pre: "\n\n", cursor: -1}
	if end+1 < len(d.text) && strings.TrimSpace(firstLine(d.text[end+1:])) != "" {
		ins.post = "\n"
	}
	return d.openInsertion(last, ins, "")
}

// Indent nests the open list item one level (under the item above it) or
// takes it out one (delta -1): its lines move to the item above's text
// column, or to its parent's marker column.
func (d *Doc) Indent(delta int) error {
	s := d.edit
	if s == nil {
		return errors.New("no block is open")
	}
	at := s.start
	cur := s.indent + indentOf(strings.SplitN(string(s.input.Text), "\n", 2)[0])
	// The items before the open one, nearest first.
	var prev []Element
	for _, e := range d.elems {
		if e.IsItem() && e.Start < at {
			prev = append([]Element{e}, prev...)
		}
	}
	want := cur
	if delta > 0 {
		for _, e := range prev {
			if e.Indent == cur {
				want = e.Content
				break
			}
			if e.Indent < cur {
				break
			}
		}
	} else {
		want = max(0, cur-2)
		for _, e := range prev {
			if e.Indent < cur {
				want = e.Indent
				break
			}
		}
	}
	if want == cur {
		return nil
	}
	shift := want - cur
	s.indent = max(0, s.indent+shift)
	s.x = max(0, s.x+shift)
	return nil
}

// Cell moves the cursor to the next table cell (delta 1) or the previous
// one (-1); past the last cell it starts a row below.
func (d *Doc) Cell(delta int) error {
	b, ok := d.buffer()
	if !ok {
		return errors.New("no block is open")
	}
	var pipes []int
	for i := 0; i < len(b.line); i++ {
		if b.line[i] == '\\' {
			i++
			continue
		}
		if b.line[i] == '|' {
			pipes = append(pipes, i)
		}
	}
	col := b.at - b.lineStart
	// cell starts: after each pipe but the last.
	var starts []int
	for k, p := range pipes {
		if k == len(pipes)-1 && strings.TrimSpace(b.line[p+1:]) == "" {
			break
		}
		at := p + 1
		if at < len(b.line) && b.line[at] == ' ' {
			at++
		}
		starts = append(starts, at)
	}
	cur := -1
	for k, st := range starts {
		if col >= st-1 {
			cur = k
		}
	}
	next := cur + delta
	if next < 0 {
		return nil
	}
	if next >= len(starts) {
		return d.continueWith(true, "")
	}
	d.edit.input.Cursor = utf8.RuneCountInString(b.text[:b.lineStart+starts[next]])
	return nil
}

// Unmark is backspace on an empty list marker: the marker goes and the
// line stays, indented to the item's text, as a continuation line (a
// note under the item).
func (d *Doc) Unmark() error {
	b, ok := d.buffer()
	if !ok {
		return errors.New("no block is open")
	}
	m := itemMarker.FindStringSubmatchIndex(b.line)
	if m == nil {
		return errors.New("no list marker here")
	}
	width := m[11] // after the marker's spaces: the item's text column
	text := b.text[:b.lineStart] + strings.Repeat(" ", width) + b.text[b.lineStart+m[1]:]
	return d.replace(text, b.lineStart+width)
}

// blockKinds are the rows of the / chooser: what each puts on the line
// and where the cursor goes in it.
var blockKinds = []struct {
	id, label, text string
	cursor          int
}{
	{"table", "table", "", 0},
	{"checklist", "checklist", "- [ ] ", -1},
	{"bullets", "bullets", "- ", -1},
	{"numbered", "numbered list", "1. ", -1},
	{"h1", "heading 1", "# ", -1},
	{"h2", "heading 2", "## ", -1},
	{"h3", "heading 3", "### ", -1},
	{"code", "code block", "```\n\n```", 4},
	{"quote", "quote", "> ", -1},
	{"rule", "rule", "---", -1},
}

// BlockChooser is / on an empty line in the block editor: a chooser of
// block kinds whose pick puts the kind's Markdown on the line (a table
// asks for its column count first).
func (d *Doc) BlockChooser() error {
	if _, ok := d.buffer(); !ok {
		return errors.New("no block is open")
	}
	rows := make([]ochazuke.ChooserRow, len(blockKinds))
	for i, k := range blockKinds {
		rows[i] = ochazuke.ChooserRow{ID: "block/" + k.id, Label: k.label, Data: i}
	}
	_, err := d.r.OpenChooser(ochazuke.ChooserSpec{Kind: "markdown-blocks", Title: "Insert", Rows: rows, Choose: func(row ochazuke.ChooserRow) ochazuke.Result {
		i, _ := row.Data.(int)
		k := blockKinds[i]
		if k.id == "table" {
			return d.columnsChooser()
		}
		cursor := k.cursor
		if cursor < 0 {
			cursor = len(k.text)
		}
		return result(d.putLine(k.text, cursor))
	}})
	return err
}

func result(err error) ochazuke.Result {
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(nil)
}

// columnsChooser asks a new table's column count: 2 to 6, or a number
// typed.
func (d *Doc) columnsChooser() ochazuke.Result {
	var rows []ochazuke.ChooserRow
	for n := 2; n <= 6; n++ {
		rows = append(rows, ochazuke.ChooserRow{ID: "columns/" + strconv.Itoa(n), Label: strconv.Itoa(n) + " columns", Data: n})
	}
	table := func(n int) ochazuke.Result {
		n = min(max(1, n), 32)
		head := "|" + strings.Repeat("  |", n)
		delim := "|" + strings.Repeat(" --- |", n)
		return result(d.putLine(head+"\n"+delim, 2))
	}
	_, err := d.r.OpenChooser(ochazuke.ChooserSpec{Kind: "markdown-columns", Title: "Columns", Rows: rows,
		Choose: func(row ochazuke.ChooserRow) ochazuke.Result { n, _ := row.Data.(int); return table(n) },
		Create: func(query string) ochazuke.Result {
			n, err := strconv.Atoi(strings.Fields(query + " ")[0])
			if err != nil || n < 1 {
				return ochazuke.Failed(errors.New("a table needs a number of columns"))
			}
			return table(n)
		},
		CreateLabel: func(query string) string { return query + " columns" },
	})
	return result(err)
}

// putLine replaces the cursor's (empty) line in the open element with
// text, the cursor cursor bytes into it.
func (d *Doc) putLine(text string, cursor int) error {
	b, ok := d.buffer()
	if !ok {
		return errors.New("no block is open")
	}
	pad := b.line[:len(b.line)-len(strings.TrimLeft(b.line, " "))]
	full := b.text[:b.lineStart] + pad + text + b.text[b.lineEnd:]
	return d.replace(full, b.lineStart+len(pad)+cursor)
}
