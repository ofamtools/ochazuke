package markdown

import (
	"regexp"
	"strconv"
	"strings"

	gast "github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Element kinds: the blocks a Doc's cursor moves over, in source order.
const (
	KindHeading   = "heading"
	KindParagraph = "paragraph"
	KindBullet    = "bullet"   // a bullet list item
	KindNumbered  = "numbered" // an ordered list item
	KindTask      = "task"     // a checklist item ([ ] or [x]), bullet or numbered
	KindTable     = "table"    // one table row (the header row with its delimiter line)
	KindCode      = "code"
	KindQuote     = "quote"
	KindRule      = "rule"
	KindNote      = "note" // a note: a blockquote nested directly under a list item
)

// KindLetters are the letters ] and [ take: the kinds each one jumps to.
var KindLetters = map[string][]string{
	"h": {KindHeading},
	"t": {KindTable},
	"x": {KindTask},
	"l": {KindBullet, KindNumbered, KindTask},
	"c": {KindCode},
	"q": {KindQuote},
	"n": {KindNote},
	"p": {KindParagraph},
}

// Element is one block of a Markdown source as a Doc draws and edits it:
// a heading, a paragraph, a list item (its own lines, not its nested
// lists or notes), one table row, a code block, a quote, a rule or a note.
// Start and End are byte offsets of whole lines: the start of the first
// line and the end of the last (before its newline).
type Element struct {
	Kind  string `json:"kind"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Line  int    `json:"line"` // 1-based source line of Start
	// Level is a heading's level (1 to 6).
	Level int `json:"level,omitempty"`
	// Indent is the column the element starts at (a list item's marker, a
	// note's >); Content the column its text starts at (after a list
	// marker; a note's text after "> ").
	Indent  int `json:"indent,omitempty"`
	Content int `json:"content,omitempty"`
	// Marker is a list item's marker: "-", "*", "+", or the delimiter of an
	// ordered item ("." or ")") with Number its number.
	Marker  string `json:"marker,omitempty"`
	Number  int    `json:"number,omitempty"`
	Checked bool   `json:"checked,omitempty"`
	// Box is a task's box byte offset (see Task.Box), -1 otherwise.
	Box int `json:"-"`
	// List groups the items of one list (an id, the index of its first
	// element), Root the outermost list an item, its notes and its
	// continuations belong to; -1 outside lists.
	List int `json:"-"`
	Root int `json:"-"`
	// Item is the list item a note or a continuation paragraph belongs to
	// (an element index), -1 otherwise.
	Item int `json:"-"`
	// Group is what type jumps count as one: a table's rows share their
	// header's index, the notes of one blockquote share the first note's;
	// every other element is its own index.
	Group int `json:"-"`
	// Header marks a table's header row; Cols is a table row's cell count.
	Header bool `json:"header,omitempty"`
	Cols   int  `json:"cols,omitempty"`
	// Author, Date and Text are a note's: **author** date: text.
	Author   string `json:"author,omitempty"`
	Date     string `json:"date,omitempty"`
	Text     string `json:"text,omitempty"`
	Resolved bool   `json:"resolved,omitempty"` // a note marked done: ✓ after its date
	Sent     bool   `json:"sent,omitempty"`     // a note sent to an agent: · sent after its date
	// ms and me are where the render markers go in the source (-1 for an
	// element with no text to carry them: a rule, an empty item).
	ms, me int
}

// IsItem reports whether the element is a list item of any kind.
func (e Element) IsItem() bool {
	return e.Kind == KindBullet || e.Kind == KindNumbered || e.Kind == KindTask
}

// Ordered reports whether the element is an item of an ordered list.
func (e Element) Ordered() bool { return e.Marker == "." || e.Marker == ")" }

// Elements lists the blocks of a Markdown source in source order.
func Elements(source string) []Element {
	src := []byte(source)
	p := &elementParser{src: source}
	doc := parser.Parser().Parse(text.NewReader(src))
	p.children(doc, ctx{list: -1, root: -1, item: -1})
	for i := range p.out {
		e := &p.out[i]
		e.Line = 1 + strings.Count(source[:e.Start], "\n")
	}
	return p.out
}

type ctx struct {
	list, root, item int
}

type elementParser struct {
	src string
	pos int // the end of the last element: the next starts at or after it
	out []Element
}

func (p *elementParser) children(n gast.Node, c ctx) {
	for k := n.FirstChild(); k != nil; k = k.NextSibling() {
		p.node(k, c)
	}
}

// lineStart is the start of the line holding offset at.
func (p *elementParser) lineStart(at int) int {
	at = min(max(0, at), len(p.src))
	return strings.LastIndexByte(p.src[:at], '\n') + 1
}

// lineEnd is the end of the line holding offset at (before its newline).
func (p *elementParser) lineEnd(at int) int {
	at = min(max(0, at), len(p.src))
	if i := strings.IndexByte(p.src[at:], '\n'); i >= 0 {
		return at + i
	}
	return len(p.src)
}

// nextLine is the start of the line after the one holding at, or -1.
func (p *elementParser) nextLine(at int) int {
	end := p.lineEnd(at)
	if end >= len(p.src) {
		return -1
	}
	return end + 1
}

// nonblank is the start of the first nonblank line at or after at.
func (p *elementParser) nonblank(at int) int {
	at = p.lineStart(at)
	for at >= 0 && at < len(p.src) {
		if strings.TrimSpace(strings.TrimRight(p.src[at:p.lineEnd(at)], "\r")) != "" {
			return at
		}
		at = p.nextLine(at)
	}
	return min(max(0, p.pos), len(p.src))
}

// next is where the next element starts: at the line after the last
// element's end.
func (p *elementParser) next() int {
	if len(p.out) == 0 {
		return p.nonblank(0)
	}
	if at := p.nextLine(p.pos); at >= 0 {
		return p.nonblank(at)
	}
	return len(p.src)
}

// span is the first and last byte of the source lines a node's text
// covers, its descendants included; ok false when it has none.
func span(n gast.Node) (first, last int, ok bool) {
	first, last = -1, -1
	_ = gast.Walk(n, func(k gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering || k.Type() != gast.TypeBlock {
			return gast.WalkContinue, nil
		}
		lines := k.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			if first < 0 || seg.Start < first {
				first = seg.Start
			}
			if seg.Stop > last {
				last = seg.Stop
			}
		}
		return gast.WalkContinue, nil
	})
	return first, last, first >= 0
}

// trimEnd backs offset end over trailing spaces and line ends.
func (p *elementParser) trimEnd(start, end int) int {
	for end > start && strings.ContainsRune(" \t\r\n", rune(p.src[end-1])) {
		end--
	}
	return end
}

func (p *elementParser) skipSpace(at int) int {
	for at < len(p.src) && (p.src[at] == ' ' || p.src[at] == '\t') {
		at++
	}
	return at
}

func (p *elementParser) add(e Element) int {
	if e.End < e.Start {
		e.End = e.Start
	}
	e.Group = len(p.out)
	p.out = append(p.out, e)
	p.pos = max(p.pos, e.End)
	return len(p.out) - 1
}

// textElement is a leaf with lines: its markers around its text.
func (p *elementParser) textElement(kind string, n gast.Node, c ctx) Element {
	e := Element{Kind: kind, Box: -1, List: c.list, Root: c.root, Item: c.item, ms: -1, me: -1}
	first, last, ok := span(n)
	if !ok {
		e.Start = p.next()
		e.End = p.lineEnd(e.Start)
		return e
	}
	e.Start = p.lineStart(first)
	e.End = p.lineEnd(max(first, last-1))
	if last > first && p.src[last-1] == '\n' {
		e.End = p.lineEnd(last - 1)
	}
	e.ms = p.skipSpace(first)
	e.me = p.trimEnd(e.ms, last)
	if e.me <= e.ms {
		e.ms, e.me = -1, -1
	}
	e.Indent = indentOf(p.src[e.Start:e.End])
	return e
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }

var fenceLine = regexp.MustCompile("^ {0,3}(```|~~~)")

func (p *elementParser) node(n gast.Node, c ctx) {
	switch n := n.(type) {
	case *gast.Heading:
		e := p.textElement(KindHeading, n, c)
		e.Level = n.Level
		if e.ms < 0 {
			e.Start = p.next()
			e.End = p.lineEnd(e.Start)
		}
		// A setext heading's underline is its last line.
		if next := p.nextLine(e.End); next >= 0 && !strings.HasPrefix(strings.TrimSpace(p.src[e.Start:e.End]), "#") {
			if u := strings.TrimSpace(p.src[next:p.lineEnd(next)]); u != "" && strings.Trim(u, "=-") == "" {
				e.End = p.lineEnd(next)
			}
		}
		p.add(e)
	case *gast.ThematicBreak:
		start := p.next()
		p.add(Element{Kind: KindRule, Start: start, End: p.lineEnd(start), Box: -1, List: c.list, Root: c.root, Item: c.item, ms: -1, me: -1, Indent: indentOf(p.src[start:p.lineEnd(start)])})
	case *gast.FencedCodeBlock, *gast.CodeBlock:
		e := p.textElement(KindCode, n, c)
		if _, fenced := n.(*gast.FencedCodeBlock); fenced {
			// The opening fence is the first nonblank line from here; the
			// closing one the line after the content, when it is a fence.
			e.Start = p.next()
			last := e.Start
			if e.ms >= 0 {
				last = p.lineStart(e.me)
				if e.me > 0 && p.src[e.me-1] == '\n' {
					last = p.lineStart(e.me - 1)
				}
			}
			if next := p.nextLine(last); next >= 0 && fenceLine.MatchString(strings.TrimLeft(p.src[next:p.lineEnd(next)], " ")) {
				last = next
			}
			e.End = p.lineEnd(last)
			e.Indent = indentOf(p.src[e.Start:e.End])
		}
		p.add(e)
	case *gast.Blockquote:
		if c.item >= 0 && n.Parent() != nil && n.Parent().Kind() == gast.KindListItem {
			start := p.next()
			if body, ok := quoteBody(strings.TrimRight(p.src[start:p.lineEnd(start)], "\r")); ok && noteAuthor.MatchString(body) {
				p.notes(n, c)
				return
			}
		}
		e := p.textElement(KindQuote, n, c)
		e.Start = p.next()
		e.Indent = indentOf(p.src[e.Start:p.lineEnd(e.Start)])
		p.add(e)
	case *gast.List:
		list := len(p.out)
		root := c.root
		if root < 0 {
			root = list
		}
		for k := n.FirstChild(); k != nil; k = k.NextSibling() {
			if item, ok := k.(*gast.ListItem); ok {
				p.item(item, n, ctx{list: list, root: root, item: c.item})
			}
		}
	case *east.Table:
		p.table(n, c)
	case *gast.Paragraph, *gast.TextBlock, *gast.HTMLBlock:
		e := p.textElement(KindParagraph, n, c)
		if c.item >= 0 {
			e.Root = c.root
		}
		p.add(e)
	default:
		if n.HasChildren() {
			p.children(n, c)
		}
	}
}

var itemMarker = regexp.MustCompile(`^([ \t]*)([-*+]|(\d{1,9})([.)]))([ \t]+|$)(\[([ xX])\][ \t]*)?`)

// item adds a list item's own lines, then its notes, continuations and
// nested lists.
func (p *elementParser) item(n *gast.ListItem, list *gast.List, c ctx) {
	e := Element{Kind: KindBullet, Box: -1, List: c.list, Root: c.root, Item: c.item, ms: -1, me: -1}
	if list.IsOrdered() {
		e.Kind = KindNumbered
	}
	// The item's own lines: its leading text children, up to its first
	// nested list or note.
	var own []gast.Node
	k := n.FirstChild()
	for ; k != nil; k = k.NextSibling() {
		if k.Kind() == gast.KindList || k.Kind() == gast.KindBlockquote {
			break
		}
		own = append(own, k)
	}
	if len(own) > 0 {
		if first, _, ok := span(own[0]); ok {
			e.Start = p.lineStart(first)
		} else {
			e.Start = p.next()
		}
		e.End = p.lineEnd(e.Start)
		for _, o := range own {
			if _, last, ok := span(o); ok {
				e.End = max(e.End, p.lineEnd(max(0, last-1)))
				if last > 0 && p.src[last-1] != '\n' {
					e.End = max(e.End, p.lineEnd(last))
				}
			}
		}
		if first, _, ok := span(own[0]); ok {
			e.ms = p.skipSpace(first)
		}
		if _, last, ok := span(own[len(own)-1]); ok && e.ms >= 0 {
			e.me = p.trimEnd(e.ms, last)
		}
	} else {
		e.Start = p.next()
		e.End = p.lineEnd(e.Start)
	}
	line := p.src[e.Start:p.lineEnd(e.Start)]
	if m := itemMarker.FindStringSubmatchIndex(line); m != nil {
		e.Indent = m[3] - m[2]
		if m[6] >= 0 {
			e.Marker = line[m[8]:m[9]]
			e.Number, _ = strconv.Atoi(line[m[6]:m[7]])
		} else {
			e.Marker = line[m[4]:m[5]]
		}
	}
	e.Content = e.Indent + max(1, n.Offset)
	if e.Content > len(line) {
		e.Content = len(line)
	}
	// A task's box: its checkbox, and the text after it carries the markers.
	if len(own) > 0 {
		var box *east.TaskCheckBox
		_ = gast.Walk(own[0], func(k gast.Node, entering bool) (gast.WalkStatus, error) {
			if b, ok := k.(*east.TaskCheckBox); ok && entering {
				box = b
				return gast.WalkStop, nil
			}
			return gast.WalkContinue, nil
		})
		if box != nil {
			if t, ok := taskOf([]byte(p.src), box); ok {
				e.Kind, e.Box, e.Checked = KindTask, t.Box, t.Checked
				e.ms, e.me = t.start, t.end
				if e.me <= e.ms {
					e.ms, e.me = -1, -1
				}
			}
		}
	}
	if e.Kind != KindTask {
		// "- [ ]" with no text after it is a checklist item the parser does
		// not see as one; the Doc treats it as a task all the same.
		rest := strings.TrimRight(line[min(e.Content, len(line)):], " \t\r")
		if rest == "[ ]" || rest == "[x]" || rest == "[X]" {
			e.Kind, e.Box, e.Checked = KindTask, e.Start+e.Content+1, rest != "[ ]"
			e.ms, e.me = -1, -1
		}
	}
	if e.ms >= 0 && e.me <= e.ms {
		e.ms, e.me = -1, -1
	}
	at := p.add(e)
	inner := ctx{list: c.list, root: c.root, item: at}
	for ; k != nil; k = k.NextSibling() {
		p.node(k, inner)
	}
}

// table adds a row element per row; the header row includes the
// delimiter line below it.
func (p *elementParser) table(n *east.Table, c ctx) {
	group := len(p.out)
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		e := Element{Kind: KindTable, Box: -1, List: c.list, Root: c.root, Item: c.item, ms: -1, me: -1}
		_, header := row.(*east.TableHeader)
		e.Header = header
		cells := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells++
			lines := cell.Lines()
			if lines.Len() == 0 {
				continue
			}
			seg := lines.At(0)
			if e.ms < 0 {
				e.ms = seg.Start
			}
			e.me = lines.At(lines.Len() - 1).Stop
		}
		e.Cols = cells
		if e.ms >= 0 && strings.Trim(p.src[e.ms:max(e.ms, e.me)], "| \t") == "" {
			e.ms, e.me = -1, -1
		}
		if e.ms >= 0 {
			e.Start = p.lineStart(e.ms)
		} else {
			e.Start = p.next()
		}
		e.End = p.lineEnd(e.Start)
		if header {
			if next := p.nextLine(e.End); next >= 0 {
				e.End = p.lineEnd(next)
			}
		}
		e.Indent = indentOf(p.src[e.Start:p.lineEnd(e.Start)])
		i := p.add(e)
		p.out[i].Group = group
	}
}

