package markdown

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// session is the element open in the block editor: the input holding its
// source (its common indent taken off), where that source sits in the
// text and what of it is written, and where the editor is drawn (the
// layout as it was when it opened, lines top to top+count replaced, x
// cells in).
type session struct {
	input *ochazuke.Input
	elem  int
	// start is the byte offset of the edited range in the source; written
	// is what the range holds now (what the last flush wrote).
	start   int
	written string
	opened  string // the source when it opened: the undo step's before
	indent  int
	// pre and post go around an insertion's text once it has any; prefill
	// is an insertion's first buffer (a note's head: unchanged, nothing is
	// written).
	pre, post string
	insertion bool
	note      bool
	prefill   string
	// crlf marks an element written with \r\n line ends (the buffer holds
	// \n alone); cr that its last line ends in \r before the range's end.
	crlf, cr bool
	lay      layout
	top      int
	count    int
	x        int
	seen     string
	seq      int
}

// opening describes an element to open: the range [start, end) of the
// source (end == start for an insertion), its indent, the buffer and the
// cursor in it (a byte offset), and where it is drawn.
type opening struct {
	elem, start, end, indent int
	pre, post, buffer        string
	cursor                   int
	insertion, note          bool
	top, count, x            int
}

// compose is the source text a buffer stands for: every nonempty line
// indented again, an insertion's separators around it; an empty insertion
// (or a note's untouched head) is nothing.
func (s *session) compose(buf string) string {
	if s.insertion && (strings.TrimSpace(buf) == "" || s.note && buf == s.prefill) {
		return ""
	}
	pad := strings.Repeat(" ", s.indent)
	lines := strings.Split(buf, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	sep := "\n"
	if s.crlf {
		sep = "\r\n"
	}
	body := strings.Join(lines, sep)
	if s.cr {
		body += "\r"
	}
	if s.insertion {
		return s.pre + body + s.post
	}
	return body
}

func (d *Doc) editable() error {
	if d.src.Save == nil {
		return errors.New("this markdown is read-only")
	}
	return nil
}

// open puts o's buffer in the block editor in insert mode.
func (d *Doc) open(o opening) error {
	if err := d.editable(); err != nil {
		return err
	}
	if d.edit != nil {
		if err := d.Leave(); err != nil {
			return err
		}
	}
	crlf := strings.Contains(o.buffer, "\r\n") || strings.HasSuffix(o.buffer, "\r")
	cr := strings.HasSuffix(o.buffer, "\r")
	if crlf {
		before := len(o.buffer[:min(max(0, o.cursor), len(o.buffer))])
		o.cursor -= strings.Count(o.buffer[:before], "\r")
		o.buffer = strings.TrimSuffix(strings.ReplaceAll(o.buffer, "\r\n", "\n"), "\r")
	}
	in := ochazuke.NewInput(d.EditID(), o.buffer, true)
	if err := d.r.AddInput(in, ochazuke.Parent(d.id), ochazuke.Kind(EditNoun)); err != nil {
		return err
	}
	in.Cursor = utf8.RuneCountInString(o.buffer[:min(max(0, o.cursor), len(o.buffer))])
	s := &session{input: in, elem: o.elem, start: o.start, written: d.text[o.start:o.end], opened: d.text,
		indent: o.indent, pre: o.pre, post: o.post, insertion: o.insertion, note: o.note,
		lay: d.lay, top: min(max(0, o.top), len(d.lay.lines)), count: o.count, x: o.x, seen: o.buffer, crlf: crlf, cr: cr}
	if o.insertion {
		s.prefill = o.buffer
	}
	d.edit = s
	if err := d.r.SetFocus(in.ID); err != nil {
		return err
	}
	return d.r.SetMode("insert")
}

// flush writes the open element when its buffer changed since the last
// write. The source is loaded again first; when it changed, the range is
// found again (by the text before it and what it holds) or the write is
// refused.
func (d *Doc) flush() error {
	s := d.edit
	if s == nil {
		return nil
	}
	text := s.compose(string(s.input.Text))
	if text == s.written {
		return nil
	}
	current, err := d.src.Load()
	if err != nil {
		return fmt.Errorf("not written: load markdown: %w", err)
	}
	at := s.start
	if current != d.text {
		ctx := d.text[max(0, at-32):at]
		if at+len(s.written) > len(current) || current[at:at+len(s.written)] != s.written || current[max(0, at-len(ctx)):at] != ctx {
			at = locate(current, ctx, s.written)
			if at < 0 {
				return refused("not written: the text around the edited block changed in the source")
			}
		}
	}
	next := current[:at] + text + current[at+len(s.written):]
	if err := d.src.Save(next); err != nil {
		return fmt.Errorf("not written: save markdown: %w", err)
	}
	s.start, s.written = at, text
	return d.set(next)
}

// Leave writes the open element, tidies it (a table's pipes aligned, an
// ordered list renumbered), records the whole edit as one undo step and
// draws it again, the cursor on it in normal mode. A write that fails
// leaves the buffer in the unnamed register, so nothing typed is lost.
func (d *Doc) Leave() error { return d.leave(true) }

func (d *Doc) leave(refocus bool) error {
	s := d.edit
	if s == nil {
		return nil
	}
	err := d.flush()
	d.edit = nil
	if d.r.Instances[s.input.ID] != nil {
		refocus = refocus || d.r.Focus == s.input.ID
		_ = d.r.RemoveInstance(s.input.ID)
	}
	at := s.start
	if s.insertion && s.written != "" {
		at += len(s.pre)
	}
	if err != nil {
		_ = d.r.StoreYank(ochazuke.Yank{Text: string(s.input.Text), Shape: "char"}, "", false)
		if current, lerr := d.src.Load(); lerr == nil {
			d.text, d.tasks, d.elems = current, Tasks(current), Elements(current)
		}
		_ = d.relayout(s.elem, 0)
		d.refocus(refocus)
		return fmt.Errorf("%w (the text is in the unnamed register)", err)
	}
	if next := tidy(d.text, at); next != d.text {
		// Tidying keeps the lines; at follows its line.
		line := strings.Count(d.text[:min(at, len(d.text))], "\n")
		if serr := d.src.Save(next); serr == nil {
			d.text, d.tasks, d.elems = next, Tasks(next), Elements(next)
			at = lineOffset(next, line)
		}
	}
	d.leftAt = at
	if !s.insertion && s.opened != d.text {
		// An edited note is no longer what was sent.
		if e := elementIn(d.elems, at); e >= 0 && d.elems[e].Kind == KindNote && d.elems[e].Sent {
			if next, ferr := SetSent(d.text, d.elems[e], false); ferr == nil && d.src.Save(next) == nil {
				d.text, d.tasks, d.elems = next, Tasks(next), Elements(next)
			}
		}
	}
	if s.opened != d.text {
		d.undo, d.redo = append(d.undo, diff(s.opened, d.text)), nil
	}
	_ = d.relayout(-1, 0)
	if e := d.elementAt(at); e >= 0 {
		_ = d.SelectElement(e)
	}
	d.refocus(refocus)
	return nil
}

// elementIn is the element holding byte at (or the first after it).
func elementIn(els []Element, at int) int {
	for i, e := range els {
		if at <= e.End {
			return i
		}
	}
	return -1
}

// lineOffset is the byte offset of line (from 0) in text.
func lineOffset(text string, line int) int {
	at := 0
	for ; line > 0; line-- {
		i := strings.IndexByte(text[at:], '\n')
		if i < 0 {
			return len(text)
		}
		at += i + 1
	}
	return at
}

func (d *Doc) refocus(refocus bool) {
	if !refocus {
		return
	}
	_ = d.list.Focus()
	if d.r.Mode.Name != "normal" {
		_ = d.r.SetMode("normal")
	}
}

// HandleMessage writes the open element when its pause timer is the
// latest.
func (d *Doc) HandleMessage(msg tea.Msg) bool {
	tick, ok := msg.(DebounceMsg)
	if !ok || tick.Doc != d.id {
		return false
	}
	if d.edit != nil && tick.Seq == d.edit.seq {
		if err := d.flush(); err != nil {
			d.r.UI().Error(err.Error())
		}
	}
	return true
}

// DebounceTick is the pause timer in flight, for a host that delivers it
// at once: false when no typing waits on one.
func (d *Doc) DebounceTick() (DebounceMsg, bool) {
	if d.edit == nil {
		return DebounceMsg{}, false
	}
	msg := DebounceMsg{Doc: d.id, Seq: d.edit.seq}
	return msg, d.edit.compose(string(d.edit.input.Text)) != d.edit.written
}

// DebounceCurrent reports whether a pause timer is for the open
// element's latest typing (an older timer writes nothing).
func (d *Doc) DebounceCurrent(tick DebounceMsg) bool {
	return d.edit != nil && tick.Doc == d.id && tick.Seq == d.edit.seq
}

// PendingCommand notices typing in the open element and sets the pause
// timer; an element whose editor lost the focus (not to a modal over it)
// is left and written.
func (d *Doc) PendingCommand() tea.Cmd {
	s := d.edit
	if s == nil {
		return nil
	}
	if d.r.Focus != s.input.ID && d.r.TopModal() == "" {
		if err := d.leave(false); err != nil {
			d.r.UI().Error(err.Error())
		}
		return nil
	}
	text := string(s.input.Text)
	if text == s.seen {
		return nil
	}
	s.seen = text
	s.seq++
	seq, id, wait := s.seq, d.id, d.Debounce
	if wait <= 0 {
		wait = DefaultDebounce
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return DebounceMsg{Doc: id, Seq: seq} })
}

