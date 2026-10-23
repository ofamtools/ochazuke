package ochazuke

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Palette owns its overlay and drafts; it never edits the originating input.
// Create one palette per runtime: operation keys are globally unique.
type Palette struct {
	r             *Runtime
	frame         *Frame
	origin        Context
	stage         string
	section       string
	first         string
	query         string
	searching     bool
	selection     int
	op            Operation
	fields        []string
	drafts        map[string]string
	field         int
	message       string
	executing     bool
	actions       map[string]bool
	editors       map[string]*Input
	explicitEmpty map[string]bool
}

func NewPalette(r *Runtime) *Palette {
	p := &Palette{r: r, actions: map[string]bool{}}
	p.editors = map[string]*Input{}
	p.explicitEmpty = map[string]bool{}
	r.Layers["palette"] = &Layer{ID: "palette", Noun: "palette", Actions: map[string]Handler{}, Bindings: []Binding{
		{Keys: "ctrl+c", Request: Request{Verb: "close", Noun: "palette"}},
		{Keys: "tab", Request: Request{Verb: "move", Noun: "palette", Args: Arguments{"delta": 1}}},
		{Keys: "shift+tab", Request: Request{Verb: "move", Noun: "palette", Args: Arguments{"delta": -1}}},
	}}
	register := func(verb, noun, description string, params map[string]Parameter, fn func(Arguments) error) {
		p.actions[verb+"."+noun] = true
		r.Register(Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "palette state", Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
			err := fn(args)
			if err != nil {
				p.message = err.Error()
			}
			p.publish()
			return p.state(), err
		}})
	}
	register("open", "palette", "Open the nested verb/noun palette", nil, func(Arguments) error { return p.open() })
	register("close", "palette", "Cancel the palette without executing", nil, func(Arguments) error { return p.close() })
	register("move", "palette", "Move the palette selection or argument focus", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(a Arguments) error { return p.move(IntArg(a, "delta", 0)) })
	register("select", "palette", "Choose the highlighted entry or advance the argument form", map[string]Parameter{"value": {Type: "string"}}, p.choose)
	register("back", "palette", "Leave search, go back one level, or cancel at the root", nil, func(Arguments) error { return p.back() })
	register("search", "palette", "Enter search in the current palette list", map[string]Parameter{"query": {Type: "string"}}, func(a Arguments) error {
		if err := p.requireOpen(); err != nil {
			return err
		}
		if p.stage == "arguments" {
			return errors.New("argument forms cannot be searched")
		}
		p.searching = true
		p.query = StringArg(a, "query", "")
		p.selection = 0
		p.message = ""
		editor := p.editor("search")
		if err := editor.Replace(0, len(editor.Text), p.query); err != nil {
			return err
		}
		editor.SetMode("insert")
		p.frame.Context.InputID = editor.ID
		return nil
	})
	register("insert", "palette", "Append text to palette search or the focused argument", map[string]Parameter{"text": {Type: "string", Required: true}}, func(a Arguments) error { return p.insert(StringArg(a, "text", "")) })
	register("delete", "palette", "Delete the last search or argument character, or go back", nil, func(Arguments) error { return p.erase() })
	register("set", "palette-argument", "Set a selected operation argument using its text representation", map[string]Parameter{"name": {Type: "string", Required: true}, "value": {Type: "string", Required: true}}, func(a Arguments) error { return p.setArgument(StringArg(a, "name", ""), StringArg(a, "value", "")) })
	register("clear", "palette-argument", "Omit an argument from the selected operation", map[string]Parameter{"name": {Type: "string", Required: true}}, func(a Arguments) error {
		name := StringArg(a, "name", "")
		if err := p.argumentField(name); err != nil {
			return err
		}
		editor := p.editor("argument/" + name)
		if err := editor.Replace(0, len(editor.Text), ""); err != nil {
			return err
		}
		delete(p.explicitEmpty, name)
		delete(p.drafts, name)
		p.message = ""
		return nil
	})
	register("execute", "palette", "Execute the selected operation in the context captured on open", nil, func(Arguments) error { return p.execute() })
	return p
}

