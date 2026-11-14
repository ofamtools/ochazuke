package ochazuke

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
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
	// loads holds choices that loaded off the loop (a Pending source),
	// by paramChoices' key, until the palette opens again; loadingKey is
	// the key of the one load in flight, loadSeq the current load's
	// number (an answer of an older one, or of an earlier open, is
	// dropped). loadingNote is the loading line for the slot's choices
	// while its load runs (set by completions).
	loads       map[string]choiceList
	loadingKey  string
	loadSeq     int
	loadingNote string
	// chipWord and chipKind are the first word as the board chose it (a
	// noun or a verb: tag is both), so the second word shows only the
	// other kind; cols is the grid's width in cells as last drawn;
	// committing holds while a typed letter commits the one cell left.
	chip PaletteEntry
	// typedFrom is what was typed of the first word when it committed
	// (qua of quad), so a letter that fits no verb but continues that word
	// takes the word back (reopenWord).
	typedFrom  string
	cols       int
	committing bool
	// cache holds the offered answers and line indexes for one stamp
	// (Palette.index); nounsGen moves on every palette noun registered.
	cache    *paletteMemo
	nounsGen int
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
	register := func(verb, description string, params map[string]Parameter, fn func(Arguments) error, traits ...opTrait) {
		p.actions["ochazuke.palette."+verb] = true
		op := Operation{Key: KeyOf("ochazuke.palette", verb), Description: description, Parameters: params, Result: "palette state", Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
			if verb != "open" {
				p.sync()
			}
			err := fn(args)
			if err != nil {
				p.message = err.Error()
			}
			return p.state(), err
		}}
		applyTraits(&op, traits)
		r.Register(op)
	}
	register("open", "Open the command line", map[string]Parameter{"recall": {Type: "integer", Description: "Open on the command that many back in the palette's history, as up would (1 the last; 0 or absent an empty line)"}}, func(a Arguments) error {
		if err := p.open(); err != nil {
			return err
		}
		if n := IntArg(a, "recall", 0); n > 0 && len(p.recallLines()) > 0 {
			return p.recallLine(n)
		}
		return nil
	})
	register("close", "Close the command line without running it", nil, func(Arguments) error { return p.close() })
	register("fill", "Fill the command line: replace its text", map[string]Parameter{"text": {Type: "string", Required: true}}, func(a Arguments) error {
		if err := p.requireOpen(); err != nil {
			return err
		}
		return p.setText(StringArg(a, "text", ""))
	})
	register("move", "Move the completion highlight; from none, down selects first and up follows picker.up-from-none", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(a Arguments) error { return p.move(IntArg(a, "delta", 0)) }, asMotion, asHistory(HistoryAction))
	register("complete", "Append the highlighted completion (or value, or the only one) as the next word", map[string]Parameter{"value": {Type: "string", Description: "A completion's word; omit for the highlighted one"}}, p.complete)
	register("run", "Run the line or append the highlighted completion; a line missing an argument opens the args form for it (palette.missing-opens-form), else names its first missing word", nil, func(Arguments) error { return p.run() })
	register("cancel", "Clear the command line, or close it when it is empty; on the board go up one layer (drop the last word), closing from an empty line", nil, func(Arguments) error { return p.cancel() })
	register("cell", "Move the board's current cell: left, right, up, down, or to an edge (first, last, first-col, last-col)", map[string]Parameter{"to": {Type: "string", Required: true, Enum: cellMoves}}, func(a Arguments) error { return p.cellTo(StringArg(a, "to", "")) }, asMotion, asHistory(HistoryAction))
	register("step", "Up or down: the board's cell above or below; in the classic style the completion highlight while the line has completions, else the command history (up older)", map[string]Parameter{"direction": {Type: "string", Required: true, Enum: cellMoves[cellUp : cellDown+1]}}, func(a Arguments) error { return p.step(StringArg(a, "direction", "")) }, asMotion, asHistory(HistoryAction))
	register("back", "Go back a layer: the last committed word when the line ends in a space, else one character before the cursor", nil, func(Arguments) error { return p.back() })
	register("recall", "Replace the line with an earlier palette command from the action history: 1 older, -1 newer", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(a Arguments) error { return p.recallLine(IntArg(a, "delta", 0)) })
	// The palette's keys (bindings/palette.json), all on its own noun: the
	// line beneath is an ordinary prompt, so the editor's Readline rows
	// answer the rest. Printable text has its own row so an application's
	// any-mode row cannot steal typing.
	r.Define("ochazuke.palette").Focusable().Unpersisted()
	r.loadFrameworkBindings("palette.json")
	// The palette is its own component; the pickers picker lists it as
	// commands, which opens it (picker_registry.go).
	if err := r.AddPicker(PickerDef{Name: CommandsPickerName, Title: "Commands", About: "The command line: every verb, typed with its arguments", Verb: "ochazuke.palette.open"}); err != nil {
		panic(err)
	}
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
	// esc (modal.close) clears the line first, closing only an empty one.
	if err := p.r.SetModalClose(paletteID, Request{Noun: "ochazuke.palette", Verb: "cancel"}); err != nil {
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
	p.loads, p.loadingKey, p.loadingNote = nil, "", ""
	p.loadSeq++
	if err := p.line.Replace(0, len(p.line.Text), ""); err != nil {
		return err
	}
	p.seen = ""
	p.r.setModalFocus(paletteID, p.line.ID)
	if !p.styleBoard() {
		// The board's line stays in insert, so esc goes up a layer
		// (palette.cancel) instead of leaving insert.
		p.r.releaseModalPin(p.line)
	}
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
	if _, ok := p.highlighted(entries); !ok {
		p.highlight = -1
	}
	p.highlight = p.r.moveHighlight(p.highlight, delta, len(entries), true)
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
	return p.accept(entry)
}

