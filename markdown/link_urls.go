package markdown

import (
	"bytes"
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	gparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// LinkURLsSetting chooses when a link's URL is drawn after its text:
// LinkURLsFocused (the default) draws only the text and shows the focused
// link's URL on a line under the document; LinkURLsAlways draws it after
// the text, as Glamour does; LinkURLsNever never draws it. OpenLink and
// CopyLink use the URL whatever this says.
const LinkURLsSetting = "markdown.link-urls"

// The values of LinkURLsSetting.
const (
	LinkURLsFocused = "focused"
	LinkURLsAlways  = "always"
	LinkURLsNever   = "never"
)

// LinkTone is the tone a link's text is drawn in, a row a themes.json
// entry may set like any tone (no built-in row: Glamour's link colour).
const LinkTone = "markdown.link"

// HideLinkURLs draws only a link's text, not the URL Glamour prints after
// it (what markdown.link-urls does for a Doc, for Block's callers).
func HideLinkURLs() Option { return func(o *options) { o.hideURLs = true } }

// linkURLs is the Doc's markdown.link-urls value, "focused" when unset.
func (d *Doc) linkURLs() string {
	if d.r == nil || d.r.Settings() == nil {
		return LinkURLsFocused
	}
	switch v := d.r.Settings().String(LinkURLsSetting); v {
	case LinkURLsAlways, LinkURLsNever:
		return v
	}
	return LinkURLsFocused
}

// linkFooter is the line under the document that shows the focused link's
// URL (markdown.link-urls=focused), "" when there is none to show.
func (d *Doc) linkFooter(width int) string {
	if d.edit != nil || d.linkURLs() != LinkURLsFocused || !d.list.Focused() {
		return ""
	}
	link, ok := d.FocusedLink()
	if !ok || link.URL == "" {
		return ""
	}
	// Indented as the document's text is (Glamour's document margin).
	pad := 0
	if m := styleConfig(d.style).Document.Margin; m != nil && int(*m) < width {
		pad = int(*m)
	}
	return strings.Repeat(" ", pad) + lipgloss.NewStyle().Faint(true).Render(ansi.Truncate("→ "+link.URL, max(1, width-pad), "…"))
}

// footerFits is the focused URL line (linkFooter) when height leaves it
// a line, and the height left for the rest; height 0 is unbounded.
func (d *Doc) footerFits(width, height int) (string, int) {
	footer := d.linkFooter(width)
	switch {
	case footer == "" || height == 0:
		return footer, height
	case height > 1:
		return footer, height - 1
	}
	return "", height
}

// footerLines is the footer as the lines to draw, none for "".
func footerLines(footer string) []string {
	if footer == "" {
		return nil
	}
	return []string{footer}
}

// hrefMarker stands in for a hidden URL: an OSC sequence no terminal
// sees (stripHrefs removes it), zero cells wide, so Glamour wraps the
// paragraph without the URL's text.
const hrefMarker = "\x1b]7765;och-href\a"

// styleConfigFor is the Glamour style with the link tone and, when URLs
// are hidden, link text underlined and the URL replaced by hrefMarker.
func styleConfigFor(style string, o options) gansi.StyleConfig {
	cfg := styleConfig(style)
	if o.link != "" {
		c := o.link
		cfg.LinkText.Color, cfg.Link.Color = &c, &c
	}
	if o.hideURLs {
		on := true
		cfg.LinkText.Underline = &on
		cfg.Link.Format = hrefMarker
	}
	return cfg
}

// mdRenderer draws Markdown text as Glamour's TermRenderer does.
type mdRenderer interface {
	Render(string) (string, error)
}

// newRenderer is Glamour's renderer for the width, style and options. With
// URLs hidden it is Glamour's own pipeline (glamour.NewTermRenderer's
// parser and ANSI renderer) plus linkTextTransformer, its output passed
// through stripHrefs.
func newRenderer(width int, style string, o options) (mdRenderer, error) {
	cfg := styleConfigFor(style, o)
	if !o.hideURLs {
		ropts := []glamour.TermRendererOption{glamour.WithStyles(cfg), glamour.WithWordWrap(width)}
		if o.newlines {
			ropts = append(ropts, glamour.WithPreservedNewLines())
		}
		return glamour.NewTermRenderer(ropts...)
	}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
		goldmark.WithParserOptions(gparser.WithAutoHeadingID(), gparser.WithASTTransformers(util.Prioritized(linkTextTransformer{}, 1000))),
	)
	ar := gansi.NewRenderer(gansi.Options{WordWrap: width, Styles: cfg, PreserveNewLines: o.newlines})
	md.SetRenderer(renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ar, 1000))))
	return hidingRenderer{md}, nil
}

type hidingRenderer struct{ md goldmark.Markdown }

func (h hidingRenderer) Render(in string) (string, error) {
	var b bytes.Buffer
	if err := h.md.Convert([]byte(in), &b); err != nil {
		return "", err
	}
	return stripHrefs(b.String()), nil
}

// linkTextTransformer turns a URL autolink (<https://…>, a bare www. or
// https:// link) into a link whose text is what was written, so its text
// stays drawn when the URL Glamour prints after a link is hidden. E-mail
// autolinks already draw their text and no URL.
type linkTextTransformer struct{}

func (linkTextTransformer) Transform(doc *gast.Document, reader text.Reader, _ gparser.Context) {
	src := reader.Source()
	var autos []*gast.AutoLink
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if a, ok := n.(*gast.AutoLink); ok && entering && a.AutoLinkType == gast.AutoLinkURL {
			autos = append(autos, a)
		}
		return gast.WalkContinue, nil
	})
	for _, a := range autos {
		label := a.Label(src)
		// The label is a slice of the source; its offset is how much
		// shorter its capacity is.
		at := cap(src) - cap(label)
		if len(label) == 0 || at < 0 || at+len(label) > len(src) || !bytes.Equal(src[at:at+len(label)], label) {
			continue
		}
		link := gast.NewLink()
		link.Destination = a.URL(src)
		link.AppendChild(link, gast.NewTextSegment(text.NewSegment(at, at+len(label))))
		a.Parent().ReplaceChild(a.Parent(), a, link)
	}
}

// stripHrefs removes each hrefMarker and the space Glamour draws before a
// URL. The wrapped lines only get shorter. The space is kept when only
// spaces precede it on its line (an indent, the URL wrapped to a line of
// its own).
func stripHrefs(s string) string {
	if !strings.Contains(s, hrefMarker) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		for {
			at := strings.Index(line, hrefMarker)
			if at < 0 {
				break
			}
			before := line[:at]
			if sp := spaceBefore(before); sp >= 0 {
				before = before[:sp] + before[sp+1:]
			}
			line = before + line[at+len(hrefMarker):]
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// spaceBefore is the byte offset of the last printable cell of s when it
// is a space and some other printable text precedes it, else -1.
func spaceBefore(s string) int {
	last := -1
	var state byte
	for pos, input := 0, s; len(input) > 0; {
		_, width, n, next := ansi.DecodeSequence(input, state, nil)
		if n == 0 {
			break
		}
		if width > 0 {
			last = pos
		}
		state, input, pos = next, input[n:], pos+n
	}
	if last < 0 || s[last] != ' ' || strings.TrimSpace(ansi.Strip(s[:last])) == "" {
		return -1
	}
	return last
}
