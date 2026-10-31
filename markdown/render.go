// Package markdown renders Markdown with Charm's Glamour and edits its task
// lists in place. Block renders text for any view (a detail beside a list,
// a modal); a Doc is a component over a Markdown source whose task items
// are a focus stop: j and k move between them, x toggles one by rewriting
// only its box character in the source, u and ctrl+r undo and redo. It is
// a package of its own, built on ochazuke's public API like the grid, so
// only an application that imports it carries Glamour and its parsers.
// Tables are drawn by the package (table.go), not by Glamour.
package markdown

import (
	"fmt"
	"image/color"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// Style names a Glamour standard style: dark and light follow the
// appearance's background, notty is the plain style of the mono theme and
// NO_COLOR.
const (
	StyleDark  = "dark"
	StyleLight = "light"
	StylePlain = "notty"
)

// StyleOf is the Glamour style an appearance draws Markdown in.
func StyleOf(a ochazuke.Appearance) string {
	bg, plain := a.Background()
	switch {
	case plain:
		return StylePlain
	case bg == "light":
		return StyleLight
	}
	return StyleDark
}

// Option changes how Block and a Doc render Markdown.
type Option func(*options)

// options are the rendering choices; the zero value is Glamour's default.
type options struct {
	newlines bool
	wrapAll  bool
	// note and rule are the colours (#rrggbb, "" for none) of the tones
	// NoteTone and RuleTone in the appearance the text is drawn in.
	note, rule string
}

// The tones the package draws its own boxes and rules in, rows a
// themes.json entry may override like any tone: NoteTone a note's box,
// author and fold line (built in: #875f00 on light, #d7af5f on dark);
// RuleTone a table's column and header rules (no built-in row: drawn in
// the text's colour).
const (
	NoteTone = "markdown.note"
	RuleTone = "markdown.rule"
)

// withTones is o with the note and rule colours of the appearance.
func (o options) withTones(a ochazuke.Appearance) options {
	s := ochazuke.NewStyles(a)
	o.note, o.rule = hexOf(s.Tone(NoteTone).GetForeground()), hexOf(s.Tone(RuleTone).GetForeground())
	return o
}

// hexOf is c as #rrggbb, "" for none (no colour, mono).
func hexOf(c color.Color) string {
	if c == nil {
		return ""
	}
	if _, ok := c.(lipgloss.NoColor); ok {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func optionsOf(opts []Option) options {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// PreserveNewlines keeps every newline inside a paragraph a line break
// (Glamour's WithPreservedNewLines), so text written as plain lines draws
// one line per source line instead of joining a paragraph's lines into
// one. Lines longer than the width still wrap. Code blocks and tables are
// drawn the same either way.
func PreserveNewlines() Option { return func(o *options) { o.newlines = true } }

// WrapAll wraps every rendered line, including code and long unbroken
// words, instead of clipping overflow. It changes display only.
func WrapAll() Option { return func(o *options) { o.wrapAll = true } }

// Block renders Markdown text width cells wide in the appearance's style
// (StyleOf), without Glamour's blank first and last lines. The result is
// cached by text, width, style and options, so a view may call it every
// frame. Put it in a detail (ochazuke.AddDetail) to scroll it with J and K:
//
//	body, marker := detail.Window(markdown.Block(text, w, a).Text, w, h)
func Block(text string, width int, a ochazuke.Appearance, opts ...Option) ochazuke.Block {
	return ochazuke.Text(strings.Join(render(text, 0, width, StyleOf(a), optionsOf(opts).withTones(a)).lines, "\n"))
}

// renderKey names one rendering: the text, how many task markers it
// carries (marked), the width, the style and the options.
type renderKey struct {
	text  string
	tasks int
	width int
	style string
	opts  options
}

// rendering is a cached rendering: its lines, markers stripped, and per
// task the first and last line it covers.
type rendering struct {
	lines []string
	spans [][2]int
}

type rendererKey struct {
	width int
	style string
	opts  options
}

// cacheSize bounds the rendered texts kept: a frame renders each visible
// document once, so a few dozen cover every page and width a session uses.
const cacheSize = 64

var cache = struct {
	sync.Mutex
	renderings map[renderKey]rendering
	order      []renderKey
	renderers  map[rendererKey]*glamour.TermRenderer
}{renderings: map[renderKey]rendering{}, renderers: map[rendererKey]*glamour.TermRenderer{}}

// render is the cached rendering of text as lines no wider than width;
// tasks is how many task markers text carries (0 for none). The lines are
// shared: a caller copies before changing one.
func render(text string, tasks, width int, style string, opts options) rendering {
	width = max(1, width)
	key := renderKey{text, tasks, width, style, opts}
	cache.Lock()
	defer cache.Unlock()
	if out, ok := cache.renderings[key]; ok {
		return out
	}
	lines := renderUncached(text, width, style, opts)
	out := rendering{lines: lines, spans: spans(lines, tasks)}
	if len(cache.order) >= cacheSize {
		delete(cache.renderings, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.renderings[key] = out
	cache.order = append(cache.order, key)
	return out
}

// renderUncached runs Glamour (the caller holds the cache lock, which also
// serialises the shared renderers). A text Glamour cannot render is shown
// as it is, wrapped.
func renderUncached(text string, width int, style string, opts options) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	text, tables := cutTables(text)
	rk := rendererKey{width, style, opts}
	tr := cache.renderers[rk]
	if tr == nil {
		ropts := []glamour.TermRendererOption{glamour.WithStyles(styleConfig(style)), glamour.WithWordWrap(width)}
		if opts.newlines {
			ropts = append(ropts, glamour.WithPreservedNewLines())
		}
		var err error
		tr, err = glamour.NewTermRenderer(ropts...)
		if err == nil {
			if len(cache.renderers) >= 16 {
				clear(cache.renderers)
			}
			cache.renderers[rk] = tr
		}
	}
	out := ""
	if tr != nil {
		if rendered, err := tr.Render(text); err == nil {
			out = rendered
		}
	}
	if out == "" {
		out = ansi.Wrap(text, width, "")
	}
	lines := spliceTables(strings.Split(out, "\n"), tables, width, style, opts.rule)
	blank := func(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if opts.wrapAll {
		return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n")
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return lines
}

func styleConfig(style string) gansi.StyleConfig {
	switch style {
	case StyleLight:
		return styles.LightStyleConfig
	case StylePlain:
		return styles.NoTTYStyleConfig
	}
	return styles.DarkStyleConfig
}