func (p *Palette) Open() error {
	_, err := p.r.Execute(Request{Verb: "open", Noun: "palette"})
	return err
}

func (p *Palette) Close() {
	_, _ = p.r.Execute(Request{Verb: "close", Noun: "palette"})
}

func (p *Palette) IsOpen() bool {
	if p.frame == nil {
		return false
	}
	for _, f := range p.r.Frames {
		if f == p.frame {
			return true
		}
	}
	return false
}

func (p *Palette) open() error {
	if p.IsOpen() {
		return nil
	}
	origin := p.r.Context().Clone()
	frame := &Frame{ID: "palette", Context: Context{Path: []string{"app", "palette"}}, State: map[string]any{}}
	if err := p.r.Push(frame); err != nil {
		return err
	}
	p.origin = origin
	p.frame = frame
	p.stage = "root"
	p.section = ""
	p.first = ""
	p.query = ""
	p.searching = true
	p.selection = 0
	p.op = Operation{}
	p.fields = nil
	p.drafts = map[string]string{}
	p.field = 0
	p.message = ""
	for _, editor := range p.editors {
		delete(p.r.Inputs, editor.ID)
		delete(p.r.InputPaths, editor.ID)
	}
	p.editors = map[string]*Input{}
	p.explicitEmpty = map[string]bool{}
	p.focusEditor()
	return nil
}

func (p *Palette) close() error {
	if !p.IsOpen() {
		return nil
	}
	if p.r.Frames[len(p.r.Frames)-1] != p.frame {
		return errors.New("palette is covered by another modal")
	}
	if err := p.r.Pop(); err != nil {
		return err
	}
	p.frame = nil
	return nil
}

func (p *Palette) requireOpen() error {
	if !p.IsOpen() {
		return errors.New("palette is not open")
	}
	if p.r.Frames[len(p.r.Frames)-1] != p.frame {
		return errors.New("palette is covered by another modal")
	}
	return nil
}

func (p *Palette) options() []string {
	var options []string
	seen := map[string]bool{}
	for _, op := range p.r.Catalog() {
		values := []string{}
		if p.stage == "root" {
			values = []string{"verb:" + op.Verb, "noun:" + op.Noun}
		} else if p.section == "verbs" && op.Verb == p.first {
			values = []string{op.Noun}
		} else if p.section == "nouns" && op.Noun == p.first {
			values = []string{op.Verb}
		}
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				options = append(options, value)
			}
		}
	}
	sort.Slice(options, func(i, j int) bool {
		if p.stage == "root" && strings.HasPrefix(options[i], "verb:") != strings.HasPrefix(options[j], "verb:") {
			return strings.HasPrefix(options[i], "verb:")
		}
		return options[i] < options[j]
	})
	if p.query == "" {
		return options
	}
	filtered := make([]string, 0, len(options))
	for _, option := range options {
		if strings.Contains(strings.ToLower(option), strings.ToLower(p.query)) {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func (p *Palette) move(delta int) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	count := len(p.options())
	index := &p.selection
	if p.stage == "arguments" {
		count = len(p.fields) + 1
		index = &p.field
	}
	if count > 0 {
		*index = ((*index+delta)%count + count) % count
	}
	p.focusEditor()
	p.message = ""
	return nil
}

func (p *Palette) choose(a Arguments) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.stage == "arguments" {
		if _, ok := a["value"]; ok {
			return errors.New("select a field with move.palette; set it with set.palette-argument")
		}
		if p.field == len(p.fields) {
			return p.execute()
		}
		p.field++
		p.focusEditor()
		p.message = ""
		return nil
	}
	options := p.options()
	if value, ok := a["value"].(string); ok {
		found := false
		for i, option := range options {
			if option == value {
				p.selection = i
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown palette choice %q", value)
		}
	}
	if len(options) == 0 {
		return errors.New("no matching palette entries")
	}
	if p.selection >= len(options) {
		p.selection = len(options) - 1
	}
	value := options[p.selection]
	switch p.stage {
	case "root":
		kind, name, _ := strings.Cut(value, ":")
		p.section = kind + "s"
		p.first = name
		p.stage = "second"
	case "second":
		q := Request{Verb: p.first, Noun: value}
		if p.section == "nouns" {
			q.Verb, q.Noun = value, p.first
		}
		op, ok := p.r.Operations[q.Key()]
		if !ok {
			return errors.New("selected operation is no longer registered")
		}
		p.op = op
		p.fields = nil
		p.drafts = map[string]string{}
		p.field = 0
		for name := range op.Parameters {
			p.fields = append(p.fields, name)
		}
		sort.Strings(p.fields)
		p.stage = "arguments"
		p.query = ""
		p.searching = false
		p.selection = 0
		p.message = ""
		if len(p.fields) == 0 {
			return p.execute()
		}
		p.focusEditor()
	}
	p.query = ""
	p.searching = false
	p.selection = 0
	p.message = ""
	p.focusEditor()
	return nil
}

func (p *Palette) back() error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.searching {
		p.searching = false
		p.query = ""
		p.selection = 0
		p.frame.Context.InputID = ""
		p.message = ""
		return nil
	}
	switch p.stage {
	case "root":
		return p.close()
	case "second":
		p.stage = "root"
		p.first = ""
		p.section = ""
	case "arguments":
		p.stage = "second"
		p.op = Operation{}
		p.fields = nil
		p.drafts = map[string]string{}
		p.field = 0
	}
	p.frame.Context.InputID = ""
	p.query = ""
	p.selection = 0
	p.message = ""
	return nil
}

