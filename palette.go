package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Palette is the ex-mode command line: one prompt input whose text is the
// source of truth, parsed by the grammar in palette_parse.go, with a
// completion list for the next word beneath it. It never edits the
// originating input. Create one palette per runtime: operation keys are
// globally unique.
type Palette struct {
	r          *Runtime
	origin     Context
	originMode ModeState
	line       *Input
	// highlight is the completion ctrl+n/ctrl+p moved onto (-1: none); it
	// holds only while the line still reads marked.
	highlight int
	marked    string
	// filter restricts the list to verbs, nouns or choices ("v", "n", "p")
	// for the word at filterAt; typing :v, :n or :p sets it.
	filter   string
	filterAt int
	seen     string // the line at the last sync
	recall   int    // palette history steps back; 0 is the draft
	draft    string
	message  string
	// executing guards against the line running palette.run itself.
	executing   bool
	actions     map[string]bool
	nouns       map[string]PaletteNoun
	choiceCache map[string]choiceList
}

// paletteID is the palette's modal instance; paletteLineID its line.
const (
	paletteID     = "palette"
	paletteLineID = "__palette/line"
)

// NewPalette returns the command palette for r.
func NewPalette(r *Runtime) *Palette {
	var p *Palette
	r.Builtin(func() { p = newPalette(r) })
	return p
}

func newPalette(r *Runtime) *Palette {
	p := &Palette{r: r, highlight: -1, actions: map[string]bool{}, nouns: map[string]PaletteNoun{
		"ochazuke.page": {Source: PagesPaletteSource, DefaultVerb: "open", Argument: "id"},
	}}
	r.Instance(paletteID, Kind("ochazuke.palette"))
	register := func(verb, description string, params map[string]Parameter, fn func(Arguments) error) {
		p.actions["ochazuke.palette."+verb] = true
		r.Register(Operation{Verb: verb, Noun: "ochazuke.palette", Description: description, Parameters: params, Result: "palette state", Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
			if verb != "open" {
				p.sync()
			}
			err := fn(args)
			if err != nil {
				p.message = err.Error()
			}
			return p.state(), err
		}})
	}
	register("open", "Open the command line", nil, func(Arguments) error { return p.open() })
	register("close", "Close the command line without running it", nil, func(Arguments) error { return p.close() })
	register("set", "Replace the command line's text", map[string]Parameter{"text": {Type: "string", Required: true}}, func(a Arguments) error {
		if err := p.requireOpen(); err != nil {
			return err
		}
		return p.setText(StringArg(a, "text", ""))
	})
	register("move", "Move the completion highlight; from none, 1 lands on the first entry and -1 on the last", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(a Arguments) error { return p.move(IntArg(a, "delta", 0)) })
	register("complete", "Append the highlighted completion (or value, or the only one) as the next word", map[string]Parameter{"value": {Type: "string", Description: "A completion's word; omit for the highlighted one"}}, p.complete)
	register("run", "Run the line or append the highlighted completion; a short line names its first missing word", nil, func(Arguments) error { return p.run() })
	register("cancel", "Clear the command line, or close it when it is empty", nil, func(Arguments) error { return p.cancel() })
	register("recall", "Replace the line with an earlier palette command from the action history: 1 older, -1 newer", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(a Arguments) error { return p.recallLine(IntArg(a, "delta", 0)) })
	// The palette's keys, all on its own noun: the line beneath is an
	// ordinary prompt, so the editor's Readline rows answer the rest.
	// Printable text has its own row so an application's any-mode row
	// cannot steal typing.
	r.Define("ochazuke.palette").Focusable().Mode("").
		Bind("enter", Do("ochazuke.palette.run")).
		Bind("tab", Do("ochazuke.palette.complete")).
		Bind("ctrl+n", Do("ochazuke.palette.move", Arguments{"delta": 1})).
		Bind("ctrl+p", Do("ochazuke.palette.move", Arguments{"delta": -1})).
		Bind("up", Do("ochazuke.palette.recall", Arguments{"delta": 1})).
		Bind("down", Do("ochazuke.palette.recall", Arguments{"delta": -1})).
		Bind("esc", Do("ochazuke.palette.cancel")).
		Bind("ctrl+c", Do("ochazuke.palette.close")).
		Pattern(Printable, Do("ochazuke.input.insert").FromPayload("text", SourceText)).
		Pattern(Paste, Do("ochazuke.input.insert").FromPayload("text", SourceText))
	return p
}

