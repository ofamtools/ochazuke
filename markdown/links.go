package markdown

import (
	"errors"
	"html"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"ofam.dev/ochazuke"
)

// LinkStopsSetting lets j/k visit inline links. False keeps element-only motion.
const LinkStopsSetting = "markdown.link-stops"

// Link is a rendered link in a document element. Line is its first drawn line.
type Link struct {
	URL     string `json:"url"`
	Text    string `json:"text"`
	Element int    `json:"element"`
	Line    int    `json:"line"`
	runs    []linkRun
}

type linkRun struct{ line, start, end int }
type linkCell struct {
	linkRun
	text, url, group string
}

// linkCells reads the renderer's OSC 8 spans without changing layout. Cell
// coordinates survive wrapping, styles, wide characters and table alignment.
func linkCells(lines []string, top, bottom int) []linkCell {
	var out []linkCell
	url, group := "", ""
	for l := top; l <= bottom; l++ {
		col := 0
		var state byte
		for input := lines[l]; len(input) > 0; {
			seq, width, n, next := ansi.DecodeSequence(input, state, nil)
			if n == 0 {
				break
			}
			state, input = next, input[n:]
			if strings.HasPrefix(seq, "\x1b]8;") {
				value := strings.TrimSuffix(strings.TrimSuffix(seq, "\x1b\\"), "\a")
				group, url, _ = strings.Cut(value[4:], ";")
			}
			if width > 0 || (len(seq) > 0 && seq[0] >= 0x20 && seq[0] != 0x7f) {
				for _, r := range seq {
					if !unicode.IsSpace(r) {
						out = append(out, linkCell{linkRun{l, col, col + width}, string(r), url, group})
					}
				}
				col += width
			}
		}
	}
	return out
}

func compact(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// layoutLinks matches parsed links to the hyperlink cells Glamour and the
// table renderer already emit. No extra printable markers affect wrapping.
func layoutLinks(source string, els []Element, lay layout) []Link {
	src := []byte(source)
	doc := parser.Parser().Parse(text.NewReader(src))
	cells := make(map[int][]linkCell)
	used := make(map[int]map[int]bool)
	tableLinks := make(map[int]int)
	var links []Link
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		var url, label string
		auto := false
		switch n := n.(type) {
		case *gast.Link:
			url, label = string(n.Destination), string(n.Text(src)) //nolint:staticcheck
		case *gast.AutoLink:
			url, label, auto = string(n.URL(src)), string(n.Label(src)), true
			if n.AutoLinkType == gast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
				url = "mailto:" + url
			}
		default:
			return gast.WalkContinue, nil
		}
		at := -1
		_ = gast.Walk(n, func(child gast.Node, enter bool) (gast.WalkStatus, error) {
			if t, ok := child.(*gast.Text); enter && ok {
				at = t.Segment.Start
				return gast.WalkStop, nil
			}
			return gast.WalkContinue, nil
		})
		if at < 0 {
			for p := n.Parent(); p != nil; p = p.Parent() {
				if p.Type() == gast.TypeBlock && p.Lines().Len() > 0 {
					at = p.Lines().At(0).Start
					break
				}
			}
		}
		e := -1
		for i, el := range els {
			if at >= el.Start && at <= el.End {
				e = i
				break
			}
		}
		group := ""
		if e >= 0 && els[e].Kind == KindTable {
			tableLinks[els[e].Group]++
			group = "id=och-md-" + strconv.Itoa(tableLinks[els[e].Group])
		}
		if e < 0 || lay.spans[e][0] < 0 {
			return gast.WalkSkipChildren, nil
		}
		if cells[e] == nil {
			cells[e] = linkCells(lay.lines, lay.spans[e][0], lay.spans[e][1])
			used[e] = make(map[int]bool)
		}
		cc := cells[e]
		label = html.UnescapeString(string(util.UnescapePunctuations([]byte(label))))
		needle := label
		if !auto && !strings.HasPrefix(url, "#") && (els[e].Kind != KindTable || url != label) {
			needle += url
		}
		if auto && !strings.HasPrefix(url, "mailto:") {
			needle = url
		}
		needle = compact(needle)
		// Valid OSC links use only their own cells. Fragment links and notes,
		// which Glamour does not hyperlink, use their visible label instead.
		find := func(tagged bool, needle string) []linkCell {
			var b strings.Builder
			var indices []int
			for i := 0; i < len(cc); i++ {
				if !used[e][i] && (!tagged || (cc[i].url == url && (group == "" || cc[i].group == group))) {
					b.WriteString(cc[i].text)
					indices = append(indices, i)
				}
			}
			if needle == "" {
				return nil
			}
			want := []rune(needle)
			skip := 0
			if !tagged {
				skip = strings.Count(compact(inlineBefore(n, src)), needle)
			}
			start, end, matchedCount := 0, 0, 0
			for i, r := range []rune(b.String()) {
				if r == want[matchedCount] {
					if matchedCount == 0 {
						start = i
					}
					matchedCount++
					if matchedCount == len(want) {
						if skip > 0 {
							skip--
							matchedCount = 0
							continue
						}
						end = i + 1
						break
					}
				}
			}
			if end == 0 {
				return nil
			}
			// Include mono's visible emphasis delimiters before the label.
			for start > 0 && strings.ContainsAny(cc[indices[start-1]].text, "*_~") {
				start--
			}
			matched := make([]linkCell, end-start)
			for i := start; i < end; i++ {
				matched[i-start] = cc[indices[i]]
			}
			for i := start; i < end; i++ {
				used[e][indices[i]] = true
			}
			return matched
		}
		matched := find(true, needle)
		if len(matched) == 0 {
			matched = find(true, compact(label))
		}
		if len(matched) == 0 && (strings.HasPrefix(url, "#") || els[e].Kind == KindNote) {
			matched = find(false, compact(label))
		}
		if len(matched) > 0 {
			link := Link{URL: url, Text: label, Element: e, Line: matched[0].line}
			for _, c := range matched {
				last := len(link.runs) - 1
				if last >= 0 && link.runs[last].line == c.line && c.start <= link.runs[last].end+1 {
					link.runs[last].end = c.end
				} else {
					link.runs = append(link.runs, c.linkRun)
				}
			}
			links = append(links, link)
		}
		return gast.WalkSkipChildren, nil
	})
	sort.SliceStable(links, func(i, j int) bool {
		a, b := links[i].runs[0], links[j].runs[0]
		if a.line != b.line {
			return a.line < b.line
		}
		return a.start < b.start
	})
	return links
}

