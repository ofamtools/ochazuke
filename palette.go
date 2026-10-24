package ochazuke

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
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
	nouns         map[string]PaletteNoun
}

func NewPalette(r *Runtime) *Palette {
	p := &Palette{r: r, actions: map[string]bool{}, nouns: map[string]PaletteNoun{
		"page": {Source: PagesPaletteSource, DefaultVerb: "open", Argument: "id"},
	}}
	p.editors = map[string]*Input{}
	p.explicitEmpty = map[string]bool{}
	r.Instance("palette", Kind("palette"))
	register := func(verb, noun, description string, params map[string]Parameter, fn func(Arguments) error) {
		p.actions[verb+"."+noun] = true
		r.Register(Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "palette state", Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
			err := fn(args)
			if err != nil {
				p.message = err.Error()
			}
			return p.state(), err
		}})
	}
	register("open", "palette", "Open the nested verb/noun palette", nil, func(Arguments) error { return p.open() })
	register("close", "palette", "Cancel the palette without executing", nil, func(Arguments) error { return p.close() })
	register("move", "palette", "Move the palette selection or argument focus; column switches between the root verb and noun lists", map[string]Parameter{"delta": {Type: "integer"}, "column": {Type: "string", Description: "left or right: jump between the root columns"}}, func(a Arguments) error {
		if column := StringArg(a, "column", ""); column != "" {
			return p.moveColumn(column)
		}
		return p.move(IntArg(a, "delta", 0))
	})
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
		p.r.setFocus(p.frame, editor.ID)
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
	r.DefineGuard("palette-list", func(*Runtime, KeyEvent) bool { return p.stage != "arguments" })
	// The palette body: rows guarded by self so a field beneath keeps its own.
	body := r.Define("palette").Mode("")
	for key, delta := range map[string]int{"j": 1, "down": 1, "k": -1, "up": -1, "tab": 1, "shift+tab": -1} {
		body.Bind(key, Do("move.palette", Arguments{"delta": delta})).When("self")
	}
	for _, key := range []string{"h", "left"} {
		body.Bind(key, Do("move.palette", Arguments{"column": "left"})).When("self")
	}
	for _, key := range []string{"l", "right"} {
		body.Bind(key, Do("move.palette", Arguments{"column": "right"})).When("self")
	}
	body.Bind("enter", Do("select.palette")).When("self").
		Bind("ctrl+enter", Do("execute.palette")).When("self").
		Bind("esc", Do("back.palette")).When("self").
		Bind("ctrl+c", Do("close.palette")).
		Bind("backspace", Do("delete.palette")).When("self").
		Bind("ctrl+h", Do("delete.palette")).When("self").
		Bind("/", Do("search.palette")).When("palette-list").
		Pattern(Any, Do("ignore.key")).When("self")
	// A palette field: the shared editor beneath, palette navigation on top,
	// and its own text rows so an application's any-mode row cannot steal typing.
	field := r.Define("palette-field")
	for _, mode := range []string{"insert", "normal"} {
		m := field.Mode(mode)
		for key, delta := range map[string]int{"up": -1, "down": 1, "tab": 1, "shift+tab": -1} {
			m.Bind(key, Do("move.palette", Arguments{"delta": delta}))
		}
		m.Bind("enter", Do("select.palette")).
			Bind("ctrl+enter", Do("execute.palette")).
			Bind("ctrl+c", Do("close.palette")).
			Bind("backspace", Do("delete.palette")).
			Bind("ctrl+h", Do("delete.palette"))
	}
	field.Mode("insert").
		Pattern(Printable, Do("insert.palette").FromPayload("text", SourceText)).
		Pattern(Paste, Do("insert.palette").FromPayload("text", SourceText))
	field.Mode("normal").
		Bind("j", Do("move.palette", Arguments{"delta": 1})).
		Bind("k", Do("move.palette", Arguments{"delta": -1})).
		Bind("h", Do("move.palette", Arguments{"column": "left"})).
		Bind("l", Do("move.palette", Arguments{"column": "right"})).
		Bind("left", Do("move.palette", Arguments{"column": "left"})).
		Bind("right", Do("move.palette", Arguments{"column": "right"})).
		Bind("esc", Do("back.palette"))
	return p
}

