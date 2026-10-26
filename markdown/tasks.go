package markdown

import (
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Task is one task list item of a Markdown source, as goldmark (Glamour's
// parser, with the GFM task list extension) finds it: a list item whose
// first line starts with [ ], [x] or [X]. A "- [ ]" inside a code block,
// or anywhere else that is not a list item, is not a task.
type Task struct {
	// Box is the byte offset of the box character, the one between [ and
	// ]: a toggle rewrites that byte and nothing else.
	Box     int    `json:"box"`
	Checked bool   `json:"checked"`
	Text    string `json:"text"` // the item's first line after the box
	Line    int    `json:"line"` // 1-based source line of the box
	// start and end are the byte offsets of the item's text: the first
	// line after the box and its spaces, and the end of the item's first
	// paragraph.
	start, end int
}

// parser is goldmark as Glamour configures it, so the tasks found are the
// ones Glamour draws.
var parser = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList))

// Tasks lists the task items of a Markdown source in source order, nested
// items included.
func Tasks(source string) []Task {
	src := []byte(source)
	doc := parser.Parser().Parse(text.NewReader(src))
	var out []Task
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		box, ok := n.(*east.TaskCheckBox)
		if !entering || !ok {
			return gast.WalkContinue, nil
		}
		if t, ok := taskOf(src, box); ok {
			out = append(out, t)
		}
		return gast.WalkContinue, nil
	})
	return out
}

// taskOf locates a checkbox in the source: its paragraph's first line
// starts at the [ (after any padding), the box character is the next byte.
func taskOf(src []byte, box *east.TaskCheckBox) (Task, bool) {
	parent := box.Parent()
	if parent == nil || parent.Type() != gast.TypeBlock {
		return Task{}, false
	}
	lines := parent.Lines()
	if lines.Len() == 0 {
		return Task{}, false
	}
	at := lines.At(0).Start
	for at < len(src) && (src[at] == ' ' || src[at] == '\t') {
		at++
	}
	if at+2 >= len(src) || src[at] != '[' || src[at+2] != ']' {
		return Task{}, false
	}
	start := at + 3
	for start < len(src) && (src[start] == ' ' || src[start] == '\t') {
		start++
	}
	eol := len(src)
	if i := strings.IndexByte(string(src[start:]), '\n'); i >= 0 {
		eol = start + i
	}
	end := lines.At(lines.Len() - 1).Stop
	for end > start && strings.ContainsRune(" \t\r\n", rune(src[end-1])) {
		end--
	}
	return Task{
		Box:     at + 1,
		Checked: box.IsChecked,
		Text:    strings.TrimRight(string(src[start:eol]), " \t\r"),
		Line:    1 + strings.Count(string(src[:at]), "\n"),
		start:   start,
		end:     max(start, end),
	}, true
}

// Markers: each task's text is rendered with a start marker before it and
// an end marker after it, single private-use runes (one per task, so
// wrapping cannot split one), which the rendered lines are searched for
// and stripped from. The line holding a start marker is the task's first
// line on screen, the one holding its end marker its last.
const (
	startMarker = rune(0xF0000)
	endMarker   = rune(0xF8000)
	maxMarkers  = 0x8000
)

// marked is the source with every task's markers in place.
func marked(source string, tasks []Task) string {
	var b strings.Builder
	at := 0
	for i, t := range tasks {
		if i >= maxMarkers {
			break
		}
		b.WriteString(source[at:t.start])
		b.WriteRune(startMarker + rune(i))
		b.WriteString(source[t.start:t.end])
		b.WriteRune(endMarker + rune(i))
		at = t.end
	}
	b.WriteString(source[at:])
	return b.String()
}

// spans finds and strips the markers of n tasks in rendered lines: per
// task the first and last line it covers ({-1, -1} when Glamour dropped
// it). A stripped marker's cell is given back as a space at the line's
// end, so every line keeps Glamour's width.
func spans(lines []string, n int) [][2]int {
	out := make([][2]int, n)
	for i := range out {
		out[i] = [2]int{-1, -1}
	}
	if n == 0 {
		return out
	}
	for i, line := range lines {
		if !strings.ContainsFunc(line, func(r rune) bool { return isMarker(r, n) }) {
			continue
		}
		var b strings.Builder
		stripped := 0
		for _, r := range line {
			if !isMarker(r, n) {
				b.WriteRune(r)
				continue
			}
			stripped++
			if r >= endMarker {
				k := int(r - endMarker)
				out[k][1] = i
			} else {
				k := int(r - startMarker)
				if out[k][0] < 0 {
					out[k][0] = i
				}
			}
		}
		lines[i] = b.String() + strings.Repeat(" ", stripped)
	}
	for i := range out {
		switch {
		case out[i][0] < 0:
			out[i] = [2]int{-1, -1}
		case out[i][1] < out[i][0]:
			out[i][1] = out[i][0]
		}
	}
	return out
}

func isMarker(r rune, n int) bool {
	return r >= startMarker && r < startMarker+rune(n) || r >= endMarker && r < endMarker+rune(n)
}