func (p *Palette) Open() error {
	_, err := p.r.Execute(Request{Verb: "open", Noun: "ochazuke.palette"})
	return err
}

func (p *Palette) Close() {
	_, _ = p.r.Execute(Request{Verb: "close", Noun: "ochazuke.palette"})
}

func (p *Palette) IsOpen() bool { return slices.Contains(p.r.modals, paletteID) }

// IsActive reports whether the palette is the top modal.
func (p *Palette) IsActive() bool { return p.IsOpen() && p.r.TopModal() == paletteID }

// Text is the command line.
func (p *Palette) Text() string {
	if p.line == nil {
		return ""
	}
	return string(p.line.Text)
}

func (p *Palette) open() error {
	if p.IsOpen() {
		return nil
	}
	origin, mode := p.r.Context().Clone(), p.r.Mode
	if err := p.r.Push(paletteID, paletteID); err != nil {
		return err
	}
	p.origin, p.originMode = origin, mode
	if p.line == nil {
		p.line = NewInput(paletteLineID, "", false)
		if err := p.r.AddInput(p.line, Parent(paletteID), Kind("ochazuke.prompt")); err != nil {
			return err
		}
	}
	p.highlight, p.marked, p.filter, p.recall, p.draft, p.message = -1, "", "", 0, "", ""
	p.choiceCache = nil
	if err := p.line.Replace(0, len(p.line.Text), ""); err != nil {
		return err
	}
	p.seen = ""
	p.r.setModalFocus(paletteID, p.line.ID)
	return nil
}

func (p *Palette) close() error {
	if !p.IsOpen() {
		return nil
	}
	if p.r.TopModal() != paletteID {
		return errors.New("palette is covered by another modal")
	}
	return p.r.Pop()
}

func (p *Palette) requireOpen() error {
	if !p.IsOpen() {
		return errors.New("palette is not open")
	}
	if p.r.TopModal() != paletteID {
		return errors.New("palette is covered by another modal")
	}
	return nil
}

// highlighted is the highlighted completion, if the highlight still holds.
func (p *Palette) highlighted(entries []PaletteEntry) (PaletteEntry, bool) {
	if p.highlight < 0 || p.marked != p.Text() || p.highlight >= len(entries) {
		return PaletteEntry{}, false
	}
	return entries[p.highlight], true
}

func (p *Palette) move(delta int) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	entries := p.completions()
	if len(entries) == 0 {
		return errors.New("no completions for this word")
	}
	n := len(entries)
	switch _, ok := p.highlighted(entries); {
	case ok:
		p.highlight = ((p.highlight+delta)%n + n) % n
	case delta > 0:
		p.highlight = 0
	default:
		p.highlight = n - 1
	}
	p.marked = p.Text()
	p.message = ""
	return nil
}

func (p *Palette) complete(a Arguments) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	entries := p.completions()
	entry, ok := p.highlighted(entries)
	if value, given := a["value"].(string); given {
		ok = false
		for _, e := range entries {
			if e.Value == value {
				entry, ok = e, true
				break
			}
		}
		if !ok {
			return fmt.Errorf("unknown palette completion %q", value)
		}
	}
	if !ok && len(entries) == 1 {
		entry, ok = entries[0], true
	}
	if !ok {
		return errors.New("no completion highlighted (ctrl+n / ctrl+p)")
	}
	return p.appendWord(entry.Value)
}

