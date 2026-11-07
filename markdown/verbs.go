package markdown

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// defaultBindings are the markdown nouns' keys: the one place they are
// written.
//
//go:embed bindings.json
var defaultBindings []byte

// Register defines the markdown nouns, their verbs and keys once per
// runtime (see Doc for the keys). Cursor rows own j/k; gg/G and paging
// remain the list noun's.
func Register(r *ochazuke.Runtime) {
	if _, ok := r.Operations[DocNoun+".toggle"]; ok {
		return
	}
	r.Builtin(func() { register(r) })
}

// writableGuard is true unless the focus is in a read-only Doc;
// writingVerbs are the normal-mode verbs it guards.
const writableGuard = "markdown-writable"

var writingVerbs = []string{"toggle", "undo", "redo", "insert", "open", "cut", "paste", "note", "resolve"}

// docOf is the Doc the event's focus is in (an input of it included).
func docOf(r *ochazuke.Runtime, focus string) *Doc {
	anc := r.Ancestry(focus)
	for i := len(anc) - 1; i >= 0; i-- {
		if d := Of(r, anc[i]); d != nil {
			return d
		}
	}
	return Of(r, focus)
}

// on lifts an edit of the Doc a request names into a verb handler that
// answers the Doc's snapshot.
func on(run func(*Doc, ochazuke.Arguments) error) ochazuke.Handler {
	return func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		d, err := resolve(r, c, a)
		if err != nil {
			return nil, err
		}
		// A refusal (ErrRefused) is a failure: the CLI exits
		// non-zero and the TUI shows it as the key's error.
		err = run(d, a)
		return d.Snapshot(), err
	}
}

// paced is on for } and {, the motions between neighbouring blocks:
// the half-page rule (halfPage, UX-RULES R3.14) caps how far one press
// scrolls. ]] [[ and ] [ with a kind are searches (jumps, as G is):
// a cursor held back on a row of another block would change what
// the next one searches for.
func paced(run func(*Doc, ochazuke.Arguments) error) ochazuke.Handler {
	return on(func(d *Doc, a ochazuke.Arguments) error {
		from := d.cursorLine()
		if err := run(d, a); err != nil {
			return err
		}
		d.halfPage(from)
		return nil
	})
}

func plain(run func(*Doc) error) ochazuke.Handler {
	return on(func(d *Doc, _ ochazuke.Arguments) error { return run(d) })
}

func steps(a ochazuke.Arguments) int { return max(1, ochazuke.IntArg(a, "steps", 1)) }

func move(dir int, match func(*Doc) func(Element) bool, skip bool, what func(*Doc, ochazuke.Arguments) string) ochazuke.Handler {
	return on(func(d *Doc, a ochazuke.Arguments) error {
		return d.Move(dir*steps(a), match(d), skip, what(d, a))
	})
}

func same(d *Doc) func(Element) bool {
	e, _ := d.cursor()
	if e < 0 {
		return func(Element) bool { return false }
	}
	class := kindClass(d.elems[e].Kind)
	return func(el Element) bool { return kindClass(el.Kind) == class }
}

func sameWhat(d *Doc, _ ochazuke.Arguments) string {
	if e, _ := d.cursor(); e >= 0 {
		return kindClass(d.elems[e].Kind)
	}
	return "element"
}

func byKind(dir int) ochazuke.Handler {
	return on(func(d *Doc, a ochazuke.Arguments) error {
		kinds := KindLetters[ochazuke.StringArg(a, "kind", "h")]
		if kinds == nil {
			return fmt.Errorf("unknown kind %q", ochazuke.StringArg(a, "kind", ""))
		}
		skip := slices.Contains(kinds, KindTable)
		return d.Move(dir*steps(a), func(el Element) bool { return slices.Contains(kinds, el.Kind) }, skip, kindName(ochazuke.StringArg(a, "kind", "h")))
	})
}