func (p *Palette) argumentField(name string) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.stage != "arguments" {
		return errors.New("no operation argument form is selected")
	}
	if _, ok := p.op.Parameters[name]; !ok {
		return fmt.Errorf("unknown argument %q", name)
	}
	return nil
}

func (p *Palette) setArgument(name, value string) error {
	if err := p.argumentField(name); err != nil {
		return err
	}
	editor := p.editor("argument/" + name)
	if err := editor.Replace(0, len(editor.Text), value); err != nil {
		return err
	}
	p.explicitEmpty[name] = value == ""
	p.drafts[name] = value
	p.message = ""
	return nil
}

func (p *Palette) insert(text string) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if strings.ContainsAny(text, "\r\n") {
		return errors.New("palette fields accept single-line text")
	}
	editor := p.activeEditor()
	if editor == nil {
		return errors.New("select a search or argument field before typing")
	}
	if _, err := p.r.ExecuteAt(Context{Path: []string{"app", "palette"}, InputID: editor.ID}, Request{Verb: "insert", Noun: "input", Args: Arguments{"text": text}}); err != nil {
		return err
	}
	p.collectEditors()
	p.message = ""
	return nil
}

func (p *Palette) erase() error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	editor := p.activeEditor()
	if editor == nil {
		if p.stage == "arguments" {
			return p.move(-1)
		}
		return p.back()
	}
	if editor.Cursor > 0 {
		if err := editor.Replace(editor.Cursor-1, editor.Cursor, ""); err != nil {
			return err
		}
	}
	p.collectEditors()
	p.message = ""
	return nil
}

func (p *Palette) arguments() (Arguments, error) {
	p.collectEditors()
	args := Arguments{}
	for _, name := range p.fields {
		param := p.op.Parameters[name]
		text, present := p.drafts[name]
		if !present {
			if param.Required {
				return nil, fmt.Errorf("%s is required", name)
			}
			continue
		}
		switch param.Type {
		case "string":
			args[name] = text
		case "integer":
			n, err := strconv.Atoi(text)
			if err != nil {
				return nil, fmt.Errorf("%s must be integer", name)
			}
			args[name] = n
		case "boolean":
			if text != "true" && text != "false" {
				return nil, fmt.Errorf("%s must be boolean (true or false)", name)
			}
			args[name] = text == "true"
		default:
			return nil, fmt.Errorf("unsupported argument type %q for %s", param.Type, name)
		}
	}
	return args, nil
}