// Close writes an open element.
func (d *Doc) Close() {
	if d.edit != nil {
		_ = d.flush()
	}
}

// target is the cursor's element (and line within it) in the source as
// it is now: the source is loaded again first and, when it changed, the
// element is found again by its text (and its place among elements with
// that text), else refused.
func (d *Doc) target() (int, int, error) {
	if err := d.editable(); err != nil {
		return -1, 0, err
	}
	if err := d.Leave(); err != nil {
		return -1, 0, err
	}
	e, off := d.cursor()
	current, err := d.src.Load()
	if err != nil {
		return -1, 0, fmt.Errorf("not written: load markdown: %w", err)
	}
	if current == d.text {
		return e, off, nil
	}
	raw, nth := "", 0
	if e >= 0 {
		raw = d.text[d.elems[e].Start:d.elems[e].End]
		for _, el := range d.elems[:e] {
			if d.text[el.Start:el.End] == raw {
				nth++
			}
		}
	}
	before := d.elems
	beforeText := d.text
	if err := d.set(current); err != nil {
		return -1, 0, err
	}
	if e < 0 {
		return d.elementAt(0), 0, nil
	}
	count := func(els []Element, text string) (out []int) {
		for i, el := range els {
			if text[el.Start:el.End] == raw {
				out = append(out, i)
			}
		}
		return out
	}
	was, is := count(before, beforeText), count(d.elems, d.text)
	found := -1
	switch {
	case len(is) == len(was) && nth < len(is):
		found = is[nth]
	case len(is) == 1:
		found = is[0]
	}
	if found < 0 {
		return -1, 0, refused("not written: %q changed or is gone in the source", firstLine(raw))
	}
	_ = d.selectAt(found, off)
	return found, off, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// subtreeEnd is the last element of item i's subtree (its notes,
// continuations and nested items); i itself for any other element.
func subtreeEnd(els []Element, i int) int {
	e := els[i]
	if !e.IsItem() {
		return i
	}
	j := i
	for j+1 < len(els) {
		n := els[j+1]
		if n.Root != e.Root || n.Root < 0 || n.Indent <= e.Indent && !(n.Kind == KindNote && n.Item == i) {
			break
		}
		j++
	}
	return j
}

// itemOf is the list item an element belongs to: itself, or the item a
// note, quote or continuation is under; -1 for none.
func itemOf(els []Element, e int) int {
	if e < 0 || e >= len(els) {
		return -1
	}
	if els[e].IsItem() {
		return e
	}
	return els[e].Item
}

// lead is how many cells line starts with blank.
func lead(line string) int {
	plain := ansi.Strip(line)
	return len(plain) - len(strings.TrimLeft(plain, " "))
}

// commonIndent is the indent every nonblank line of raw shares, at most max.
func commonIndent(raw string, max int) int {
	n := max
	for _, l := range strings.Split(raw, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n = min(n, len(l)-len(strings.TrimLeft(l, " ")))
	}
	return n
}

func dedent(raw string, n int) string {
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		k := 0
		for k < n && k < len(l) && l[k] == ' ' {
			k++
		}
		lines[i] = l[k:]
	}
	return strings.Join(lines, "\n")
}