// appendWord replaces the word being typed (or adds one after a space) and
// a space, so the list moves on to the next word.
func (p *Palette) appendWord(word string) error {
	text := p.Text()
	if _, trailing := tokenize(text); !trailing {
		i := strings.LastIndexAny(text, " \t")
		text = text[:i+1]
	}
	return p.setText(text + quoteWord(word) + " ")
}

// setText replaces the line, as typing would.
func (p *Palette) setText(text string) error {
	if err := p.line.Replace(0, len(p.line.Text), text); err != nil {
		return err
	}
	p.sync()
	return nil
}

func (p *Palette) run() error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if entry, ok := p.highlighted(p.completions()); ok {
		return p.appendWord(entry.Value)
	}
	c := p.parse(p.Text())
	if c.Err != nil {
		return c.Err
	}
	if c.Missing != "" {
		return fmt.Errorf("missing %s", p.describeMissing(c))
	}
	return p.dispatch(c.Request())
}

// describeMissing names the first missing word with its type for a
// parameter.
func (p *Palette) describeMissing(c commandLine) string {
	param, ok := c.Op.Parameters[c.Missing]
	if !c.known() || !ok {
		return c.Missing
	}
	out := c.Missing + " (" + param.Type
	if len(param.Enum) > 0 || param.Source != "" || param.Noun != "" {
		out += ", pick"
	}
	out += ")"
	if param.Description != "" {
		out += ": " + param.Description
	}
	return out
}

func (p *Palette) cancel() error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.Text() != "" {
		p.message = ""
		return p.setText("")
	}
	return p.close()
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
	defer p.sourced()()
	// Palette controls act on the live line; the recursion guard also
	// covers a line that names palette.run itself.
	if p.actions[q.Key()] {
		_, err := p.r.ExecuteAt(origin, q)
		return err
	}
	focus, mode := p.r.Focus, p.r.BaseMode()
	if err := p.close(); err != nil {
		return err
	}
	value, err := p.r.ExecuteAt(origin, q)
	if err == nil {
		present := func(value any) error {
			if p.r.TopModal() != "" {
				return nil // A command or a newer interaction already opened a modal.
			}
			op := p.r.Operations[q.Key()]
			if value == nil && (q.Verb == "list" || op.Rows != "") {
				value = []any{}
			}
			return p.showResults(q.Key(), value)
		}
		if ticket, pending := value.(Ticket); pending {
			if job := p.r.jobs[ticket.Job]; job != nil {
				then := job.then
				job.then = func(c Ctx, value any, workErr error) error {
					var callbackErr error
					if then != nil {
						callbackErr = then(c, value, workErr)
					}
					if workErr != nil || callbackErr != nil || p.r.Instances[ticket.Owner] == nil {
						return callbackErr
					}
					return present(value)
				}
			}
		} else {
			err = present(value)
		}
	}
	if err != nil {
		// Dismiss before dispatch so focus/push/pop act on the originating
		// focus. A failure reopens this same line with its focus and mode.
		if restoreErr := p.r.Push(paletteID, focus); restoreErr != nil {
			return fmt.Errorf("%w (restoring palette: %v)", err, restoreErr)
		}
		p.r.restoreMode(ModeState{Name: mode}, nil)
	}
	return err
}

// sourced records the requests a line runs with the source "palette",
// over the key that ran it; a macro replay keeps its own source.
func (p *Palette) sourced() func() {
	if strings.HasPrefix(p.r.history.source, "macro:") {
		return func() {}
	}
	return p.r.forceSource("palette")
}

// HandleKey dispatches one key while the palette is the active modal.
func (p *Palette) HandleKey(key string) (bool, error) {
	if !p.IsActive() {
		return false, nil
	}
	_, err := p.r.Dispatch(MacroKey{Key: key})
	p.sync()
	if err != nil {
		p.message = err.Error()
	}
	return true, err
}