// accept takes a completion: a word replaces the word being typed (and a
// space follows), a rest parameter's choice is spliced over the line from
// its From to the cursor, with the cursor after it.
func (p *Palette) accept(entry PaletteEntry) error {
	if !entry.Splice {
		if layer, _, board := p.boardLayer(); board && !p.committing {
			p.committing = true
			defer func() { p.committing = false }()
			return p.takeCell(entry, layer)
		}
		return p.appendWord(entry.Value)
	}
	at := max(0, min(p.line.Cursor, len(p.line.Text)))
	from := max(0, min(entry.From, at))
	if err := p.line.Replace(from, at, entry.Value); err != nil {
		return err
	}
	p.sync()
	return nil
}

// appendWord replaces the word being typed (or adds one after a space) and
// a space, so the list moves on to the next word.
func (p *Palette) appendWord(word string) error {
	text := p.Text()
	if tokens, trailing := tokenize(text); !trailing {
		// From where the word being typed starts: a quoted word may hold
		// spaces ("Pull R).
		text = string([]rune(text)[:tokens[len(tokens)-1].Start])
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
		return p.accept(entry)
	}
	if _, _, board := p.boardLayer(); board {
		// The board's current cell is the first until an arrow moves it.
		if cells := p.completions(); len(cells) > 0 {
			return p.accept(cells[0])
		}
	}
	c := p.parse(p.Text())
	if c.Err != nil {
		return c.Err
	}
	for _, name := range c.Old {
		p.r.NoteNounAlias(name) // a deprecated noun alias: accepted, with its one-time notice
	}
	if a, ok := p.r.actionNames.aliases[c.Via]; ok && len(c.OldParams) > 0 {
		for _, text := range p.r.paramAliasNotices(a, c.OldParams) {
			p.r.UI().Notify(text) // an old parameter name: accepted, with its one-time notice
		}
	}
	if c.Missing != "" {
		if _, declared := c.Op.Parameters[c.Missing]; declared && c.known() && p.r.missingOpensForm() {
			// The args form asks for what the line lacks; esc there comes
			// back to this line as typed.
			if _, err := p.r.OpenArgsForm(c.Request(), p.dispatch); err == nil {
				return nil
			}
		}
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
		if p.styleBoard() {
			return p.up()
		}
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
	top := p.r.TopModal()
	present := p.r.resultPresenter(q, func(value any) (bool, error) {
		return p.present(q, value, top)
	})
	q = p.reach(q)
	value, err := p.r.ExecuteAt(origin, q)
	if err == nil {
		// Keys and palette share the same focus/mode/page/modal guard.
		err = present(value)
	}
	if err != nil {
		// Dismiss before dispatch so focus/push/pop act on the originating
		// focus. A failure reopens this same line with its focus and mode.
		p.r.reopening = paletteID // its own level: it opened over whatever is open now
		restoreErr := p.r.Push(paletteID, focus)
		p.r.reopening = ""
		if restoreErr != nil {
			return fmt.Errorf("%w (restoring palette: %v)", err, restoreErr)
		}
		p.r.restoreMode(ModeState{Name: mode}, nil)
		// A verb that says it lacks a value asks for it: the form opens
		// over the restored line, esc comes back to it, confirm runs the
		// request again (askNeed).
		if p.r.askNeed(q, err, p.dispatch, false) {
			return nil
		}
	}
	return err
}

func (p *Palette) present(q Request, value any, top string) (bool, error) {
	return p.r.presentAnswer(q, value, top)
}

// presentAnswer is the one presenter of an answer for a person's request
// that is not a key: the palette's, and the help's run. A text answer opens
// its viewer, rows open as a table, any other data answer of an application
// verb is shown (presentData); top is the modal level the request started
// at, so a verb that opened a modal (a confirm) has said what it has to.
// Wrapped by resultPresenter, an answer that lands later is presented the
// same way.
func (r *Runtime) presentAnswer(q Request, value any, top string) (bool, error) {
	value = AnswerRows(value)
	op := r.Operations.At(Key(q.Key()))
	if value == nil && (q.Verb == "list" || op.Rows != "") {
		value = []any{}
	}
	if shown, err := r.PresentResult(q, value); shown || err != nil {
		return shown, err
	}
	if run := r.Operations.At(Key(requestOperation(q))); r.StandardVerbsView().Record {
		if row := standardRow(run.Standard); row != nil && row.Record {
			if rows, ok := recordRows(r.Nouns[run.Noun()], value); ok {
				value = rows
			} else if prop := cmp.Or(q.Prop, StringArg(q.Args, "prop", "")); prop != "" {
				value = []map[string]any{{"prop": prop, "value": value}}
			}
		}
	}
	before := r.Instances[r.TopModal()]
	err := r.showResults(q.Key(), value)
	if err == nil && before == r.Instances[r.TopModal()] && r.TopModal() == top {
		err = r.presentData(q, value) // a verb that opened a modal (a confirm) has said what it has to
	}
	return before != r.Instances[r.TopModal()], err
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
	return map[string]any{"open": p.IsOpen(), "origin": p.origin.Clone(), "line": p.Text(), "words": c.Words, "operation": c.Op.Key.Short(), "implied": c.Implied, "complete": c.Complete(), "missing": c.Missing, "filter": p.filter, "highlight": highlight, "entries": entries, "error": errText}
}

func (p *Palette) View() string { return p.ViewSized(80, 20) }

func (p *Palette) ViewSized(width, height int) string {
	return p.viewBlock(Styles{}, width, height).Text
}

// paletteEntryLabel is an entry's label drawn in s: its own tones, with the
// match marks added when markMatches.
func paletteEntryLabel(s Styles, entry PaletteEntry, matcher Matcher, markMatches bool) string {
	label := entry.Label
	if markMatches {
		match := matcher.positions(label)
		return s.tonedText(label, append(slices.Clone(entry.Tones), positionSpans(match, chromeTonePrefix+s.chrome+".palette.match")...), lipgloss.NewStyle())
	}
	if len(entry.Tones) > 0 {
		return s.tonedText(label, entry.Tones, lipgloss.NewStyle())
	}
	return label
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
	line := InputBlock(p.line, max(8, width-ansi.StringWidth(prefix)), GrownRows(p.line, max(8, width-ansi.StringWidth(prefix)), max(1, height/3)))
	cursor := &Pos{X: ansi.StringWidth(prefix) + line.Cursor.X, Y: line.Cursor.Y}
	out.WriteString(prefix + indentRows(line.Text, ansi.StringWidth(prefix)) + "\n")
	if board, ok := p.boardBlock(s, width, height); ok {
		out.WriteString(board)
		return Block{Text: out.String(), Cursor: cursor}
	}
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
		fmt.Fprintf(&out, "  %s\n", s.chromeRender("palette.resolve", "→ "+describeRequest(c)))
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
		out.WriteString(p.noCompletionsLine())
	}
	highlight, ok := p.highlight, false
	if _, ok = p.highlighted(entries); !ok {
		highlight = -1
	}
	limit := max(1, height-4)
	var matcher Matcher
	markMatches := s.HasChrome("palette.match")
	if markMatches {
		tokens, trailing := tokenize(p.Text())
		partial := ""
		if !trailing && len(tokens) > 0 {
			partial = tokens[len(tokens)-1].Text
		}
		matcher = NewMatcher(MatchSubstring, SearchCaseIgnore, partial)
	}
	start := max(0, highlight-limit+1)
	group := ""
	for i := start; i < min(len(entries), start+limit); i++ {
		entry := entries[i]
		if heading := map[string]string{"verb": "verbs", "noun": "noun", "prop": "props", "choice": "choices"}[entry.Kind]; heading != group {
			group = heading
			if heading == "choices" && inSlot {
				heading, headed = p.paramHeader(s, slot, width), true
			}
			out.WriteString(s.chromeRender("palette.heading", heading) + "\n")
		}
		out.WriteString(p.entryLine(s, entry, i == highlight, matcher, markMatches, width) + "\n")
	}
	hidden := len(entries) - min(len(entries), start+limit)
	if hidden > 0 {
		fmt.Fprintf(&out, "  (%d more)\n", hidden)
	}
	out.WriteString(p.stillLoadingLine(entries))
	if inSlot && !headed && hidden == 0 {
		// Nothing to pick, but the next word may still fill the parameter
		// (a verb's line lists the nouns that have it).
		out.WriteString(p.paramHeader(s, slot, width) + "\n")
	}
	return Block{Text: out.String(), Cursor: cursor}
}