func (p *Palette) execute() error {
	if p.executing {
		return errors.New("recursive palette execution is not allowed")
	}
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.stage != "arguments" || p.op.Verb == "" {
		return errors.New("select an operation before executing")
	}
	args, err := p.arguments()
	if err != nil {
		return err
	}
	q := Request{Verb: p.op.Verb, Noun: p.op.Noun, Args: args}
	origin := p.origin.Clone()
	p.executing = true
	defer func() { p.executing = false }()
	// Palette controls act on the live overlay; the recursion guard also covers
	// selecting select.palette or execute.palette from the palette itself.
	if p.actions[q.Key()] {
		_, err = p.r.ExecuteAt(origin, q)
		return err
	}
	frame := p.frame
	if err := p.close(); err != nil {
		return err
	}
	_, err = p.r.ExecuteAt(origin, q)
	if err != nil {
		// Dismiss before dispatch so focus/push/pop act on the originating frame.
		// A failure restores this same overlay, including its argument drafts.
		if restoreErr := p.r.Push(frame); restoreErr != nil {
			return fmt.Errorf("%w (restoring palette: %v)", err, restoreErr)
		}
		p.frame = frame
	}
	return err
}

// HandleKey applies layered overrides before palette navigation or the shared input editor.
func (p *Palette) HandleKey(key string) (bool, error) {
	if !p.IsOpen() || p.r.Frames[len(p.r.Frames)-1] != p.frame {
		return false, nil
	}
	if handled, err := p.r.HandleBinding(key); handled || err != nil {
		p.collectEditors()
		p.publish()
		return handled, err
	}
	if p.stage != "arguments" && p.r.Mode(p.frame.Context) == "normal" {
		switch key {
		case "h":
			key = "left"
		case "j":
			key = "down"
		case "k":
			key = "up"
		case "l":
			key = "right"
		}
	}
	if handled, err := p.handleEditorKey(key); handled || err != nil {
		return handled, err
	}
	q := Request{Noun: "palette"}
	switch key {
	case "left", "right":
		if p.stage != "root" {
			return true, nil
		}
		options := p.options()
		split := 0
		for split < len(options) && strings.HasPrefix(options[split], "verb:") {
			split++
		}
		row := p.selection
		if row >= split {
			row -= split
		}
		target := p.selection
		if key == "left" && split > 0 {
			target = min(row, split-1)
		}
		if key == "right" && len(options) > split {
			target = split + min(row, len(options)-split-1)
		}
		q.Verb = "move"
		q.Args = Arguments{"delta": target - p.selection}
	case "esc", "escape":
		q.Verb = "back"
	case "ctrl+c":
		q.Verb = "close"
	case "up", "shift+tab":
		q.Verb = "move"
		q.Args = Arguments{"delta": -1}
	case "down":
		q.Verb = "move"
		q.Args = Arguments{"delta": 1}
	case "enter":
		q.Verb = "select"
	case "tab":
		q.Verb = "move"
		q.Args = Arguments{"delta": 1}
	case "ctrl+enter":
		q.Verb = "execute"
	case "backspace", "ctrl+h":
		q.Verb = "delete"
	case "ctrl+u":
		return true, nil
	default:
		if p.stage != "arguments" && !p.searching {
			switch key {
			case "j":
				q.Verb = "move"
				q.Args = Arguments{"delta": 1}
			case "k":
				q.Verb = "move"
				q.Args = Arguments{"delta": -1}
			case "/":
				q.Verb = "search"
			default:
				return true, nil
			}
		} else {
			return true, nil
		}
	}
	_, err := p.r.Execute(q)
	if err != nil {
		p.message = err.Error()
		p.publish()
	}
	return true, err
}