// contentStart is where a buffer line's text starts: after a list marker
// and box, a heading's #s, a quote's >, a note's head, a row's first |.
func contentStart(line string) int {
	if m := itemMarker.FindStringIndex(line); m != nil {
		return m[1]
	}
	t := strings.TrimLeft(line, " ")
	at := len(line) - len(t)
	switch {
	case strings.HasPrefix(t, "#"):
		k := at
		for k < len(line) && line[k] == '#' {
			k++
		}
		for k < len(line) && line[k] == ' ' {
			k++
		}
		return k
	case strings.HasPrefix(t, ">"):
		k := at + 1
		if k < len(line) && line[k] == ' ' {
			k++
		}
		if m := noteAuthor.FindStringIndex(line[k:]); m != nil {
			k += m[1]
		}
		return k
	case strings.HasPrefix(t, "|"):
		k := at + 1
		if k < len(line) && line[k] == ' ' {
			k++
		}
		return k
	}
	return at
}

// lineOf is the byte range of the buffer line holding byte at.
func lineOf(buf string, at int) (start, end int) {
	at = min(max(0, at), len(buf))
	start = strings.LastIndexByte(buf[:at], '\n') + 1
	end = len(buf)
	if i := strings.IndexByte(buf[at:], '\n'); i >= 0 {
		end = at + i
	}
	return start, end
}

