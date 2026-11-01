package markdown

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gtext "github.com/yuin/goldmark/text"
)

// Glamour draws a document as "\n", then each top-level block, then
// "\n"; a block drawn alone comes back the same way. So a document cut
// into runs of whole top-level blocks draws as the runs drawn one by
// one, each without its own first and last "\n", between one "\n" and
// another. Each run is cached, so an edit draws again only the run it
// touched: typing in one paragraph of a long document no longer runs
// Glamour over every other block.
//
// Element markers (startMarker, endMarker) and table markers carry their
// element's index, so inserting a block renumbers every marker after it.
// A run is drawn with its markers renumbered from 0 and given back their
// numbers after, so the cache still hits for the runs below an insert.

// chunkCacheBytes bounds the drawn runs kept, by their size: a long
// document at a few widths and styles fits many times over.
const chunkCacheBytes = 32 << 20

// chunkKey names a drawn run: the renderer and the text drawn, its
// markers renumbered from 0; or, with given set, the run as given, its
// lead before it and its markers as they are.
type chunkKey struct {
	renderer rendererKey
	text     string
	given    bool
}

var chunkCache = struct {
	runs  map[chunkKey]string
	bytes int
}{runs: map[chunkKey]string{}}

// refDef is a link reference or footnote definition: it reaches blocks
// in other runs, so a document holding one is drawn whole.
var refDef = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:`)

// htmlStart is a line opening an HTML block, which may run across blank
// lines: a document holding one is drawn whole.
var htmlStart = regexp.MustCompile(`(?m)^ {0,3}<`)

// listLine is a line that may continue a list (or a definition list)
// after a blank line: never the start of a run.
var listLine = regexp.MustCompile(`^(?:[-+*](?:[ \t]|$)|[0-9]{1,9}[.)](?:[ \t]|$)|:)`)

// renderRuns draws text with tr (rk names it) run by run, each run from
// the cache when it is there; the caller holds the cache lock.
func renderRuns(tr *glamour.TermRenderer, rk rendererKey, text string) (string, error) {
	runs := splitRuns(text)
	if len(runs) < 2 {
		return tr.Render(text)
	}
	outs := make([]string, 0, len(runs))
	size := 2
	lead := ""
	for i := 0; i < len(runs); i++ {
		run := runs[i]
		out, ok, err := renderRun(tr, rk, run, lead)
		// A run that leaves its last line started (a heading, a code
		// block) sets where the next run's first line starts: it is drawn
		// together with the next run.
		for err == nil && ok && !strings.HasSuffix(out, "\n") && i+1 < len(runs) {
			i++
			run += runs[i]
			out, ok, err = renderRun(tr, rk, run, lead)
		}
		if err != nil {
			return "", err
		}
		if !ok {
			return tr.Render(text)
		}
		outs = append(outs, out)
		size += len(out)
		lead = afterLine
	}
	var b strings.Builder
	b.Grow(size)
	b.WriteByte('\n')
	for _, out := range outs {
		b.WriteString(out)
	}
	b.WriteByte('\n')
	return b.String(), nil
}

// afterLine is the sentinel a run after the first is drawn behind, so it
// is drawn as in the whole document: its first heading or paragraph as
// one that follows a block (Glamour sets a first block apart), its first
// line started on a line of its own (a paragraph ends its last line).
const afterLine = "x\n\n"

// cached is text drawn by tr without Glamour's first and last "\n", from
// the cache when it is there.
func cached(tr *glamour.TermRenderer, rk rendererKey, text string) (string, error) {
	key := chunkKey{rk, text, false}
	if out, ok := chunkCache.runs[key]; ok {
		return out, nil
	}
	rendered, err := tr.Render(text)
	if err != nil {
		return "", err
	}
	out := strings.TrimSuffix(strings.TrimPrefix(rendered, "\n"), "\n")
	keep(key, out)
	return out, nil
}

// keep caches a drawn run, dropping every one kept when they would
// outgrow chunkCacheBytes.
func keep(key chunkKey, out string) {
	if chunkCache.bytes+len(key.text)+len(out) > chunkCacheBytes {
		clear(chunkCache.runs)
		chunkCache.bytes = 0
	}
	chunkCache.runs[key] = out
	chunkCache.bytes += len(key.text) + len(out)
}

// renderRun is one run drawn, without Glamour's first and last "\n",
// its markers renumbered from 0 for the cache and numbered back after;
// a run after the first is drawn behind the sentinel lead, which is cut
// off again. False when the sentinel is not drawn as alone, or leaves its
// last line started (the caller draws the whole text).
func renderRun(tr *glamour.TermRenderer, rk rendererKey, run, lead string) (string, bool, error) {
	given := chunkKey{rk, lead + run, true}
	if out, ok := chunkCache.runs[given]; ok {
		return out, true, nil
	}
	out, ok, err := renderNormalized(tr, rk, run, lead)
	if ok && err == nil {
		keep(given, out)
	}
	return out, ok, err
}

func renderNormalized(tr *glamour.TermRenderer, rk rendererKey, run, lead string) (string, bool, error) {
	norm, base := normalizeMarkers(run)
	if lead == "" {
		out, err := cached(tr, rk, norm)
		return restoreMarkers(out, base), err == nil, err
	}
	drawn, err := cached(tr, rk, lead)
	if err != nil {
		return "", false, err
	}
	if !strings.HasSuffix(drawn, "\n") {
		return "", false, nil
	}
	out, err := cached(tr, rk, lead+norm)
	if err != nil {
		return "", false, err
	}
	if !strings.HasPrefix(out, drawn) {
		return "", false, nil
	}
	return restoreMarkers(out[len(drawn):], base), true, nil
}

// glamourParser parses as Glamour does (GitHub Flavored Markdown with
// definition lists), to find where its top-level blocks start.
var glamourParser = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList)).Parser()

// splitRuns cuts text into runs of whole top-level blocks that
// concatenate to text. A run starts at a line at the left margin, after a
// blank line, that cannot continue a list, and where no top-level block
// of the parsed text holds content on both sides. A text holding a
// reference definition or an HTML block is one run.
func splitRuns(text string) []string {
	if refDef.MatchString(text) || htmlStart.MatchString(text) {
		return []string{text}
	}
	var cuts []int
	blank := false
	for at := 0; at < len(text); {
		next := len(text)
		if end := strings.IndexByte(text[at:], '\n'); end >= 0 {
			next = at + end + 1
		}
		line := strings.TrimRight(text[at:next], "\r\n")
		if blank && startsRun(line) {
			cuts = append(cuts, at)
		}
		blank = strings.TrimSpace(line) == ""
		at = next
	}
	if len(cuts) == 0 {
		return []string{text}
	}
	src := []byte(text)
	doc := glamourParser.Parse(gtext.NewReader(src))
	var spans [][2]int
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if lo, hi := contentSpan(n); lo < hi {
			spans = append(spans, [2]int{lo, hi})
		}
	}
	var runs []string
	start, k := 0, 0
	for _, at := range cuts {
		for k < len(spans) && spans[k][1] <= at {
			k++
		}
		if k < len(spans) && spans[k][0] < at {
			continue // a block holds content on both sides
		}
		runs = append(runs, text[start:at])
		start = at
	}
	return append(runs, text[start:])
}

// contentSpan is the first and last byte (end exclusive) of n's content
// lines and inline text, over its descendants (lo >= hi for none).
func contentSpan(n gast.Node) (lo, hi int) {
	lo, hi = -1, -1
	add := func(a, b int) {
		if lo < 0 || a < lo {
			lo = a
		}
		if b > hi {
			hi = b
		}
	}
	_ = gast.Walk(n, func(c gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		if c.Type() == gast.TypeBlock {
			lines := c.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				add(seg.Start, seg.Stop)
			}
		}
		if t, ok := c.(*gast.Text); ok {
			add(t.Segment.Start, t.Segment.Stop)
		}
		return gast.WalkContinue, nil
	})
	return lo, hi
}

// startsRun reports whether line, after a blank line, may start a
// top-level block: at the left margin, and not a list item or a
// definition, which can continue the block before the blank line.
func startsRun(line string) bool {
	plain := strings.TrimLeftFunc(line, isAnyMarker)
	if plain == "" || line[0] == ' ' || line[0] == '\t' || plain[0] == ' ' || plain[0] == '\t' {
		return false
	}
	return !listLine.MatchString(plain)
}

func isAnyMarker(r rune) bool {
	return r >= startMarker && r < endMarker+maxMarkers || r >= tableMarker && r < tableMarker+maxMarkers
}

// markerBase is the lowest element and table index a run's markers
// carry (-1 for none).
type markerBase struct{ element, table rune }

// normalizeMarkers renumbers run's markers from 0 (elements and tables
// apart).
func normalizeMarkers(run string) (string, markerBase) {
	base := markerBase{-1, -1}
	for _, r := range run {
		switch {
		case r >= startMarker && r < endMarker+maxMarkers:
			i := (r - startMarker) % maxMarkers
			if base.element < 0 || i < base.element {
				base.element = i
			}
		case r >= tableMarker && r < tableMarker+maxMarkers:
			if i := r - tableMarker; base.table < 0 || i < base.table {
				base.table = i
			}
		}
	}
	if base.element <= 0 && base.table <= 0 {
		return run, base
	}
	return strings.Map(func(r rune) rune {
		switch {
		case base.element > 0 && r >= startMarker && r < endMarker+maxMarkers:
			return r - base.element
		case base.table > 0 && r >= tableMarker && r < tableMarker+maxMarkers:
			return r - base.table
		}
		return r
	}, run), base
}

// restoreMarkers gives a drawn run's markers back their numbers.
func restoreMarkers(out string, base markerBase) string {
	if base.element <= 0 && base.table <= 0 {
		return out
	}
	return strings.Map(func(r rune) rune {
		switch {
		case base.element > 0 && r >= startMarker && r < endMarker+maxMarkers:
			return r + base.element
		case base.table > 0 && r >= tableMarker && r < tableMarker+maxMarkers:
			return r + base.table
		}
		return r
	}, out)
}
