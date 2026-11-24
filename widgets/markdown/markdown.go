// Package markdown is a pure, interactive Markdown reader widget model: a
// scrollable document with a heading table of contents, link navigation and
// code-block yank, rendered to ui.KindMarkdown nodes inside a ui.KindPanel
// (or a ui.KindSplit with the table of contents on the left).
//
// Scroll positions are source lines: View hands the renderer the source from
// Offset onward, so heading jumps are exact whatever the wrapping. It
// performs no I/O and imports only the standard library, core and ui.
package markdown

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

// Command IDs handled by Model.HandleCommand besides the standard motions.
const (
	CmdTOCToggle    = "markdown.toc.toggle"
	CmdTOCNext      = "markdown.toc.next"
	CmdTOCPrev      = "markdown.toc.prev"
	CmdHeadingJump  = "markdown.heading.jump"
	CmdLinkNext     = "markdown.link.next"
	CmdLinkPrev     = "markdown.link.prev"
	CmdLinkOpen     = "markdown.link.open"
	CmdCodeYank     = "markdown.code.yank"
	ActionOpen      = "open"
	ActionYank      = "yank"
	defaultPageSize = 20
)

// Specs returns the specs of the markdown-specific commands. The standard
// motions come from core.MotionSpecs.
func Specs() []core.CommandSpec {
	idx := func(desc string) []core.ParamSpec {
		return []core.ParamSpec{{Name: "index", Type: core.ParamInt, Description: desc}}
	}
	tags := []string{"markdown"}
	return []core.CommandSpec{
		{ID: CmdTOCToggle, Description: "Show or hide the table of contents", Tags: tags},
		{ID: CmdTOCNext, Description: "Select the next heading in the table of contents", Tags: tags},
		{ID: CmdTOCPrev, Description: "Select the previous heading in the table of contents", Tags: tags},
		{ID: CmdHeadingJump, Description: "Scroll to a heading (default: the selected one)", Tags: tags, Params: idx("heading index")},
		{ID: CmdLinkNext, Description: "Activate the next link", Tags: tags},
		{ID: CmdLinkPrev, Description: "Activate the previous link", Tags: tags},
		{ID: CmdLinkOpen, Description: "Open the active link", Tags: tags},
		{ID: CmdCodeYank, Description: "Copy the code block under the viewport", Tags: tags, Params: idx("code block index")},
	}
}

// Link is one [label](href) in the source. Line and Col are the 0-based
// source line and byte column of its opening bracket.
type Link struct {
	Label, Href string
	Line, Col   int
}

// CodeBlock is one fenced code block. StartLine and EndLine are the 0-based
// source lines of its opening and closing fences.
type CodeBlock struct {
	Lang, Content      string
	StartLine, EndLine int
}

// Action is what the host should do after a command: Kind "open" with
// Target (route through file handlers) or "yank" with Text.
type Action struct {
	Kind   string
	Target string
	Text   string
}

// Model is the reader state. Use pointer methods to mutate. After changing
// Source directly call Reparse (SetSource does both).
type Model struct {
	Title      string
	Source     string
	Headings   []ui.Heading
	Links      []Link
	CodeBlocks []CodeBlock

	Offset          int // first visible source line
	PageSize        int // lines per page (default 20 when <= 0)
	ScrollOff       int // vertical context lines kept around revealed targets
	ActiveLinkIdx   int // index into Links; the first link is active by default
	ShowTOC         bool
	SelectedHeading int // index into Headings
}

// New returns a model for source with defaults.
func New(title, source string) *Model {
	m := &Model{Title: title, PageSize: defaultPageSize}
	m.SetSource(source)
	return m
}

// SetSource replaces the source, re-derives headings, links and code blocks,
// and keeps the position within the new bounds.
func (m *Model) SetSource(src string) {
	m.Source = src
	m.Reparse()
}

var (
	linkRe   = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	inlineRe = regexp.MustCompile("`[^`]*`")
)

func sourceLines(src string) []string {
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(src, "\r\n", "\n"), "\n"), "\n")
}

// Reparse re-derives Headings, Links and CodeBlocks from Source.
func (m *Model) Reparse() {
	m.Headings = ui.MarkdownHeadings(m.Source)
	m.Links, m.CodeBlocks = nil, nil
	lines := sourceLines(m.Source)
	inCode := false
	var blk CodeBlock
	var body []string
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			if !inCode {
				inCode = true
				fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(ln), "```"))
				blk = CodeBlock{StartLine: i}
				if len(fields) > 0 {
					blk.Lang = fields[0]
				}
				body = nil
			} else {
				inCode = false
				blk.EndLine, blk.Content = i, strings.Join(body, "\n")
				m.CodeBlocks = append(m.CodeBlocks, blk)
			}
			continue
		}
		if inCode {
			body = append(body, ln)
			continue
		}
		masked := inlineRe.ReplaceAllStringFunc(ln, func(s string) string { return strings.Repeat(" ", len(s)) })
		for _, g := range linkRe.FindAllStringSubmatchIndex(masked, -1) {
			if g[3] > g[2] { // "!" prefix: an image, not a link
				continue
			}
			m.Links = append(m.Links, Link{Label: ln[g[4]:g[5]], Href: ln[g[6]:g[7]], Line: i, Col: g[0]})
		}
	}
	if inCode { // unterminated fence runs to the end
		blk.EndLine, blk.Content = len(lines)-1, strings.Join(body, "\n")
		m.CodeBlocks = append(m.CodeBlocks, blk)
	}
	m.SelectedHeading = max(0, min(m.SelectedHeading, len(m.Headings)-1))
	m.ActiveLinkIdx = max(0, min(m.ActiveLinkIdx, len(m.Links)-1))
	m.clamp()
}