// Insert edits the cursor's element in the block editor: where is
// "cursor" (the start of the text on the cursor's line), "after" (the end
// of the element), "start" (the start of the cursor's line) or "end" (its
// end). On an empty document it starts the first paragraph.
func (d *Doc) Insert(where string) error {
	e, off, err := d.target()
	if err != nil {
		return err
	}
	if e < 0 {
		return d.Open("below")
	}
	el := d.elems[e]
	raw := d.text[el.Start:el.End]
	base := commonIndent(raw, el.Indent)
	buf := dedent(raw, base)
	lines := strings.Split(buf, "\n")
	span := d.lay.spans[e]
	height := span[1] - span[0] + 1
	bl := off
	if height != len(lines) {
		bl = off * len(lines) / max(1, height)
	}
	bl = min(max(0, bl), len(lines)-1)
	lineStart := 0
	for _, l := range lines[:bl] {
		lineStart += len(l) + 1
	}
	line := lines[bl]
	cursor := lineStart
	switch where {
	case "after":
		cursor = len(buf)
	case "start":
	case "end":
		cursor = lineStart + len(line)
	default:
		cursor = lineStart + contentStart(line)
	}
	return d.open(opening{elem: e, start: el.Start, end: el.End, indent: base, buffer: buf, cursor: cursor,
		top: span[0], count: height, x: lead(d.lay.lines[span[0]])})
}

// insertion is what o or O inserts for element e: where, the separators,
// the indent and the first buffer (its marker).
type insertion struct {
	at        int
	pre, post string
	indent    int
	buffer    string
	cursor    int // where the cursor starts in buffer; -1 its end
	note      bool
}

