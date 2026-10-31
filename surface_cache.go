package ochazuke

import "slices"

// The input to a composer is already a value snapshot. Compare it before
// fitting rows, measuring graphemes or allocating a canvas. Page renderers
// still run, so their placements, scroll windows and public state stay live.
type surfaceStyle struct {
	key           styleKey
	chrome, roles string
	width, height int
}

type shellFrame struct {
	valid       bool
	style       surfaceStyle
	header      Header
	footer      Footer
	body, block Block
}

type modalFrame struct {
	style                         surfaceStyle
	background, title, hint, lead string
	body, block                   Block
	cells                         []HintCell
	hintWidth, x, y               int
	anchored                      bool
}

type modelSurfaces struct {
	shell  shellFrame
	modals map[string]modalFrame
}

func (m *Model) surfaceStyle() surfaceStyle {
	return surfaceStyle{m.styles.key, m.styles.chrome, m.styles.roles, m.Width, m.Height}
}

func sameBlock(a, b Block) bool {
	return a.Text == b.Text && (a.Cursor == nil && b.Cursor == nil || a.Cursor != nil && b.Cursor != nil && *a.Cursor == *b.Cursor)
}

func copyBlock(b Block) Block {
	if b.Cursor != nil {
		p := *b.Cursor
		b.Cursor = &p
	}
	return b
}

func sameEcho(a, b *KeyEchoCell) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Width == b.Width && slices.Equal(a.Labels, b.Labels)
}

func copyEcho(c *KeyEchoCell) *KeyEchoCell {
	if c == nil {
		return nil
	}
	return &KeyEchoCell{Labels: slices.Clone(c.Labels), Width: c.Width}
}

func (m *Model) shellBlock(l Layout, h Header, b Block, f Footer) Block {
	c := &m.surfaces.shell
	style := m.surfaceStyle()
	old, foot := c.header, c.footer
	same := c.valid && c.style == style && sameBlock(c.body, b) &&
		old.Title == h.Title && old.Subtitle == h.Subtitle && old.Badge == h.Badge && old.Active == h.Active && old.BadgeWidth == h.BadgeWidth &&
		slices.Equal(old.Focus, h.Focus) && slices.Equal(old.Tabs, h.Tabs) && slices.Equal(old.Segments, h.Segments) && sameEcho(old.Keys, h.Keys) &&
		foot.Text == f.Text && foot.Inspector == f.Inspector && foot.Error == f.Error && slices.Equal(foot.Hints, f.Hints) && slices.Equal(foot.Jobs, f.Jobs) && sameEcho(foot.Keys, f.Keys)
	if same && m.Runtime.Settings().Int(RenderMaxFPSSetting) > 0 {
		return copyBlock(c.block)
	}
	out := l.Shell(h, b, f)
	if m.redraw.trace != nil {
		m.redraw.trace.compositions++
	}
	h.Focus, h.Tabs, h.Segments, h.Keys = slices.Clone(h.Focus), slices.Clone(h.Tabs), slices.Clone(h.Segments), copyEcho(h.Keys)
	f.Hints, f.Jobs, f.Keys = slices.Clone(f.Hints), slices.Clone(f.Jobs), copyEcho(f.Keys)
	*c = shellFrame{valid: true, style: style, header: h, footer: f, body: copyBlock(b), block: copyBlock(out)}
	return out
}

func (m *Model) modalBlock(id string, l Layout, background, title string, body Block, hint, lead string, cells []HintCell, hintWidth int, styled func(int) (string, bool), place func(int, int) (int, int)) Block {
	key := modalFrame{style: m.surfaceStyle(), background: background, title: title, body: body, hint: hint, lead: lead, cells: cells, hintWidth: hintWidth, anchored: place != nil}
	if place != nil {
		key.x, key.y = place(0, 0)
	}
	old, ok := m.surfaces.modals[id]
	if ok && old.style == key.style && old.background == background && old.title == title && sameBlock(old.body, body) && old.hint == hint && old.lead == lead &&
		old.hintWidth == hintWidth && old.anchored == key.anchored && old.x == key.x && old.y == key.y && slices.Equal(old.cells, cells) && m.Runtime.Settings().Int(RenderMaxFPSSetting) > 0 {
		return copyBlock(old.block)
	}
	out := composeModalAt(background, title, body, hint, styled, m.Width, m.Height, l.Styles, place)
	if m.redraw.trace != nil {
		m.redraw.trace.modalCompositions++
	}
	if m.surfaces.modals == nil || len(m.surfaces.modals) > 8 {
		m.surfaces.modals = map[string]modalFrame{}
	}
	key.cells, key.body, key.block = slices.Clone(cells), copyBlock(body), copyBlock(out)
	m.surfaces.modals[id] = key
	return out
}