func (m *Model) pageSize() int {
	if m.PageSize <= 0 {
		return defaultPageSize
	}
	return m.PageSize
}

func (m *Model) total() int { return len(sourceLines(m.Source)) }

func (m *Model) viewportState() traits.ViewportState {
	return traits.ViewportState{Offset: m.Offset, Total: m.total(), Height: m.pageSize()}
}

func (m *Model) viewportConfig() traits.ViewportConfig {
	return traits.ViewportConfig{ScrollOff: m.ScrollOff, Mode: traits.ViewportModeOffset, Overscroll: true}
}

func (m *Model) clamp() {
	m.Offset = traits.ViewportClamp(m.viewportState(), m.viewportConfig()).Offset
}

// syncHeading selects the last heading at or above the viewport top.
func (m *Model) syncHeading() {
	m.SelectedHeading = 0
	for i, h := range m.Headings {
		if h.Line <= m.Offset {
			m.SelectedHeading = i
		}
	}
}

func (m *Model) scrollTo(line int) {
	m.Offset = line
	m.clamp()
	m.syncHeading()
}

// reveal scrolls the minimum needed to bring line into the viewport.
func (m *Model) reveal(line int) {
	m.Offset = traits.ViewportReveal(m.viewportState(), m.viewportConfig(), line).Offset
	m.syncHeading()
}

func intArg(cmd core.Command, name string) (int, bool) {
	switch x := cmd.Args[name].(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	}
	return 0, false
}

// HandleCommand applies a motion or markdown command. The bool reports
// whether the command was handled; the Action tells the host what to do for
// markdown.link.open and markdown.code.yank (zero when there is nothing to
// open or copy).
func (m *Model) HandleCommand(cmd core.Command) (Action, bool) {
	m.clamp()
	if next, _, handled := traits.ViewportStep(m.viewportState(), m.viewportConfig(), cmd); handled {
		m.Offset = next.Offset
		m.syncHeading()
		return Action{}, true
	}
	n := cmd.Times()
	switch cmd.ID {
	case "motion.block.next", "motion.block.prev":
		next := cmd.ID == "motion.block.next"
		for ; n > 0; n-- {
			to := -1
			if next {
				for i, h := range m.Headings {
					if h.Line > m.Offset {
						to = i
						break
					}
				}
			} else {
				for i := len(m.Headings) - 1; i >= 0; i-- {
					if m.Headings[i].Line < m.Offset {
						to = i
						break
					}
				}
			}
			if to < 0 {
				break
			}
			m.scrollTo(m.Headings[to].Line)
		}
	case CmdTOCToggle:
		m.ShowTOC = !m.ShowTOC
	case CmdTOCNext, CmdTOCPrev:
		d := n
		if cmd.ID == CmdTOCPrev {
			d = -n
		}
		m.SelectedHeading = max(0, min(m.SelectedHeading+d, len(m.Headings)-1))
	case CmdHeadingJump:
		idx, ok := intArg(cmd, "index")
		if !ok {
			idx = m.SelectedHeading
		}
		if idx >= 0 && idx < len(m.Headings) {
			m.scrollTo(m.Headings[idx].Line)
			m.SelectedHeading = idx
		}
	case CmdLinkNext, CmdLinkPrev:
		if len(m.Links) > 0 {
			d := n
			if cmd.ID == CmdLinkPrev {
				d = -n
			}
			m.ActiveLinkIdx = ((m.ActiveLinkIdx+d)%len(m.Links) + len(m.Links)) % len(m.Links)
			m.reveal(m.Links[m.ActiveLinkIdx].Line)
		}
	case CmdLinkOpen:
		if m.ActiveLinkIdx >= 0 && m.ActiveLinkIdx < len(m.Links) {
			return Action{Kind: ActionOpen, Target: m.Links[m.ActiveLinkIdx].Href}, true
		}
	case CmdCodeYank:
		idx, ok := intArg(cmd, "index")
		if !ok {
			idx = m.nearestCode()
		}
		if idx >= 0 && idx < len(m.CodeBlocks) {
			return Action{Kind: ActionYank, Text: m.CodeBlocks[idx].Content}, true
		}
	default:
		return Action{}, false
	}
	return Action{}, true
}