// opened is the insertion o (below) or O (above) makes at element e of
// src: a list item of the same marker and indent (numbered one on, a
// checklist item unchecked), a table row, a note line, else a paragraph.
func opened(src string, els []Element, e int, below bool, author, date string) insertion {
	if e < 0 || e >= len(els) {
		ins := insertion{at: len(src), post: "\n", cursor: -1}
		switch {
		case src == "":
		case strings.HasSuffix(src, "\n\n"):
		case strings.HasSuffix(src, "\n"):
			ins.pre = "\n"
		default:
			ins.pre = "\n\n"
		}
		return ins
	}
	el := els[e]
	lineBlank := func(at int) bool {
		end := strings.IndexByte(src[at:], '\n')
		if end < 0 {
			end = len(src) - at
		}
		return strings.TrimSpace(src[at:at+end]) == ""
	}
	switch {
	case el.IsItem():
		ins := insertion{indent: el.Indent, buffer: itemMarkerFor(els, e, below), cursor: -1}
		if below {
			ins.at, ins.pre = els[subtreeEnd(els, e)].End, "\n"
		} else {
			ins.at, ins.post = el.Start, "\n"
		}
		return ins
	case el.Kind == KindTable:
		row := "|" + strings.Repeat("  |", max(1, el.Cols))
		if below || el.Header {
			return insertion{at: el.End, pre: "\n", indent: el.Indent, buffer: row, cursor: 2}
		}
		return insertion{at: el.Start, post: "\n", indent: el.Indent, buffer: row, cursor: 2}
	case el.Kind == KindNote:
		at, indent := noteInsertion(els, el.Item)
		head := noteHead(author, date, false)
		return insertion{at: at, pre: "\n", indent: indent, buffer: head, note: true, cursor: -1}
	}
	ins := insertion{cursor: -1}
	if el.Item >= 0 {
		ins.indent = el.Indent
	}
	if below {
		ins.at, ins.pre = el.End, "\n\n"
		if next := el.End + 1; next < len(src) && !lineBlank(next) {
			ins.post = "\n"
		}
		return ins
	}
	ins.at, ins.post = el.Start, "\n\n"
	if el.Start > 0 && el.Start >= 2 && !lineBlank(strings.LastIndexByte(src[:el.Start-1], '\n')+1) {
		ins.pre = "\n"
	}
	return ins
}

// start is where the cursor starts in the insertion's buffer.
func (ins insertion) start() int {
	if ins.cursor < 0 || ins.cursor > len(ins.buffer) {
		return len(ins.buffer)
	}
	return ins.cursor
}

// itemMarkerFor is the marker a new item next to item e starts with.
func itemMarkerFor(els []Element, e int, below bool) string {
	el := els[e]
	box := ""
	if el.Kind == KindTask {
		box = "[ ] "
	}
	if !el.Ordered() {
		return el.Marker + " " + box
	}
	n := el.Number
	if below && !lazyNumbers(els, el.List) {
		n++
	}
	return strconv.Itoa(n) + el.Marker + " " + box
}

// lazyNumbers reports whether every item of an ordered list has the same
// number ("1." throughout): such a list is never renumbered.
func lazyNumbers(els []Element, list int) bool {
	first, n := -1, 0
	for _, e := range els {
		if e.List != list || !e.IsItem() {
			continue
		}
		n++
		if first < 0 {
			first = e.Number
		} else if e.Number != first {
			return false
		}
	}
	return n > 1
}

// Open opens a new line below or above the cursor's element in insert
// mode, continuing its structure (see opened).
func (d *Doc) Open(where string) error {
	e, _, err := d.target()
	if err != nil {
		return err
	}
	return d.openAt(e, where != "above", "")
}

// openAt opens an insertion at element e with tail after its marker.
func (d *Doc) openAt(e int, below bool, tail string) error {
	ins := opened(d.text, d.elems, e, below, d.author(), d.today())
	o := opening{elem: e, start: ins.at, end: ins.at, indent: ins.indent, pre: ins.pre, post: ins.post,
		buffer: ins.buffer + tail, cursor: ins.start(), insertion: true, note: ins.note}
	switch {
	case e < 0:
		o.top = len(d.lay.lines)
		if len(d.elems) == 0 {
			o.top = 0
		}
	case below:
		last := e
		for k := e; k <= subtreeEnd(d.elems, e); k++ {
			if d.drawn(k) {
				last = k
			}
		}
		if d.elems[e].Kind == KindNote {
			for k := e; k < len(d.elems) && d.elems[k].Kind == KindNote && d.elems[k].Group == d.elems[e].Group; k++ {
				if d.drawn(k) {
					last = k
				}
			}
		}
		o.top = d.lay.spans[last][1] + 1
		o.x = lead(d.lay.lines[d.lay.spans[e][0]])
	default:
		o.top = d.lay.spans[e][0]
		o.x = lead(d.lay.lines[d.lay.spans[e][0]])
	}
	if len(d.elems) == 0 && len(d.lay.lines) == 1 && d.lay.owner[0] < 0 {
		o.count = 1 // the empty document's blank line
	}
	return d.open(o)
}