func docMove(d *Doc, a ochazuke.Arguments) error {
	return d.MoveCursor(ochazuke.IntArg(a, "delta", 1), steps(a))
}
func docNextLink(d *Doc, a ochazuke.Arguments) error     { return d.MoveLink(1, steps(a)) }
func docPreviousLink(d *Doc, a ochazuke.Arguments) error { return d.MoveLink(-1, steps(a)) }
func docDoneShow(d *Doc) error                           { return d.SetDone("show") }
func docDoneHide(d *Doc) error                           { return d.SetDone("hide") }
func docDoneToggle(d *Doc) error                         { return d.SetDone("toggle") }
func docNextBlock(d *Doc, a ochazuke.Arguments) error    { return d.MoveComponent(steps(a)) }
func docPreviousBlock(d *Doc, a ochazuke.Arguments) error {
	return d.MoveComponent(-steps(a))
}
func docInsert(d *Doc, a ochazuke.Arguments) error {
	return d.Insert(ochazuke.StringArg(a, "at", "cursor"))
}
func docOpen(d *Doc, a ochazuke.Arguments) error {
	return d.Open(ochazuke.StringArg(a, "where", "below"))
}
func docPaste(d *Doc, a ochazuke.Arguments) error {
	return d.Paste(ochazuke.StringArg(a, "where", "below") != "above")
}
func docIndent(d *Doc) error  { return d.Indent(1) }
func docOutdent(d *Doc) error { return d.Indent(-1) }
func docCell(d *Doc, a ochazuke.Arguments) error {
	return d.Cell(ochazuke.IntArg(a, "delta", 1))
}

// docVerb is one row of the Doc noun's verb table: the verb, what it
// does, the handler that runs it, and whether it is a motion.
type docVerb struct {
	verb, description string
	run               ochazuke.Handler
	motion            bool
}