// noCompletionsLine is the line for an empty completion list: the slot's
// loading line while its choices load off the loop (a Pending source), so
// the list never reads as empty while they come (R5.19).
func (p *Palette) noCompletionsLine() string {
	if p.loadingNote != "" {
		return "  " + p.loadingNote + "\n"
	}
	return "  No completions\n"
}

// stillLoadingLine ends a list of other words (verbs, props) with the
// slot's loading line while its choices are still coming; "" otherwise.
func (p *Palette) stillLoadingLine(entries []PaletteEntry) string {
	if len(entries) == 0 || p.loadingNote == "" {
		return ""
	}
	return "  " + p.loadingNote + "\n"
}

// describeRequest spells the request a complete line runs:
// "row.transform name=upper", with "(focus)" when the noun was omitted.
func describeRequest(c commandLine) string {
	out := c.Op.Key.Short()
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

// focusLine is a row drawn as the focus: the text on the whole width in the
// palette.focus tone (the accent filled, bold), or reversed where the theme
// has none, so it reads in a terminal without colour.
func focusLine(s Styles, text string, width int) string {
	text = " " + ansi.Truncate(text, max(1, width-2), "…")
	text += strings.Repeat(" ", max(0, width-1-ansi.StringWidth(text)))
	style := lipgloss.NewStyle().Reverse(true).Bold(true)
	if look, ok := s.chromeLook("palette.focus"); ok {
		style = look.over(lipgloss.NewStyle())
	}
	return style.Render(text)
}

// entryLine is one completion row: the label (with its match marks and the
// reason it cannot run, when it cannot), the focused one filled across the
// width, or marked with > under palette.marker arrow.
func (p *Palette) entryLine(s Styles, entry PaletteEntry, focused bool, matcher Matcher, markMatches bool, width int) string {
	label := paletteEntryLabel(s, entry, matcher, markMatches)
	if entry.Reason != "" {
		label += " · unavailable: " + entry.Reason
	}
	if entry.Unusual {
		label += " · " + NotUsualHere
	}
	if focused && p.r.Settings().String(PaletteMarkerSetting) != PaletteMarkerArrow {
		return focusLine(s, ansi.Strip(label), width)
	}
	marker := "  "
	if focused {
		marker = "> "
	}
	return s.chromeRender("palette.marker", marker) + label
}