// deletion is the byte range dd removes at element e, line off within
// it: an item with its subtree, a table's header with the whole table, a
// line of a paragraph, quote or code block, else the element; and the
// lines it yanks.
func (d *Doc) deletion(e, off int) (start, end int) {
	el := d.elems[e]
	start, end = el.Start, el.End
	switch {
	case el.IsItem():
		end = d.elems[subtreeEnd(d.elems, e)].End
	case el.Kind == KindTable && el.Header:
		for k := e + 1; k < len(d.elems) && d.elems[k].Kind == KindTable && d.elems[k].Group == el.Group; k++ {
			end = d.elems[k].End
		}
	case el.Kind == KindParagraph || el.Kind == KindQuote || el.Kind == KindCode:
		lines := strings.Split(d.text[el.Start:el.End], "\n")
		if len(lines) <= 1 {
			break
		}
		span := d.lay.spans[e]
		height := span[1] - span[0] + 1
		bl := off
		if el.Kind == KindCode && (strings.HasPrefix(strings.TrimSpace(lines[0]), "```") || strings.HasPrefix(strings.TrimSpace(lines[0]), "~~~")) {
			if len(lines) <= 2 {
				break
			}
			bl = 1 + off*max(1, len(lines)-2)/max(1, height)
			bl = min(bl, len(lines)-2)
		} else if height != len(lines) {
			bl = off * len(lines) / max(1, height)
		}
		bl = min(max(0, bl), len(lines)-1)
		for _, l := range lines[:bl] {
			start += len(l) + 1
		}
		end = start + len(lines[bl])
	}
	return start, end
}

// cutLines removes the lines [start, end) of src and their line end, and
// one blank line where that leaves two together.
func cutLines(src string, start, end int) (string, int) {
	if end < len(src) {
		end++
	} else if start > 0 {
		start--
	}
	next := src[:start] + src[end:]
	at := min(start, len(next))
	blankAt := func(i int) bool {
		if i < 0 || i >= len(next) {
			return false
		}
		e := strings.IndexByte(next[i:], '\n')
		return e >= 0 && strings.TrimSpace(next[i:i+e]) == ""
	}
	prevBlank := at == 0 || at >= 2 && next[at-1] == '\n' && blankAt(strings.LastIndexByte(next[:at-1], '\n')+1)
	if prevBlank && blankAt(at) {
		e := strings.IndexByte(next[at:], '\n')
		next = next[:at] + next[at+e+1:]
	}
	return next, min(at, len(next))
}

// Delete removes the cursor's line or element (see deletion), yanked as
// lines; written at once.
func (d *Doc) Delete() error {
	e, off, err := d.target()
	if err != nil {
		return err
	}
	if e < 0 {
		return errors.New("nothing to delete")
	}
	start, end := d.deletion(e, off)
	if err := d.r.StoreYank(ochazuke.Yank{Text: d.text[start:end] + "\n", Shape: "line"}, "", true); err != nil {
		return err
	}
	next, at := cutLines(d.text, start, end)
	next = tidy(next, at)
	return d.write(next, at)
}

// Yank copies the cursor's line or element (what dd would delete) as
// lines.
func (d *Doc) Yank() error {
	if err := d.Leave(); err != nil {
		return err
	}
	e, off := d.cursor()
	if e < 0 {
		return errors.New("nothing to yank")
	}
	start, end := d.deletion(e, off)
	return d.r.StoreYank(ochazuke.Yank{Text: d.text[start:end] + "\n", Shape: "line"}, "", false)
}

// Paste puts the unnamed register's lines below (or above) the cursor's
// element (after an item's subtree); written at once.
func (d *Doc) Paste(below bool) error {
	e, _, err := d.target()
	if err != nil {
		return err
	}
	y, err := d.r.ReadYank("")
	if err != nil {
		return err
	}
	text := strings.TrimSuffix(y.Text, "\n")
	if text == "" {
		return errors.New("nothing to paste")
	}
	var next string
	at := len(d.text)
	switch {
	case e < 0:
		next = d.text + text + "\n"
	case below:
		at = d.elems[subtreeEnd(d.elems, e)].End
		next = d.text[:at] + "\n" + text + d.text[at:]
		at++
	default:
		at = d.elems[e].Start
		next = d.text[:at] + text + "\n" + d.text[at:]
	}
	return d.write(tidy(next, at), at)
}