func (p *Palette) state() map[string]any {
	p.collectEditors()
	drafts := map[string]string{}
	for name, value := range p.drafts {
		drafts[name] = value
	}
	return map[string]any{"open": p.IsOpen(), "origin": p.origin.Clone(), "stage": p.stage, "section": p.section, "first": p.first, "search": p.query, "searching": p.searching, "selection": p.selection, "operation": p.op.Key(), "arguments": drafts, "field": p.field, "error": p.message}
}

func (p *Palette) publish() {
	if p.frame != nil {
		p.frame.State = p.state()
	}
}

func (p *Palette) View() string { return p.ViewSized(80, 20) }

func (p *Palette) ViewSized(width, height int) string {
	if !p.IsOpen() {
		return ""
	}
	p.collectEditors()
	var out strings.Builder
	out.WriteString("Command palette\n")
	if p.stage == "arguments" {
		fmt.Fprintf(&out, "%s — %s\n", p.op.Key(), p.op.Description)
		for i, name := range p.fields {
			param := p.op.Parameters[name]
			marker := "  "
			if i == p.field {
				marker = "> "
			}
			kind := "optional"
			if param.Required {
				kind = "required"
			}
			value, present := p.drafts[name]
			if !present {
				value = "<omitted>"
			} else {
				value = strconv.Quote(value)
			}
			if i == p.field {
				value = InputView(p.editor("argument/"+name), max(8, width/2), 1)
			}
			fmt.Fprintf(&out, "%s%s (%s, %s): %s", marker, name, param.Type, kind, value)
			if param.Description != "" {
				fmt.Fprintf(&out, " — %s", param.Description)
			}
			out.WriteByte('\n')
		}
		marker := "  "
		if p.field == len(p.fields) {
			marker = "> "
		}
		fmt.Fprintf(&out, "%sExecute\nTab/arrows: field · Enter: next/execute · Esc: normal/back · Ctrl-C: cancel\n", marker)
	} else {
		if p.stage != "root" {
			fmt.Fprintf(&out, "%s", p.section)
			if p.first != "" {
				fmt.Fprintf(&out, " / %s", p.first)
			}
			out.WriteByte('\n')
		}
		if p.searching {
			fmt.Fprintf(&out, "Search [%s]: %s\n", p.editor("search").Mode, InputView(p.editor("search"), max(8, width-20), 1))
		}
		options := p.options()
		if len(options) == 0 {
			out.WriteString("  No matches\n")
		}
		limit := max(1, height-6)
		if p.stage == "root" {
			split := 0
			for split < len(options) && strings.HasPrefix(options[split], "verb:") {
				split++
			}
			row := p.selection
			if row >= split {
				row -= split
			}
			start := max(0, row-limit+1)
			column := max(8, (width-3)/2)
			fmt.Fprintf(&out, "%-*s  %s\n", column, "VERBS", "NOUNS")
			for i := start; i < min(max(split, len(options)-split), start+limit); i++ {
				left, right := "", ""
				if i < split {
					mark := "  "
					if p.selection == i {
						mark = "> "
					}
					left = mark + strings.TrimPrefix(options[i], "verb:")
				}
				if split+i < len(options) {
					mark := "  "
					if p.selection == split+i {
						mark = "> "
					}
					right = mark + strings.TrimPrefix(options[split+i], "noun:")
				}
				fmt.Fprintf(&out, "%-*s  %s\n", column, left, right)
			}
		} else {
			start := max(0, p.selection-limit+1)
			for i := start; i < min(len(options), start+limit); i++ {
				marker := "  "
				if i == p.selection {
					marker = "> "
				}
				fmt.Fprintf(&out, "%s%s\n", marker, options[i])
			}
		}
		out.WriteString("j/k: move · ←/→: column · Enter: choose · /: search · Esc: normal/back\n")
	}
	if p.message != "" {
		fmt.Fprintf(&out, "Error: %s\n", p.message)
	}
	return out.String()
}