// moveColumn jumps between the root verb and noun columns; it is a no-op on
// other stages, where left and right have no meaning.
func (p *Palette) moveColumn(direction string) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.stage != "root" {
		return nil
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
	if direction == "left" && split > 0 {
		target = min(row, split-1)
	}
	if direction == "right" && len(options) > split {
		target = split + min(row, len(options)-split-1)
	}
	return p.move(target - p.selection)
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
	frame := &Frame{ID: "palette", Focus: "palette"}
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
		p.r.removeInstance(editor.ID)
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
	if p.r.top() != p.frame {
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
	if p.r.top() != p.frame {
		return errors.New("palette is covered by another modal")
	}
	return nil
}

func (p *Palette) options() []string {
	entries := p.entries()
	options := make([]string, len(entries))
	for i, entry := range entries {
		options[i] = entry.Value
	}
	return options
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
	entries := p.entries()
	if value, ok := a["value"].(string); ok {
		found := false
		for i, entry := range entries {
			if entry.Value == value {
				p.selection = i
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown palette choice %q", value)
		}
	}
	if len(entries) == 0 {
		return errors.New("no matching palette entries")
	}
	if p.selection >= len(entries) {
		p.selection = len(entries) - 1
	}
	entry := entries[p.selection]
	value := entry.Value
	switch p.stage {
	case "root":
		kind, name, _ := strings.Cut(value, ":")
		p.section = kind + "s"
		p.first = name
		p.stage = "second"
	case "second":
		if entry.Kind == "instance" {
			config := p.nouns[p.first]
			return p.dispatch(Request{Verb: config.DefaultVerb, Noun: p.first, Args: Arguments{config.Argument: entry.ID}})
		}
		q := Request{Verb: p.first, Noun: value}
		if p.section == "nouns" {
			q.Verb, q.Noun = entry.Verb, p.first
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
	if p.searching {
		editor := p.editor("search")
		if err := editor.Replace(0, len(editor.Text), ""); err != nil {
			return err
		}
	}
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
		p.r.setFocus(p.frame, "palette")
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
	p.r.setFocus(p.frame, "palette")
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
	if _, err := p.r.Execute(Request{Verb: "insert", Noun: "input", Target: editor.ID, Args: Arguments{"text": text}}); err != nil {
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
	return p.dispatch(Request{Verb: p.op.Verb, Noun: p.op.Noun, Args: args})
}

func (p *Palette) dispatch(q Request) error {
	if p.executing {
		return errors.New("recursive palette execution is not allowed")
	}
	if err := p.requireOpen(); err != nil {
		return err
	}
	origin := p.origin.Clone()
	p.executing = true
	defer func() { p.executing = false }()
	// Palette controls act on the live overlay; the recursion guard also covers
	// selecting select.palette or execute.palette from the palette itself.
	if p.actions[q.Key()] {
		_, err := p.r.ExecuteAt(origin, q)
		return err
	}
	frame := p.frame
	if err := p.close(); err != nil {
		return err
	}
	_, err := p.r.ExecuteAt(origin, q)
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

// HandleKey dispatches one key while the palette is the active modal. The
// palette and palette-field nouns answer first; the shared editor answers for
// everything else.
func (p *Palette) HandleKey(key string) (bool, error) {
	if !p.IsActive() {
		return false, nil
	}
	// Reassert the form's own field even if an external caller moved overlay focus.
	p.focusEditor()
	_, err := p.r.Dispatch(MacroKey{Key: key})
	p.collectEditors()
	if err != nil {
		p.message = err.Error()
	}
	return true, err
}

func (p *Palette) state() map[string]any {
	p.collectEditors()
	drafts := map[string]string{}
	for name, value := range p.drafts {
		drafts[name] = value
	}
	return map[string]any{"open": p.IsOpen(), "origin": p.origin.Clone(), "stage": p.stage, "section": p.section, "first": p.first, "search": p.query, "searching": p.searching, "selection": p.selection, "entries": p.entries(), "operation": p.op.Key(), "arguments": drafts, "field": p.field, "error": p.message}
}

func (p *Palette) View() string { return p.ViewSized(80, 20) }

func (p *Palette) ViewSized(width, height int) string { return p.viewBlock(width, height).Text }

// viewBlock renders the palette and reports the cursor of the editor it shows:
// the active argument field or the search line.
func (p *Palette) viewBlock(width, height int) Block {
	if !p.IsOpen() {
		return Block{}
	}
	p.collectEditors()
	var out strings.Builder
	var cursor *Pos
	// place records the editor's cursor at the current row after prefix.
	place := func(prefix string, editor Block) string {
		if editor.Cursor != nil {
			cursor = &Pos{X: ansi.StringWidth(prefix) + editor.Cursor.X, Y: strings.Count(out.String(), "\n") + editor.Cursor.Y}
		}
		return prefix + editor.Text
	}
	out.WriteString("Command palette\n")
	if p.stage == "arguments" {
		fmt.Fprintf(&out, "%s (%s) — %s\n", p.op.Key(), p.op.Kind, p.op.Description)
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
			line := fmt.Sprintf("%s%s (%s, %s): ", marker, name, param.Type, kind)
			if i == p.field {
				line = place(line, InputBlock(p.editor("argument/"+name), max(8, width/2), 1))
			} else {
				line += value
			}
			out.WriteString(line)
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
			out.WriteString(place(fmt.Sprintf("Search [%s]: ", p.editor("search").Mode), InputBlock(p.editor("search"), max(8, width-20), 1)) + "\n")
		}
		entries := p.entries()
		if len(entries) == 0 {
			out.WriteString("  No matches\n")
		}
		limit := max(1, height-6)
		if p.stage == "root" {
			split := 0
			for split < len(entries) && entries[split].Kind == "verb" {
				split++
			}
			row := p.selection
			if row >= split {
				row -= split
			}
			start := max(0, row-limit+1)
			column := max(8, (width-3)/2)
			fmt.Fprintf(&out, "%-*s  %s\n", column, "VERBS", "NOUNS")
			for i := start; i < min(max(split, len(entries)-split), start+limit); i++ {
				left, right := "", ""
				if i < split {
					mark := "  "
					if p.selection == i {
						mark = "> "
					}
					left = mark + entries[i].Label
				}
				if split+i < len(entries) {
					mark := "  "
					if p.selection == split+i {
						mark = "> "
					}
					right = mark + entries[split+i].Label
				}
				fmt.Fprintf(&out, "%-*s  %s\n", column, left, right)
			}
		} else {
			start := max(0, p.selection-limit+1)
			for i := start; i < min(len(entries), start+limit); i++ {
				marker := "  "
				if i == p.selection {
					marker = "> "
				}
				entry := entries[i]
				label := entry.Label
				if entry.Kind == "instance" {
					label = fmt.Sprintf("%s [%s] → %s.%s", label, entry.ID, p.nouns[p.first].DefaultVerb, p.first)
				}
				fmt.Fprintf(&out, "%s%s\n", marker, label)
			}
		}
		out.WriteString("j/k: move · ←/→: column · Enter: choose · /: search · Esc: normal/back\n")
	}
	if p.message != "" {
		fmt.Fprintf(&out, "Error: %s\n", p.message)
	}
	return Block{Text: out.String(), Cursor: cursor}
}