// author signs new notes: Doc.Author, else the AuthorSetting, else
// "owner".
func (d *Doc) author() string {
	if d.Author != "" {
		return d.Author
	}
	if a := d.r.Settings().String(AuthorSetting); a != "" {
		return a
	}
	return "owner"
}

func (d *Doc) today() string {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	return now().Format("2006-01-02")
}

// AddNote opens a new note under the cursor's list item (the item a note
// or continuation belongs to) in insert mode, after its notes, signed by
// the author and dated today.
func (d *Doc) AddNote() error {
	e, _, err := d.target()
	if err != nil {
		return err
	}
	item := itemOf(d.elems, e)
	if item < 0 {
		return errors.New("notes go under a list item")
	}
	if d.folded(item) {
		delete(d.folds, d.foldKey(item))
		_ = d.relayout(item, 0)
	}
	at, indent := noteInsertion(d.elems, item)
	head := noteHead(d.author(), d.today(), false)
	o := opening{elem: item, start: at, end: at, indent: indent, pre: "\n", buffer: head, cursor: len(head), insertion: true, note: true}
	last := item
	for k := item + 1; k < len(d.elems) && k <= subtreeEnd(d.elems, item); k++ {
		if d.elems[k].Kind == KindNote && d.elems[k].Item == item && d.drawn(k) {
			last = k
		}
	}
	o.top = d.lay.spans[last][1] + 1
	o.x = lead(d.lay.lines[d.lay.spans[item][0]]) + 2
	return d.open(o)
}

// Resolve marks the note under the cursor resolved (✓ after its date),
// or open again; written at once.
func (d *Doc) Resolve() error {
	e, _, err := d.target()
	if err != nil {
		return err
	}
	if e < 0 || d.elems[e].Kind != KindNote {
		return errors.New("no note selected")
	}
	next, err := SetResolved(d.text, d.elems[e], !d.elems[e].Resolved)
	if err != nil {
		return err
	}
	return d.write(next, d.elems[e].Start)
}

// Fold folds or unfolds the notes of the cursor's list item.
func (d *Doc) Fold() error {
	if err := d.Leave(); err != nil {
		return err
	}
	e, _ := d.cursor()
	item := itemOf(d.elems, e)
	if item < 0 {
		return errors.New("no list item here")
	}
	has := false
	for k := item + 1; k <= subtreeEnd(d.elems, item); k++ {
		if d.elems[k].Kind == KindNote && d.elems[k].Item == item {
			has = true
		}
	}
	if !has {
		return errors.New("this item has no notes")
	}
	key := d.foldKey(item)
	if d.folds[key] {
		delete(d.folds, key)
	} else {
		d.folds[key] = true
	}
	if d.elems[e].Kind == KindNote && d.folds[key] {
		e = item
	}
	return d.relayout(e, 0)
}

// Move moves the cursor steps elements on (negative back) among the drawn
// elements match accepts, skipping elements of the cursor's group when
// skipGroup (a table's rows); an error says when there is none.
func (d *Doc) Move(steps int, match func(Element) bool, skipGroup bool, what string) error {
	if err := d.Leave(); err != nil {
		return err
	}
	e, _ := d.cursor()
	dir := 1
	if steps < 0 {
		dir, steps = -1, -steps
	}
	at := e
	moved := false
	for ; steps > 0; steps-- {
		group := -1
		if at >= 0 && skipGroup {
			group = d.elems[at].Group
		}
		k := at + dir
		if at < 0 && dir < 0 {
			k = len(d.elems) - 1
		}
		for ; k >= 0 && k < len(d.elems); k += dir {
			if !d.drawn(k) || !match(d.elems[k]) || skipGroup && d.elems[k].Group == group {
				continue
			}
			if at >= 0 && d.drawn(at) && d.lay.spans[k][0] == d.lay.spans[at][0] {
				continue
			}
			break
		}
		if k < 0 || k >= len(d.elems) {
			break
		}
		if dir < 0 && skipGroup && d.elems[k].Kind == KindTable {
			for k > 0 && d.elems[k-1].Kind == KindTable && d.elems[k-1].Group == d.elems[k].Group {
				k--
			}
		}
		at, moved = k, true
	}
	if !moved {
		return nil // edges are quiet
	}
	return d.SelectElement(at)
}