// inlineBefore is the ordinary inline text before n in its block. Earlier
// links are omitted because their cells have already been matched.
func inlineBefore(n gast.Node, src []byte) string {
	block := n.Parent()
	for block != nil && block.Type() != gast.TypeBlock {
		block = block.Parent()
	}
	if block == nil {
		return ""
	}
	var b strings.Builder
	_ = gast.Walk(block, func(k gast.Node, enter bool) (gast.WalkStatus, error) {
		if !enter {
			return gast.WalkContinue, nil
		}
		if k == n {
			return gast.WalkStop, nil
		}
		switch k := k.(type) {
		case *gast.Link, *gast.AutoLink:
			return gast.WalkSkipChildren, nil
		case *gast.Text:
			b.Write(k.Segment.Value(src))
		case *gast.String:
			b.Write(k.Value)
		}
		return gast.WalkContinue, nil
	})
	return html.UnescapeString(string(util.UnescapePunctuations([]byte(b.String()))))
}

// Links returns the links currently drawn, in document and cell order.
func (d *Doc) Links() []Link { return append([]Link(nil), d.links...) }

// FocusedLink returns the link at the cursor, if any. The element remains
// the context for task, editing and component verbs.
func (d *Doc) FocusedLink() (Link, bool) {
	if d.edit != nil {
		return Link{}, false
	}
	e, _ := d.cursor()
	if e != d.linkElement || d.cursorLine() != d.linkLine {
		d.linkElement, d.linkIndex, d.linkExplicit = e, -1, false
		if d.r.Settings().Bool(LinkStopsSetting) {
			d.linkIndex = d.firstLink(e)
			if !d.itemCursor() && d.linkIndex >= 0 && d.links[d.linkIndex].Line != d.cursorLine() {
				d.linkIndex = -1
			}
			if d.linkIndex >= 0 && d.itemCursor() {
				for i, l := range d.links {
					if l.Element == e && abs(l.Line-d.cursorLine()) < abs(d.links[d.linkIndex].Line-d.cursorLine()) {
						d.linkIndex = i
					}
				}
			}
		}
		d.linkLine = d.cursorLine()
	}
	if d.linkIndex < 0 && d.r.Settings().Bool(LinkStopsSetting) && d.itemCursor() {
		d.linkIndex = d.firstLink(e)
	}
	if d.linkIndex < 0 || d.linkIndex >= len(d.links) || d.links[d.linkIndex].Element != e {
		return Link{}, false
	}
	if !d.r.Settings().Bool(LinkStopsSetting) && !d.linkExplicit {
		return Link{}, false
	}
	return d.links[d.linkIndex], true
}

func (d *Doc) firstLink(e int) int {
	for i, l := range d.links {
		if l.Element == e {
			return i
		}
	}
	return -1
}

func (d *Doc) selectLink(i int, explicit bool) error {
	if i < 0 || i >= len(d.links) {
		return errors.New("no link")
	}
	l := d.links[i]
	if err := d.selectLine(l.Line); err != nil {
		return err
	}
	d.linkIndex, d.linkElement, d.linkLine, d.linkExplicit = i, l.Element, l.Line, explicit
	return nil
}

// MoveLink jumps between links, skipping elements without links, and clamps
// at the first or last link. It also works with link stops turned off.
func (d *Doc) MoveLink(delta, count int) error {
	if d.edit != nil {
		return errors.New("Markdown links are text while editing")
	}
	current, ok := d.FocusedLink()
	e, _ := d.cursor()
	at := -1
	for n := 0; n < max(1, count); n++ {
		next := -1
		if delta >= 0 {
			for i, l := range d.links {
				if (ok && i > d.linkIndex) || (!ok && l.Element >= e) {
					next = i
					break
				}
			}
		} else {
			for i := len(d.links) - 1; i >= 0; i-- {
				if (ok && i < d.linkIndex) || (!ok && d.links[i].Element <= e) {
					next = i
					break
				}
			}
		}
		if next < 0 {
			break
		}
		at = next
		_ = d.selectLink(at, true)
		current, ok = d.FocusedLink()
		e = current.Element
	}
	if at < 0 && !ok {
		return errors.New("no link in that direction")
	}
	return nil
}

// OpenLink opens the focused URL through OpenURL. The default uses the
// platform handler; a host or test can inject an opener on the Doc.
func (d *Doc) OpenLink() error {
	l, ok := d.FocusedLink()
	if !ok {
		return errors.New("no Markdown link focused")
	}
	open := d.OpenURL
	if open == nil {
		open = openURL
	}
	return open(l.URL)
}

// CopyLink yanks the focused URL into the framework's active register.
func (d *Doc) CopyLink() error {
	l, ok := d.FocusedLink()
	if !ok {
		return errors.New("no Markdown link focused")
	}
	return d.r.StoreYank(ochazuke.Yank{Text: l.URL, Shape: "char"}, "", false)
}

func openURL(url string) error {
	name, args := "xdg-open", []string{url}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