// docVerbs are the Doc noun's verbs, in the order they are declared.
func docVerbs() []docVerb {
	return []docVerb{
		{"move", "Move by Markdown element, or drawn line with markdown.cursor=line; spill at the document's edges", on(docMove), true},
		{"link-open", "Open the focused Markdown link with the system URL handler", plain((*Doc).OpenLink), false},
		{"link-copy", "Yank the focused Markdown link URL", plain((*Doc).CopyLink), false},
		{"next-link", "Jump to the next Markdown link", on(docNextLink), true},
		{"previous-link", "Jump to the previous Markdown link", on(docPreviousLink), true},
		{"toggle", "Toggle the checklist item under the cursor: rewrites only its box character in the source, written at once", plain((*Doc).Toggle), false},
		{"done-show", "Show checked tasks in this document for the session; never changes the source or settings", plain(docDoneShow), false},
		{"done-hide", "Hide checked tasks and their subtrees in this document for the session", plain(docDoneHide), false},
		{"done-toggle", "Toggle checked-task visibility in this document for the session", plain(docDoneToggle), false},
		{"undo", "Undo the last edit (a toggle, a typed block, a line deleted or pasted); the undo is written like any edit", plain((*Doc).Undo), false},
		{"redo", "Redo the last undone edit", plain((*Doc).Redo), false},
		{"reload", "Read the Markdown source again", plain((*Doc).Reload), false},
		{"next-block", "Move the cursor to the next component: a heading, a paragraph, a whole list, a table, a code block, a quote, an item's notes", paced(docNextBlock), true},
		{"previous-block", "Move the cursor to the start of the previous component", paced(docPreviousBlock), true},
		{"next-same", "Move the cursor to the next block of the same kind as the one it is on", move(1, same, true, sameWhat), true},
		{"previous-same", "Move the cursor to the previous block of the same kind as the one it is on", move(-1, same, true, sameWhat), true},
		{"next-kind", "Move the cursor to the next block of a kind: h heading, t table, x checklist item, l list item, c code, q quote, n note, p paragraph", byKind(1), true},
		{"previous-kind", "Move the cursor to the previous block of a kind (see next-kind)", byKind(-1), true},
		{"insert", "Edit the block under the cursor as Markdown, in place: at the start of the cursor line's text (cursor), the end of the block (after), the start of the line (start) or its end (end); written as you type", on(docInsert), false},
		{"open", "Open a new line below or above the block in insert mode, continuing its structure: a list item with the same marker, a table row, a note, else a paragraph", on(docOpen), false},
		{"cut", "Cut the cursor's line or block (an item with its notes and nested items, a heading's line only); yanked, written at once", plain((*Doc).Delete), false},
		{"yank", "Yank the cursor's line or block as lines (what cut would take)", plain((*Doc).Yank), false},
		{"paste", "Paste the yanked lines below (or above) the block; written at once", on(docPaste), false},
		{"note", "Add a note under the list item: insert mode in a new note line after its notes, signed by the note author (the markdown.note-author setting) and dated today", plain((*Doc).AddNote), false},
		{"resolve", "Mark the note under the cursor resolved (✓ after its date), or open again; it stays in the file", plain((*Doc).Resolve), false},
		{"fold", "Fold or unfold the list item's notes; a folded item shows how many are open", plain((*Doc).Fold), false},
		{"outline", "Jump to a heading: a picker over the document's headings", plain((*Doc).Outline), false},
		{"leave", "Leave the block editor: the block is written, tidied (table pipes aligned, numbers renumbered) and drawn again", plain((*Doc).Leave), false},
		{"continue-item", "Enter on a list item: end it at the cursor and start the next item below, the rest of the line in it", plain((*Doc).ContinueItem), false},
		{"end-list", "Enter on an empty list item: drop it and start a paragraph after the list", plain((*Doc).EndList), false},
		{"next-row", "Enter on a table row: start a new row below it", plain((*Doc).NextRow), false},
		{"after-heading", "Enter on a heading: start a paragraph below it", plain((*Doc).AfterHeading), false},
		{"quote-line", "Enter in a quote or note: break the line and quote the next", plain((*Doc).QuoteLine), false},
		{"indent", "Nest the list item being edited under the item above it", plain(docIndent), false},
		{"outdent", "Take the list item being edited out one level", plain(docOutdent), false},
		{"cell", "Move to the next (delta 1) or previous (-1) table cell; past the last, a new row", on(docCell), true},
		{"unmark", "Take the marker off an empty list item, leaving an indented line under the item above", plain((*Doc).Unmark), false},
		{"insert-block", "On an empty line: choose a block to insert (table, checklist, bullets, numbered list, heading 1 to 3, code block, quote, rule)", plain((*Doc).BlockChooser), false},
	}
}

// editing is the guard test for the block editor's own keys: the focus
// is in the Doc's open editor and test holds of the line being edited.
func editing(r *ochazuke.Runtime, ev ochazuke.KeyEvent, test func(buffer) bool) bool {
	d := docOf(r, ev.Focus)
	if d == nil || d.edit == nil || r.Focus != d.edit.input.ID {
		return false
	}
	b, _ := d.buffer()
	return test(b)
}

// cursorIs is the guard test for the keys of the Doc's cursor element.
func cursorIs(test func(d *Doc, e int) bool) ochazuke.Guard {
	return func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		d := docOf(r, ev.Focus)
		if d == nil || d.edit != nil {
			return false
		}
		e, _ := d.cursor()
		return e >= 0 && test(d, e)
	}
}

// lineGuard is a guard of the block editor's line being edited.
func lineGuard(test func(buffer) bool) ochazuke.Guard {
	return func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool { return editing(r, ev, test) }
}

func hasNotes(d *Doc, e int) bool {
	item := itemOf(d.elems, e)
	for k := item + 1; item >= 0 && k <= subtreeEnd(d.elems, item); k++ {
		if d.elems[k].Kind == KindNote && d.elems[k].Item == item {
			return true
		}
	}
	return false
}

