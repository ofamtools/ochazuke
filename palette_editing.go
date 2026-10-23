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
	if err := p.r.AddInput(in, []string{"app", "palette"}); err != nil {
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
	p.frame.Context = Context{Path: []string{"app", "palette"}}
	if in := p.activeEditor(); in != nil {
		p.frame.Context.InputID = in.ID
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
func (p *Palette) handleEditorKey(key string) (bool, error) {
	in := p.activeEditor()
	if in == nil {
		return false, nil
	}
	switch key {
	case "enter", "tab", "shift+tab", "up", "down", "ctrl+enter", "ctrl+c":
		return false, nil
	case "esc", "escape":
		if in.Mode == "normal" {
			return false, nil
		}
	}
	// Reassert the form's own input even if an external caller changed overlay focus.
	p.focusEditor()
	handled, err := HandleInputKey(p.r, key)
	p.collectEditors()
	if err != nil {
		p.message = err.Error()
	} else {
		p.message = ""
	}
	p.publish()
	// Unknown keys in a field stay in its mode; never fall through to literal insertion.
	_ = handled
	return true, err
}
func (p *Palette) IsActive() bool { return p.IsOpen() && p.r.Frames[len(p.r.Frames)-1] == p.frame }
