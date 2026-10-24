package ochazuke

func (p *Palette) editorKey(name string) string {
	if name == "search" {
		return name
	}
	return p.op.Key() + "/" + name
}
func (p *Palette) editor(name string) *Input {
	key := p.editorKey(name)
	if in := p.editors[key]; in != nil {
		return in
	}
	in := NewInput("__palette/"+key, "", false)
	in.SetMode("insert")
	if err := p.r.AddInput(in, Parent("palette"), Kind("palette-field")); err != nil {
		panic(err)
	}
	p.editors[key] = in
	return in
}
func (p *Palette) activeEditor() *Input {
	if p.searching {
		return p.editor("search")
	}
	if p.stage == "arguments" && p.field < len(p.fields) {
		return p.editor("argument/" + p.fields[p.field])
	}
	return nil
}
func (p *Palette) focusEditor() {
	if p.frame == nil {
		return
	}
	focus := "palette"
	in := p.activeEditor()
	if in != nil {
		focus = in.ID
	}
	moved := p.r.FocusID() != focus
	cursor := 0
	if in != nil {
		cursor = in.Cursor
	}
	p.r.setFocus(p.frame, focus)
	// A field the palette moves to types again where its cursor was.
	if in != nil && moved {
		in.SetMode("insert")
		in.Cursor = cursor
	}
}
func (p *Palette) collectEditors() {
	if p.searching {
		if in := p.editors["search"]; in != nil {
			query := string(in.Text)
			if query != p.query {
				p.query = query
				p.selection = 0
			}
		}
	}
	if p.stage != "arguments" {
		return
	}
	for _, name := range p.fields {
		if in := p.editors[p.editorKey("argument/"+name)]; in != nil {
			text := string(in.Text)
			if text != "" || p.explicitEmpty[name] {
				p.drafts[name] = text
			} else {
				delete(p.drafts, name)
			}
		}
	}
}

// IsActive reports whether the palette is the top modal.
func (p *Palette) IsActive() bool { return p.IsOpen() && p.r.top() == p.frame }