// docGuards are the Markdown keys' guards.
func docGuards() map[string]ochazuke.Guard {
	return map[string]ochazuke.Guard{
		"markdown-item-line": lineGuard(func(b buffer) bool { return isItemLine(b.line) }),
		"markdown-item-text": lineGuard(func(b buffer) bool {
			return isItemLine(b.line) && !(emptyItem(b.line) && strings.TrimSpace(b.after) == "")
		}),
		"markdown-heading-line": lineGuard(func(b buffer) bool { return strings.HasPrefix(b.line, "#") && !strings.Contains(b.text, "\n") }),
		"markdown-quote-line":   lineGuard(func(b buffer) bool { return strings.HasPrefix(strings.TrimLeft(b.line, " "), ">") }),
		"markdown-table-line":   lineGuard(func(b buffer) bool { return strings.HasPrefix(strings.TrimLeft(b.line, " "), "|") }),
		"markdown-empty-item": lineGuard(func(b buffer) bool {
			return emptyItem(b.line) && strings.TrimSpace(b.after) == "" && isItemLine(b.before)
		}),
		"markdown-empty-line": lineGuard(func(b buffer) bool { return strings.TrimSpace(b.line) == "" }),
		"markdown-on-link":    cursorIs(func(d *Doc, _ int) bool { _, ok := d.FocusedLink(); return ok }),
		"markdown-on-task":    cursorIs(func(d *Doc, e int) bool { return d.elems[e].Kind == KindTask }),
		"markdown-on-item":    cursorIs(func(d *Doc, e int) bool { return itemOf(d.elems, e) >= 0 }),
		"markdown-has-notes":  cursorIs(hasNotes),
		// A read-only Doc (no Save: the Markdown viewer) leaves out the
		// verbs that write, so their keys leave its hints and fall through.
		writableGuard: func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
			d := docOf(r, ev.Focus)
			return d == nil || d.Editable()
		},
	}
}

// kindLetters are the kind letters next-kind and previous-kind take.
var kindLetters = []string{"h", "t", "x", "l", "c", "q", "n", "p"}

const kindHelp = "h heading, t table, x checklist item, l list item, c code, q quote, n note, p paragraph"

// docParams are the parameters each Doc verb takes beside id.
func docParams() map[string]map[string]ochazuke.Parameter {
	count := ochazuke.Parameter{Type: "integer", Description: "Repeat count (3})"}
	kind := ochazuke.Parameter{Type: "string", Required: true, Enum: kindLetters, Description: kindHelp}
	where := ochazuke.Parameter{Type: "string", Enum: []string{"below", "above"}}
	return map[string]map[string]ochazuke.Parameter{
		"next-link": {"steps": count}, "previous-link": {"steps": count},
		"move":       {"delta": {Type: "integer"}, "steps": count},
		"next-block": {"steps": count}, "previous-block": {"steps": count},
		"next-same": {"steps": count}, "previous-same": {"steps": count},
		"next-kind":     {"steps": count, "kind": kind},
		"previous-kind": {"steps": count, "kind": kind},
		"insert":        {"at": {Type: "string", Enum: []string{"cursor", "after", "start", "end"}}},
		"open":          {"where": where},
		"paste":         {"where": where},
		"cell":          {"delta": {Type: "integer"}},
	}
}

// describeDocOperations gives every Doc verb its parameters (id, then its
// own), its answer and its history kind.
func describeDocOperations(r *ochazuke.Runtime) {
	id := ochazuke.Parameter{Type: "string", Description: "The markdown instance (default the one on the focus path)"}
	params := docParams()
	for _, op := range r.Operations {
		if op.Noun != DocNoun {
			continue
		}
		p := map[string]ochazuke.Parameter{"id": id}
		for name, param := range params[op.Verb] {
			p[name] = param
		}
		op.Parameters = p
		switch op.Verb {
		case "link-open", "link-copy", "next-link", "previous-link":
			op.HistoryKind = ochazuke.HistoryAction
		}
		op.Result = "markdown state"
		r.Operations[op.Key()] = op
	}
}