func (p *Palette) state() map[string]any {
	p.sync()
	c := p.parse(p.Text())
	errText := p.message
	if errText == "" && c.Err != nil {
		errText = c.Err.Error()
	}
	entries := p.completions()
	highlight := -1
	if _, ok := p.highlighted(entries); ok {
		highlight = p.highlight
	}
	return map[string]any{"open": p.IsOpen(), "origin": p.origin.Clone(), "line": p.Text(), "words": c.Words, "operation": c.Op.Key(), "implied": c.Implied, "complete": c.Complete(), "missing": c.Missing, "filter": p.filter, "highlight": highlight, "entries": entries, "error": errText}
}

func (p *Palette) View() string { return p.ViewSized(80, 20) }

func (p *Palette) ViewSized(width, height int) string {
	return p.viewBlock(Styles{}, width, height).Text
}

// viewBlock renders the line, what it resolves to, and the completion list
// grouped by kind, with the line's cursor. A parameter's choices are
// headed by the parameter (paramHeader), drawn in s.
func (p *Palette) viewBlock(s Styles, width, height int) Block {
	if !p.IsOpen() || p.line == nil {
		return Block{}
	}
	p.sync()
	var out strings.Builder
	prefix := ": "
	line := InputBlock(p.line, max(8, width-ansi.StringWidth(prefix)), 1)
	cursor := &Pos{X: ansi.StringWidth(prefix) + line.Cursor.X, Y: 0}
	out.WriteString(prefix + line.Text + "\n")
	c := p.parse(p.Text())
	entries := p.completions()
	switch {
	case p.message != "":
		fmt.Fprintf(&out, "Error: %s\n", p.message)
	case c.Err != nil && len(entries) > 0 && !strings.HasSuffix(p.Text(), " "):
		// The word being typed is still a prefix: the list shows its
		// candidates.
		out.WriteString("  …\n")
	case c.Err != nil:
		fmt.Fprintf(&out, "  %s\n", c.Err)
	case c.Missing != "":
		fmt.Fprintf(&out, "  missing %s\n", p.describeMissing(c))
	default:
		fmt.Fprintf(&out, "  → %s\n", describeRequest(c))
	}
	slot, inSlot := p.currentSlot()
	if inSlot && p.filter != "" && p.filter != "p" {
		inSlot = false
	}
	headed := false
	if len(entries) == 0 {
		if inSlot {
			out.WriteString(p.paramHeader(s, slot, width) + "\n")
			headed = true
		}
		out.WriteString("  No completions\n")
	}
	highlight, ok := p.highlight, false
	if _, ok = p.highlighted(entries); !ok {
		highlight = -1
	}
	limit := max(1, height-4)
	start := max(0, highlight-limit+1)
	group := ""
	for i := start; i < min(len(entries), start+limit); i++ {
		entry := entries[i]
		if heading := map[string]string{"verb": "verbs", "noun": "nouns", "choice": "choices"}[entry.Kind]; heading != group {
			group = heading
			if heading == "choices" && inSlot {
				heading, headed = p.paramHeader(s, slot, width), true
			}
			out.WriteString(heading + "\n")
		}
		marker := "  "
		if i == highlight {
			marker = "> "
		}
		fmt.Fprintf(&out, "%s%s\n", marker, entry.Label)
	}
	hidden := len(entries) - min(len(entries), start+limit)
	if hidden > 0 {
		fmt.Fprintf(&out, "  (%d more)\n", hidden)
	}
	if inSlot && !headed && hidden == 0 {
		// Nothing to pick, but the next word may still fill the parameter
		// (a verb's line lists the nouns that have it).
		out.WriteString(p.paramHeader(s, slot, width) + "\n")
	}
	return Block{Text: out.String(), Cursor: cursor}
}

// describeRequest spells the request a complete line runs:
// "row.transform name=upper", with "(focus)" when the noun was omitted.
func describeRequest(c commandLine) string {
	out := c.Op.Key()
	names := make([]string, 0, len(c.Args))
	for name := range c.Args {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out += fmt.Sprintf(" %s=%v", name, c.Args[name])
	}
	if c.Implied {
		out += " (noun from the focus)"
	}
	return out
}
