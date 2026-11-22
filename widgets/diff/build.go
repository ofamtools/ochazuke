package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// contextLines is the number of unchanged lines kept around each change.
const contextLines = 3

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// ParseUnified parses unified diff text, including `git diff` output with
// `diff --git` headers, into a Document with one Section per file.
// Hunks end by their declared line counts, so removed lines that look like
// file headers ("--- x") are read correctly. Unknown header lines (index,
// mode, rename, binary notices) are ignored.
func ParseUnified(src string) Document {
	var doc Document
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	cur := -1
	fileHdr := false
	var hunk *Hunk
	oldRem, newRem, oldNo, newNo := 0, 0, 0, 0

	flush := func() {
		if hunk != nil && cur >= 0 {
			doc.Sections[cur].Hunks = append(doc.Sections[cur].Hunks, *hunk)
		}
		hunk = nil
	}
	newSection := func(s Section) {
		flush()
		doc.Sections = append(doc.Sections, s)
		cur = len(doc.Sections) - 1
		fileHdr = false
	}

	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if hunk != nil && (oldRem > 0 || newRem > 0) {
			if strings.HasPrefix(ln, `\`) {
				continue
			}
			c := byte(0)
			if ln != "" {
				c = ln[0]
			}
			text := ""
			if ln != "" {
				text = ln[1:]
			}
			switch {
			case (c == ' ' || (ln == "" && i < len(lines)-1)) && oldRem > 0 && newRem > 0:
				hunk.Lines = append(hunk.Lines, Line{Kind: LineContext, OldNum: oldNo, NewNum: newNo, Text: text})
				oldNo, newNo, oldRem, newRem = oldNo+1, newNo+1, oldRem-1, newRem-1
				continue
			case c == '-' && oldRem > 0:
				hunk.Lines = append(hunk.Lines, Line{Kind: LineRemoved, OldNum: oldNo, Text: text})
				oldNo, oldRem = oldNo+1, oldRem-1
				continue
			case c == '+' && newRem > 0:
				hunk.Lines = append(hunk.Lines, Line{Kind: LineAdded, NewNum: newNo, Text: text})
				newNo, newRem = newNo+1, newRem-1
				continue
			}
		}
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			oldP, newP := splitGitPaths(strings.TrimPrefix(ln, "diff --git "))
			newSection(Section{ID: pathOf(oldP, newP), Title: pathOf(oldP, newP), OldLabel: oldP, NewLabel: newP})
		case strings.HasPrefix(ln, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			oldL := labelOf(ln[4:])
			newL := labelOf(lines[i+1][4:])
			if cur < 0 || fileHdr {
				newSection(Section{})
			}
			flush()
			s := &doc.Sections[cur]
			s.OldLabel, s.NewLabel = oldL, newL
			if p := pathOf(trimSide(oldL), trimSide(newL)); p != "" && (s.ID == "" || s.Title == "") {
				s.ID, s.Title = p, p
			}
			fileHdr = true
			i++
		case hunkRe.MatchString(ln):
			flush()
			if cur < 0 {
				newSection(Section{})
			}
			g := hunkRe.FindStringSubmatch(ln)
			h := Hunk{Header: ln, OldStart: atoi(g[1]), OldCount: countOf(g[2]), NewStart: atoi(g[3]), NewCount: countOf(g[4])}
			hunk = &h
			oldNo, newNo, oldRem, newRem = h.OldStart, h.NewStart, h.OldCount, h.NewCount
		case strings.HasPrefix(ln, "rename to ") && cur >= 0:
			p := strings.TrimSpace(strings.TrimPrefix(ln, "rename to "))
			doc.Sections[cur].ID, doc.Sections[cur].Title = p, p
		}
	}
	flush()
	return doc
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func countOf(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}

// splitGitPaths splits "a/x b/y" into its two labels.
func splitGitPaths(rest string) (oldP, newP string) {
	if i := strings.Index(rest, " b/"); i >= 0 {
		return rest[:i], rest[i+1:]
	}
	f := strings.Fields(rest)
	switch len(f) {
	case 0:
		return "", ""
	case 1:
		return f[0], f[0]
	}
	return f[0], f[1]
}

// labelOf drops a trailing tab-separated timestamp from a ---/+++ label.
func labelOf(s string) string {
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// trimSide strips the a/ or b/ prefix git puts on labels; /dev/null stays.
func trimSide(l string) string {
	if strings.HasPrefix(l, "a/") || strings.HasPrefix(l, "b/") {
		return l[2:]
	}
	return l
}

// pathOf picks the display path of a file: the new one unless it is
// /dev/null (a deletion).
func pathOf(oldP, newP string) string {
	oldP, newP = trimSide(oldP), trimSide(newP)
	if newP != "" && newP != "/dev/null" {
		return newP
	}
	if oldP == "/dev/null" {
		return ""
	}
	return oldP
}

// --- line diff ----------------------------------------------------------------

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	return strings.Split(s, "\n")
}

// FromStrings diffs two texts line by line (Myers) and returns a Document
// with one Section and hunks carrying 3 lines of context. Equal texts give a
// Document with no sections.
func FromStrings(title, oldLabel, newLabel, oldText, newText string) Document {
	doc := Document{Title: title}
	hunks := hunksFromLines(splitLines(oldText), splitLines(newText))
	if len(hunks) == 0 {
		return doc
	}
	id := title
	if id == "" {
		id = newLabel
	}
	doc.Sections = []Section{{ID: id, Title: id, OldLabel: oldLabel, NewLabel: newLabel, Hunks: hunks}}
	return doc
}

// diffLines returns the full line script (context, removed, added) with line
// numbers set.
func diffLines(a, b []string) []Line {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var out []Line
	oldNo, newNo := 1, 1
	emit := func(k LineKind, text string) {
		l := Line{Kind: k, Text: text}
		switch k {
		case LineContext:
			l.OldNum, l.NewNum = oldNo, newNo
			oldNo, newNo = oldNo+1, newNo+1
		case LineRemoved:
			l.OldNum = oldNo
			oldNo++
		case LineAdded:
			l.NewNum = newNo
			newNo++
		}
		out = append(out, l)
	}
	for _, s := range a[:pre] {
		emit(LineContext, s)
	}
	for _, e := range myers(a[pre:len(a)-suf], b[pre:len(b)-suf]) {
		emit(e.kind, e.text)
	}
	for _, s := range a[len(a)-suf:] {
		emit(LineContext, s)
	}
	return out
}

type edit struct {
	kind LineKind
	text string
}

// myers computes a shortest edit script between a and b.
func myers(a, b []string) []edit {
	n, m := len(a), len(b)
	if n+m == 0 {
		return nil
	}
	maxD := n + m
	off := maxD + 1
	v := make([]int, 2*maxD+4)
	var trace [][]int // trace[d] is v[-d-1..d+1] at the start of round d
	finalD := -1
search:
	for d := 0; d <= maxD; d++ {
		snap := make([]int, 2*d+3)
		copy(snap, v[off-d-1:off+d+2])
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				finalD = d
				break search
			}
		}
	}
	var rev []edit
	x, y := n, m
	for d := finalD; d >= 0; d-- {
		snap := trace[d]
		at := func(k int) int { return snap[k+d+1] }
		k := x - y
		var prevK int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := at(prevK)
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, edit{LineContext, a[x-1]})
			x, y = x-1, y-1
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, edit{LineAdded, b[prevY]})
			} else {
				rev = append(rev, edit{LineRemoved, a[prevX]})
			}
		}
		x, y = prevX, prevY
	}
	out := make([]edit, len(rev))
	for i, e := range rev {
		out[len(rev)-1-i] = e
	}
	return out
}

// hunksFromLines diffs a and b and groups the changes into hunks with
// contextLines of context; changes closer than twice that share a hunk.
func hunksFromLines(a, b []string) []Hunk {
	all := diffLines(a, b)
	var changes []int
	oldBefore := make([]int, len(all)+1)
	newBefore := make([]int, len(all)+1)
	for i, l := range all {
		oldBefore[i+1], newBefore[i+1] = oldBefore[i], newBefore[i]
		if l.Kind != LineAdded {
			oldBefore[i+1]++
		}
		if l.Kind != LineRemoved {
			newBefore[i+1]++
		}
		if l.Kind != LineContext {
			changes = append(changes, i)
		}
	}
	var hunks []Hunk
	for i := 0; i < len(changes); {
		j := i
		for j+1 < len(changes) && changes[j+1]-changes[j] <= 2*contextLines+1 {
			j++
		}
		start := max(0, changes[i]-contextLines)
		stop := min(len(all)-1, changes[j]+contextLines)
		h := Hunk{Lines: append([]Line(nil), all[start:stop+1]...)}
		for _, l := range h.Lines {
			if l.Kind != LineAdded {
				h.OldCount++
			}
			if l.Kind != LineRemoved {
				h.NewCount++
			}
		}
		h.OldStart, h.NewStart = oldBefore[start]+1, newBefore[start]+1
		if h.OldCount == 0 {
			h.OldStart--
		}
		if h.NewCount == 0 {
			h.NewStart--
		}
		h.Header = hunkHeader(h)
		hunks = append(hunks, h)
		i = j + 1
	}
	return hunks
}

// --- JSON ---------------------------------------------------------------------

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// FromJSON diffs two JSON values structurally and returns one Section per
// changed path (ID and Title like "$.a.b[2].c"), holding the canonical
// (sorted-key, indented) old and new values as removed and added lines.
// Key order and whitespace never matter, so equal values give a Document with
// no sections. Values may be any JSON-marshalable Go value; []byte and
// json.RawMessage are parsed as JSON text.
func FromJSON(title string, oldVal, newVal any) Document {
	doc := Document{Title: title}
	jsonWalk(&doc, "$", canonical(oldVal), canonical(newVal), true, true)
	return doc
}

func canonical(v any) any {
	var raw []byte
	switch x := v.(type) {
	case []byte:
		raw = x
	case json.RawMessage:
		raw = x
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		raw = b
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return string(raw)
	}
	return out
}

func jsonWalk(doc *Document, path string, a, b any, hasA, hasB bool) {
	if hasA && hasB {
		switch x := a.(type) {
		case map[string]any:
			if y, ok := b.(map[string]any); ok {
				keys := map[string]bool{}
				for k := range x {
					keys[k] = true
				}
				for k := range y {
					keys[k] = true
				}
				sorted := make([]string, 0, len(keys))
				for k := range keys {
					sorted = append(sorted, k)
				}
				sort.Strings(sorted)
				for _, k := range sorted {
					av, aok := x[k]
					bv, bok := y[k]
					jsonWalk(doc, joinKey(path, k), av, bv, aok, bok)
				}
				return
			}
		case []any:
			if y, ok := b.([]any); ok {
				for i := 0; i < max(len(x), len(y)); i++ {
					var av, bv any
					if i < len(x) {
						av = x[i]
					}
					if i < len(y) {
						bv = y[i]
					}
					jsonWalk(doc, fmt.Sprintf("%s[%d]", path, i), av, bv, i < len(x), i < len(y))
				}
				return
			}
		}
		if reflect.DeepEqual(a, b) {
			return
		}
	}
	var oldLines, newLines []string
	if hasA {
		oldLines = splitLines(pretty(a))
	}
	if hasB {
		newLines = splitLines(pretty(b))
	}
	doc.Sections = append(doc.Sections, Section{
		ID: path, Title: path,
		OldLabel: compact(a, hasA), NewLabel: compact(b, hasB),
		Hunks: hunksFromLines(oldLines, newLines),
	})
}

func joinKey(path, k string) string {
	if identRe.MatchString(k) {
		return path + "." + k
	}
	return fmt.Sprintf("%s[%q]", path, k)
}

func pretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func compact(v any, has bool) string {
	if !has {
		return "(absent)"
	}
	b, err := json.Marshal(v)
	s := fmt.Sprint(v)
	if err == nil {
		s = string(b)
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	return s
}
