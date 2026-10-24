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
	if in := p.activeEditor(); in != nil {
		focus = in.ID
	}
	p.r.setFocus(p.frame, focus)
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