// nearestCode returns the index of the code block under the viewport top, or
// the one nearest to it (-1 when there are none).
func (m *Model) nearestCode() int {
	best, bestDist := -1, 0
	for i, b := range m.CodeBlocks {
		var d int
		switch {
		case b.StartLine <= m.Offset && m.Offset <= b.EndLine:
			d = 0
		case b.StartLine > m.Offset:
			d = b.StartLine - m.Offset
		default:
			d = m.Offset - b.EndLine
		}
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// CurrentHeading returns the heading at or above the viewport top.
func (m *Model) CurrentHeading() (ui.Heading, bool) {
	cur, ok := ui.Heading{}, false
	for _, h := range m.Headings {
		if h.Line <= m.Offset {
			cur, ok = h, true
		}
	}
	return cur, ok
}

// visibleSource returns the source from Offset onward, enough for a few
// pages. A viewport that starts inside a code block gets its opening fence.
func (m *Model) visibleSource() string {
	lines := sourceLines(m.Source)
	start := max(0, min(m.Offset, len(lines)-1))
	end := min(len(lines), start+m.pageSize()*3)
	text := strings.Join(lines[start:end], "\n")
	for _, b := range m.CodeBlocks {
		if b.StartLine < start && start <= b.EndLine {
			text = "```" + b.Lang + "\n" + text
			break
		}
	}
	return text
}

func (m *Model) tocText() (string, []ui.ToneSpan) {
	if len(m.Headings) == 0 {
		return "(no headings)", []ui.ToneSpan{{From: 0, To: len("(no headings)"), Tone: "muted"}}
	}
	ps := m.pageSize()
	start := max(0, min(m.SelectedHeading-ps/2, len(m.Headings)-ps))
	end := min(len(m.Headings), start+ps)
	var sb strings.Builder
	var spans []ui.ToneSpan
	n := 0
	for i := start; i < end; i++ {
		h := m.Headings[i]
		if i > start {
			sb.WriteByte('\n')
			n++
		}
		line := "  "
		if i == m.SelectedHeading {
			line = "▸ "
		}
		line += strings.Repeat("  ", max(0, h.Level-1)) + h.Text
		from := n
		sb.WriteString(line)
		n += len([]rune(line))
		if i == m.SelectedHeading {
			spans = append(spans, ui.ToneSpan{From: from, To: n, Tone: "accent"})
		}
	}
	return sb.String(), spans
}

// View renders the model: a ui.KindPanel wrapping a ui.KindMarkdown node, or
// with ShowTOC a ui.KindSplit of a table-of-contents panel and that panel.
func (m *Model) View(id string) ui.ViewNode {
	m.clamp()
	md := m.visibleSource()
	title := m.Title
	if title == "" {
		title = "markdown"
	}
	var badges []ui.Badge
	active := ""
	if m.ActiveLinkIdx >= 0 && m.ActiveLinkIdx < len(m.Links) {
		active = m.Links[m.ActiveLinkIdx].Href
		badges = append(badges, ui.Badge{Text: fmt.Sprintf("link %d/%d %s", m.ActiveLinkIdx+1, len(m.Links), active), Tone: "accent"})
	}
	content := ui.ViewNode{
		ID: id + "/content", Kind: ui.KindPanel, Title: title, Badges: badges, Border: true,
		Children: []ui.ViewNode{{ID: id + "/markdown", Kind: ui.KindMarkdown, Props: map[string]any{"markdown": md}}},
	}

	h := fnv.New64a()
	fmt.Fprintf(h, "%s|%s|%v|%d|%d", md, title, badges, m.Offset, m.pageSize())
	var root ui.ViewNode
	if m.ShowTOC {
		toc, spans := m.tocText()
		fmt.Fprintf(h, "|toc|%s|%v", toc, spans)
		root = ui.ViewNode{
			ID: id, Kind: ui.KindSplit, Ratio: 0.3,
			Children: []ui.ViewNode{
				{
					ID: id + "/toc", Kind: ui.KindPanel, Title: "Contents", Border: true,
					Children: []ui.ViewNode{{ID: id + "/toc/text", Kind: ui.KindText, Props: map[string]any{"text": toc, "spans": spans}}},
				},
				content,
			},
		}
	} else {
		root = content
		root.ID = id
	}
	root.Scope, root.Focusable = "markdown", true

	cur, hasCur := m.CurrentHeading()
	root.Facts = traits.MergeFacts(traits.ViewportFacts(m.viewportState(), m.viewportConfig()), map[string]any{
		"offset":          m.Offset,
		"showTOC":         m.ShowTOC,
		"headingCount":    len(m.Headings),
		"linkCount":       len(m.Links),
		"codeBlockCount":  len(m.CodeBlocks),
		"selectedHeading": m.SelectedHeading,
		"heading":         "",
		"activeLink":      active,
		"hasLinks":        len(m.Links) > 0,
		"hasCode":         len(m.CodeBlocks) > 0,
	})
	if hasCur {
		root.Facts["heading"] = cur.Text
	}
	fmt.Fprintf(h, "|%v", root.Facts["showTOC"])
	root.CacheKey = h.Sum64() | 1
	return root
}
