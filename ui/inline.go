package ui

import (
	"sort"
	"strings"

	"ofam.dev/ochazuke/core"
)

// InlineRun is one structured inline segment inside text, table cells, or tree
// labels. Kind is "" or "text" for plain/toned text, "link" for an actionable
// link, or "badge" for an inline chip.
type InlineRun struct {
	Kind    string         `json:"kind,omitempty"`    // "" or "text", "link", "badge"
	Text    string         `json:"text,omitempty"`    // plain text (or fallback label)
	Label   string         `json:"label,omitempty"`   // display label for link/badge
	Tone    string         `json:"tone,omitempty"`    // semantic tone name
	Bg      string         `json:"bg,omitempty"`      // optional background tone
	Href    string         `json:"href,omitempty"`    // external URL or file path
	Ref     *core.Ref      `json:"ref,omitempty"`     // internal record ref (task:, lane:, document:, page:)
	Command string         `json:"command,omitempty"` // optional verb command (e.g. nav/tab switch)
	Args    map[string]any `json:"args,omitempty"`    // optional verb args
}

// InlineLink is an actionable link extracted from a sequence of InlineRuns,
// carrying its 0-based index and rune range [From, To) within the compiled text.
type InlineLink struct {
	Index   int            `json:"index"`
	From    int            `json:"from"`
	To      int            `json:"to"`
	Label   string         `json:"label"`
	Href    string         `json:"href,omitempty"`
	Ref     *core.Ref      `json:"ref,omitempty"`
	Command string         `json:"command,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
}

func cloneRef(r *core.Ref) *core.Ref {
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

func cloneArgs(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func inlineRunDisplay(r InlineRun, isLink bool) string {
	kind := strings.ToLower(strings.TrimSpace(r.Kind))
	if isLink || kind == "badge" {
		if r.Label != "" {
			return r.Label
		}
		if r.Text != "" {
			return r.Text
		}
		if r.Href != "" {
			return r.Href
		}
		if r.Ref != nil {
			if r.Ref.ID != "" {
				return r.Ref.Kind + ":" + r.Ref.ID
			}
			return r.Ref.Kind
		}
		return r.Command
	}
	if r.Text != "" {
		return r.Text
	}
	return r.Label
}

func isActionableRun(r InlineRun) bool {
	kind := strings.ToLower(strings.TrimSpace(r.Kind))
	if kind == "link" {
		return true
	}
	return r.Href != "" || r.Ref != nil || r.Command != ""
}

// CompileInlineRuns compiles runs into plain display text, per-rune ToneSpans,
// and structured InlineLinks. When activeLink >= 0, the link at that 0-based
// index receives the "link.focused" semantic tone instead of its normal tone.
func CompileInlineRuns(runs []InlineRun, activeLink int) (plain string, spans []ToneSpan, links []InlineLink) {
	if len(runs) == 0 {
		return "", nil, nil
	}
	var sb strings.Builder
	pos := 0
	for _, r := range runs {
		isLink := isActionableRun(r)
		kind := strings.ToLower(strings.TrimSpace(r.Kind))
		disp := inlineRunDisplay(r, isLink)
		n := len([]rune(disp))
		if n == 0 {
			continue
		}
		from, to := pos, pos+n
		sb.WriteString(disp)
		pos = to

		if isLink {
			linkIdx := len(links)
			links = append(links, InlineLink{
				Index:   linkIdx,
				From:    from,
				To:      to,
				Label:   disp,
				Href:    r.Href,
				Ref:     cloneRef(r.Ref),
				Command: r.Command,
				Args:    cloneArgs(r.Args),
			})
			tone := r.Tone
			bg := r.Bg
			if activeLink >= 0 && linkIdx == activeLink {
				tone = "link.focused"
			} else if tone == "" {
				tone = "link"
			}
			spans = append(spans, ToneSpan{From: from, To: to, Tone: tone, Bg: bg})
			continue
		}

		if kind == "badge" {
			tone := r.Tone
			bg := r.Bg
			if tone == "" {
				tone = "badge"
			} else if bg == "" && !strings.HasPrefix(tone, "badge.") {
				bg = "badge"
			}
			spans = append(spans, ToneSpan{From: from, To: to, Tone: tone, Bg: bg})
			continue
		}

		if r.Tone != "" || r.Bg != "" {
			spans = append(spans, ToneSpan{From: from, To: to, Tone: r.Tone, Bg: r.Bg})
		}
	}
	return sb.String(), spans, links
}

func decodeRefAny(v any) *core.Ref {
	switch x := v.(type) {
	case nil:
		return nil
	case *core.Ref:
		return cloneRef(x)
	case core.Ref:
		cp := x
		return &cp
	case map[string]any:
		k := anyString(x["kind"])
		id := anyString(x["id"])
		if k == "" && id == "" {
			return nil
		}
		return &core.Ref{Kind: k, ID: id}
	}
	return nil
}

func decodeInlineRunMap(m map[string]any, strict bool) (InlineRun, bool) {
	if len(m) == 0 {
		return InlineRun{}, false
	}
	kind := anyString(m["kind"])
	text := anyString(m["text"])
	label := anyString(m["label"])
	tone := anyString(m["tone"])
	bg := anyString(m["bg"])
	href := anyString(m["href"])
	ref := decodeRefAny(m["ref"])
	cmd := anyString(m["command"])
	var args map[string]any
	if a, ok := m["args"].(map[string]any); ok {
		args = cloneArgs(a)
	}

	lk := strings.ToLower(strings.TrimSpace(kind))
	hasKnownKind := lk == "text" || lk == "link" || lk == "badge"
	hasAction := href != "" || ref != nil || cmd != ""
	if strict {
		hasStyledText := (text != "" || label != "") && (tone != "" || bg != "")
		if !hasKnownKind && !hasAction && !hasStyledText {
			return InlineRun{}, false
		}
	} else {
		if !hasKnownKind && !hasAction && text == "" && label == "" && tone == "" && bg == "" {
			return InlineRun{}, false
		}
	}
	return InlineRun{
		Kind:    kind,
		Text:    text,
		Label:   label,
		Tone:    tone,
		Bg:      bg,
		Href:    href,
		Ref:     ref,
		Command: cmd,
		Args:    args,
	}, true
}

// DecodeInlineRuns decodes v into a slice of InlineRuns when v is []InlineRun,
// InlineRun, *InlineRun, a single run map (e.g. {"kind":"link",...}), or a
// mixed JSON array such as ["See ", {"kind":"link","label":"PR #42","href":"..."}].
// Plain strings and non-run values return (nil, false).
func DecodeInlineRuns(v any) ([]InlineRun, bool) {
	switch x := v.(type) {
	case nil, string:
		return nil, false
	case []InlineRun:
		return x, true
	case InlineRun:
		return []InlineRun{x}, true
	case *InlineRun:
		if x == nil {
			return nil, false
		}
		return []InlineRun{*x}, true
	case map[string]any:
		run, ok := decodeInlineRunMap(x, true)
		if !ok {
			return nil, false
		}
		return []InlineRun{run}, true
	case []any:
		if len(x) == 0 {
			return nil, false
		}
		out := make([]InlineRun, 0, len(x))
		for _, elem := range x {
			switch e := elem.(type) {
			case string:
				out = append(out, InlineRun{Text: e})
			case InlineRun:
				out = append(out, e)
			case *InlineRun:
				if e != nil {
					out = append(out, *e)
				}
			case map[string]any:
				run, ok := decodeInlineRunMap(e, false)
				if !ok {
					return nil, false
				}
				out = append(out, run)
			default:
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}

// resolveTextNode returns the plain text and ToneSpans for a KindText node,
// compiling props["runs"] (with optional props["activeLink"]) when present.
func resolveTextNode(n *ViewNode) (string, []ToneSpan) {
	extraSpans, _ := propDecode[[]ToneSpan](n, "spans")
	if rawRuns, ok := n.prop("runs"); ok {
		if runs, ok := DecodeInlineRuns(rawRuns); ok {
			activeLink := -1
			if v, ok := n.propInt("activeLink"); ok {
				activeLink = v
			}
			plain, spans, _ := CompileInlineRuns(runs, activeLink)
			if len(extraSpans) > 0 {
				spans = append(spans, extraSpans...)
			}
			return plain, spans
		}
	}
	return n.propString("text"), extraSpans
}

// textNodePlain returns the display text of a KindText node for measurement.
func textNodePlain(n *ViewNode) string {
	if rawRuns, ok := n.prop("runs"); ok {
		if runs, ok := DecodeInlineRuns(rawRuns); ok {
			plain, _, _ := CompileInlineRuns(runs, -1)
			return plain
		}
	}
	return n.propString("text")
}

// paintSpannedText paints text truncated to display width maxW, applying spans
// overlaid onto baseTS, and returns the ANSI string and its visible cell width.
func paintSpannedText(st *styler, text string, maxW int, baseTS ToneStyle, spans []ToneSpan) (string, int) {
	if maxW <= 0 {
		return "", 0
	}
	if len(spans) == 0 {
		txt := truncate(text, maxW)
		return st.paint(txt, baseTS), strW(txt)
	}
	rs := []rune(text)
	rsp := st.resolveSpans(spans, baseTS)
	if w := runesW(rs); w <= maxW {
		return st.runs(rs, 0, len(rs), baseTS, rsp), w
	}
	cur, cut := 0, 0
	for i, r := range rs {
		rw := runeW(r)
		if cur+rw > maxW-1 {
			break
		}
		cur += rw
		cut = i + 1
	}
	return st.runs(rs, 0, cut, baseTS, rsp) + st.paint("…", baseTS), cur + 1
}

// TableRowLinks extracts all structured InlineLinks from a table row across its
// cells in column order, numbered sequentially from 0.
func TableRowLinks(row any) []InlineLink {
	return tableRowLinksWithCols(row, nil)
}

func tableRowLinksWithCols(row any, cols []tableCol) []InlineLink {
	var cells []any
	switch x := row.(type) {
	case nil:
		return nil
	case TableRowSpec:
		cells = x.Cells
	case *TableRowSpec:
		if x == nil {
			return nil
		}
		cells = x.Cells
	case []any:
		cells = x
	case map[string]any:
		if rawCells, ok := x["cells"]; ok {
			cells = toSlice(rawCells)
		} else if len(cols) > 0 {
			cells = make([]any, len(cols))
			for i, col := range cols {
				cells[i] = x[col.key]
			}
		} else {
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			cells = make([]any, len(keys))
			for i, k := range keys {
				cells[i] = x[k]
			}
		}
	default:
		cells = toSlice(row)
	}
	var out []InlineLink
	for _, cell := range cells {
		runs, ok := DecodeInlineRuns(cell)
		if !ok {
			continue
		}
		_, _, links := CompileInlineRuns(runs, -1)
		for _, lnk := range links {
			lnk.Index = len(out)
			out = append(out, lnk)
		}
	}
	return out
}

// NodeLinks returns the structured InlineLinks for n. For KindTable and
// KindTree, rowIdx selects the logical row (defaulting to props["selected"]
// when rowIdx < 0). For KindText and KindMarkdown, rowIdx is ignored.
func NodeLinks(n *ViewNode, rowIdx int) []InlineLink {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case KindText:
		rawRuns, ok := n.prop("runs")
		if !ok {
			return nil
		}
		runs, ok := DecodeInlineRuns(rawRuns)
		if !ok {
			return nil
		}
		_, _, links := CompileInlineRuns(runs, -1)
		return links
	case KindTable:
		rows := toSlice(n.Props["rows"])
		if rowIdx < 0 {
			rowIdx = selectedOf(n)
		}
		if rowIdx < 0 || rowIdx >= len(rows) {
			return nil
		}
		return tableRowLinksWithCols(rows[rowIdx], tableCols(n))
	case KindTree:
		if rowIdx < 0 {
			rowIdx = selectedOf(n)
		}
		if rowIdx < 0 {
			return nil
		}
		for _, r := range treeRows(n) {
			if !r.spacer && r.itemIdx == rowIdx {
				if len(r.labelRuns) == 0 {
					return nil
				}
				_, _, links := CompileInlineRuns(r.labelRuns, -1)
				return links
			}
		}
		return nil
	case KindMarkdown:
		mls := MarkdownLinks(n.propString("markdown"))
		if len(mls) == 0 {
			return nil
		}
		out := make([]InlineLink, len(mls))
		for i, ml := range mls {
			out[i] = InlineLink{Index: ml.Index, Label: ml.Label, Href: ml.Href}
		}
		return out
	}
	return nil
}