// docSettings are the settings the Markdown nouns declare.
func docSettings() []ochazuke.Setting {
	return []ochazuke.Setting{
		{Name: LinkStopsSetting, Owner: ochazuke.OwnerFramework, Type: "boolean", Default: true, Description: "Visit inline Markdown links as focus stops with j and k"},
		{Name: AuthorSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "owner", Description: "The author a new note in a Markdown document is signed with"},
		{Name: CursorSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "item", Enum: []string{"item", "line"}, Description: "Markdown cursor unit: whole elements, or individual drawn lines"},
		{Name: CursorWidthSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "pane", Enum: []string{"pane", "text"}, Description: "Markdown cursor highlight width: the whole pane, or only its text"},
		{Name: DoneSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "show", Enum: []string{"show", "hide"}, Description: "Show checked Markdown tasks, or hide them with their nested subtrees"},
		{Name: DoneIndicatorSetting, Owner: ochazuke.OwnerFramework, Type: "boolean", Default: true, Description: "Show a dim count above a Markdown document when checked tasks are hidden"},
		{Name: WrapSetting, Owner: ochazuke.OwnerFramework, Type: "boolean", Default: true, Description: "Soft-wrap long lines in the Markdown block editor at the last space that fits; off, they scroll sideways. The source never gets added line breaks"},
		{Name: ExtraSpaceSetting, Owner: ochazuke.OwnerFramework, Type: "boolean", Default: false, Description: "Add one blank line between rendered Markdown components, including adjacent list items"},
	}
}

// relayoutDocs lays every Doc out again after a setting changed.
func relayoutDocs(r *ochazuke.Runtime) {
	for _, inst := range r.Instances {
		if d, ok := inst.Component.(*Doc); ok {
			if d.edit != nil {
				d.dirty = true
			} else {
				e, off := d.cursor()
				_ = d.relayout(e, off)
			}
		}
	}
}

func register(r *ochazuke.Runtime) {
	for name, guard := range docGuards() {
		r.DefineGuard(name, guard)
	}
	r.Define(TaskNoun)
	r.Define(LineNoun)
	r.Define(NoteNoun)
	b := r.Define(DocNoun)
	for _, v := range docVerbs() {
		b.Verb(v.verb, v.run, v.description)
		if v.motion {
			b.Motion()
		}
	}
	if n := r.Noun(DocNoun); n != nil {
		if n.Guards == nil {
			n.Guards = map[string]string{}
		}
		for _, verb := range writingVerbs {
			n.Guards[verb] = writableGuard
		}
	}
	registerView(r)
	describeDocOperations(r)
	if r.Settings() != nil {
		for _, s := range docSettings() {
			_ = r.DeclareSetting(s)
		}
		for _, name := range []string{DoneSetting, DoneIndicatorSetting, ExtraSpaceSetting} {
			r.Settings().Observe(name, func(string, any) { relayoutDocs(r) })
		}
	}
	r.MustLoadDefaultBindings(ochazuke.FrameworkBindingsSource+"markdown/bindings.json", defaultBindings)
}

// kindName names a kind letter for "no next …".
func kindName(letter string) string {
	switch letter {
	case "h":
		return "heading"
	case "t":
		return "table"
	case "x":
		return "checklist item"
	case "l":
		return "list item"
	case "c":
		return "code block"
	case "q":
		return "quote"
	case "n":
		return "note"
	case "p":
		return "paragraph"
	}
	return "block"
}

// resolve is the Doc named by args.id, else the nearest on the context.
func resolve(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (*Doc, error) {
	if id := ochazuke.StringArg(a, "id", ""); id != "" {
		if d := Of(r, id); d != nil {
			return d, nil
		}
		return nil, fmt.Errorf("unknown markdown %q", id)
	}
	for i := len(c.Path) - 1; i >= 0; i-- {
		if d := Of(r, c.Path[i]); d != nil {
			return d, nil
		}
	}
	return nil, errors.New("no markdown on the focus path")
}
