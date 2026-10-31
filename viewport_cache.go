package ochazuke

import "charm.land/bubbles/v2/viewport"

type viewportFrame struct {
	content               string
	generation            uint64
	width, height, offset int
	drawnGeneration       uint64
	text                  string
	valid                 bool
}

type modelViewports map[string]*viewportFrame

// Exact content equality is the content generation for arbitrary app
// renderers: no new revision API or cooperation from the consumer is needed.
// SetContent scans every line for width, so do it only when the text changes.
func (c *modelViewports) content(key string, view *viewport.Model, text string, width, height int) {
	if *c == nil {
		*c = modelViewports{}
	}
	f := (*c)[key]
	if f == nil {
		f = &viewportFrame{}
		(*c)[key] = f
	}
	view.SetWidth(max(1, width))
	view.SetHeight(max(1, height))
	if f.generation == 0 || f.content != text {
		view.SetContent(text)
		f.content, f.generation = text, f.generation+1
	}
}

// View wraps and styles the visible window. Keep that result by content
// generation, width, height and offset; the Block's cursor remains live.
func (c modelViewports) text(key string, view *viewport.Model) string {
	f := c[key]
	w, h, y := view.Width(), view.Height(), view.YOffset()
	if !f.valid || f.drawnGeneration != f.generation || f.width != w || f.height != h || f.offset != y {
		f.text = view.View()
		f.width, f.height, f.offset, f.drawnGeneration, f.valid = w, h, y, f.generation, true
	}
	return f.text
}