// Component is what } and { count as one: a list (its items, notes
// apart), one blockquote of notes, a table; every other element alone.
func Component(els []Element, i int) string {
	e := els[i]
	switch {
	case e.Kind == KindNote:
		return "notes " + strconv.Itoa(e.Group)
	case e.Kind == KindTable:
		return "table " + strconv.Itoa(e.Group)
	case e.Root >= 0:
		return "list " + strconv.Itoa(e.Root)
	}
	return "element " + strconv.Itoa(i)
}

// MoveComponent moves the cursor steps components on (negative back): to
// the first element of the next component in drawing order, or the
// first of the one before.
func (d *Doc) MoveComponent(steps int) error {
	if err := d.Leave(); err != nil {
		return err
	}
	e, _ := d.cursor()
	if e < 0 {
		return nil
	}
	dir := 1
	if steps < 0 {
		dir, steps = -1, -steps
	}
	at := e
	for ; steps > 0; steps-- {
		here := Component(d.elems, at)
		if dir < 0 {
			first := at
			for first > 0 && Component(d.elems, first-1) == here && d.drawn(first-1) {
				first--
			}
			if first != at {
				at = first // back to the start of this component first
				continue
			}
		}
		k := at + dir
		for ; k >= 0 && k < len(d.elems); k += dir {
			if d.drawn(k) && Component(d.elems, k) != here {
				break
			}
		}
		if k < 0 || k >= len(d.elems) {
			if dir < 0 {
				// Back at the start of the first component.
				for k = at; k > 0 && Component(d.elems, k-1) == here; k-- {
				}
				if d.drawn(k) {
					at = k
				}
			}
			break
		}
		if dir < 0 {
			// The first drawn element of that component.
			c := Component(d.elems, k)
			for k > 0 && Component(d.elems, k-1) == c {
				k--
			}
			for !d.drawn(k) {
				k++
			}
		}
		at = k
	}
	return d.SelectElement(at)
}

// kindClass is what ]] and [[ count as the same kind as an element.
func kindClass(kind string) string {
	switch kind {
	case KindBullet, KindNumbered:
		return "list"
	}
	return kind
}

// Headings are the document's headings as chooser rows (their element
// index as the data).
func (d *Doc) headings() []ochazuke.ChooserRow {
	var rows []ochazuke.ChooserRow
	for i, e := range d.elems {
		if e.Kind != KindHeading {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(firstLine(d.text[e.Start:e.End]), "#"))
		rows = append(rows, ochazuke.ChooserRow{ID: "heading/" + strconv.Itoa(i), Label: strings.Repeat("  ", max(0, e.Level-1)) + title, Data: i, Cells: []string{strings.Repeat("  ", max(0, e.Level-1)) + title, "line " + strconv.Itoa(e.Line)}})
	}
	return rows
}

// Outline opens a chooser over the headings; choosing one puts the cursor
// on it.
func (d *Doc) Outline() error {
	if err := d.Leave(); err != nil {
		return err
	}
	rows := d.headings()
	if len(rows) == 0 {
		return errors.New("no headings")
	}
	_, err := d.r.OpenChooser(ochazuke.ChooserSpec{Kind: "markdown-outline", Title: "Headings", Rows: rows, Choose: func(row ochazuke.ChooserRow) ochazuke.Result {
		i, _ := row.Data.(int)
		if err := d.SelectElement(i); err != nil {
			return ochazuke.Failed(err)
		}
		_ = d.list.Focus()
		return ochazuke.Done(nil)
	}})
	return err
}

// Edit makes an edit of the application's own (marking notes sent, say)
// through the Doc: the source is loaded again, change turns its text into
// the next and names where the cursor goes, and the result is written as
// one undo step.
func (d *Doc) Edit(change func(text string) (next string, at int, err error)) error {
	if err := d.editable(); err != nil {
		return err
	}
	if err := d.Leave(); err != nil {
		return err
	}
	current, err := d.src.Load()
	if err != nil {
		return fmt.Errorf("not written: load markdown: %w", err)
	}
	if current != d.text {
		if err := d.set(current); err != nil {
			return err
		}
	}
	next, at, err := change(d.text)
	if err != nil {
		return err
	}
	return d.write(next, at)
}