// noteAuthor reads a note line's start: **author**, an optional date,
// its flags (✓ resolved, · sent), then a colon. Only a blockquote under a
// list item whose first line has this shape holds notes; any other is a
// quote.
var noteAuthor = regexp.MustCompile(`^\*\*([^*\n]+)\*\*(?:[ \t]+(\d{4}-\d{2}-\d{2}(?:[ T]\d{2}:\d{2})?))?((?:[ \t]*(?:✓|· sent))*)[ \t]*:[ \t]?`)

// notes adds one element per note of a blockquote under a list item: a
// line that starts with **author** date: starts a note, the lines after
// it without one continue it.
func (p *elementParser) notes(n *gast.Blockquote, c ctx) {
	start := p.next()
	_, last, ok := span(n)
	end := p.lineEnd(start)
	if ok {
		end = max(end, p.lineEnd(max(start, last-1)))
	}
	group := len(p.out)
	var cur *Element
	flush := func() {
		if cur != nil {
			i := p.add(*cur)
			p.out[i].Group = group
			cur = nil
		}
	}
	for at := start; at >= 0 && at <= end; at = p.nextLine(at) {
		lineEnd := p.lineEnd(at)
		line := strings.TrimRight(p.src[at:lineEnd], "\r")
		body, quoted := quoteBody(line)
		if !quoted && strings.TrimSpace(line) == "" {
			break
		}
		if m := noteAuthor.FindStringSubmatch(body); m != nil || cur == nil {
			flush()
			cur = &Element{Kind: KindNote, Start: at, End: lineEnd, Box: -1, List: c.list, Root: c.root, Item: c.item, ms: -1, me: -1}
			cur.Indent = indentOf(line)
			cur.Content = cur.Indent + 1
			if strings.HasPrefix(line[cur.Indent:], "> ") {
				cur.Content++
			}
			if m != nil {
				cur.Author, cur.Date = strings.TrimSpace(m[1]), m[2]
				cur.Resolved, cur.Sent = strings.Contains(m[3], "✓"), strings.Contains(m[3], "sent")
				body = body[len(m[0]):]
			}
			cur.Text = body
		} else {
			cur.End = lineEnd
			cur.Text += "\n" + body
		}
		if lineEnd >= len(p.src) {
			break
		}
	}
	flush()
}

// quoteBody is a blockquote line's text after its > and one space.
func quoteBody(line string) (string, bool) {
	t := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(t, ">") {
		return strings.TrimSpace(line), false
	}
	t = t[1:]
	t = strings.TrimPrefix(t, " ")
	return t, true
}
