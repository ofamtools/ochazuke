package markdown

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
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

func register(r *ochazuke.Runtime) {
	on := func(run func(*Doc, ochazuke.Arguments) error) ochazuke.Handler {
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
	paced := func(run func(*Doc, ochazuke.Arguments) error) ochazuke.Handler {
		return on(func(d *Doc, a ochazuke.Arguments) error {
			from := d.cursorLine()
			if err := run(d, a); err != nil {
				return err
			}
			d.halfPage(from)
			return nil
		})
	}
	plain := func(run func(*Doc) error) ochazuke.Handler {
		return on(func(d *Doc, _ ochazuke.Arguments) error { return run(d) })
	}
	steps := func(a ochazuke.Arguments) int { return max(1, ochazuke.IntArg(a, "steps", 1)) }
	move := func(dir int, match func(*Doc) func(Element) bool, skip bool, what func(*Doc, ochazuke.Arguments) string) ochazuke.Handler {
		return on(func(d *Doc, a ochazuke.Arguments) error {
			return d.Move(dir*steps(a), match(d), skip, what(d, a))
		})
	}
	same := func(d *Doc) func(Element) bool {
		e, _ := d.cursor()
		if e < 0 {
			return func(Element) bool { return false }
		}
		class := kindClass(d.elems[e].Kind)
		return func(el Element) bool { return kindClass(el.Kind) == class }
	}
	sameWhat := func(d *Doc, _ ochazuke.Arguments) string {
		if e, _ := d.cursor(); e >= 0 {
			return kindClass(d.elems[e].Kind)
		}
		return "element"
	}
	kindIn := func(a ochazuke.Arguments) []string { return KindLetters[ochazuke.StringArg(a, "kind", "h")] }
	byKind := func(dir int) ochazuke.Handler {
		return on(func(d *Doc, a ochazuke.Arguments) error {
			kinds := kindIn(a)
			if kinds == nil {
				return fmt.Errorf("unknown kind %q", ochazuke.StringArg(a, "kind", ""))
			}
			skip := slices.Contains(kinds, KindTable)
			return d.Move(dir*steps(a), func(el Element) bool { return slices.Contains(kinds, el.Kind) }, skip, kindName(ochazuke.StringArg(a, "kind", "h")))
		})
	}
	editing := func(r *ochazuke.Runtime, ev ochazuke.KeyEvent, test func(buffer) bool) bool {
		d := docOf(r, ev.Focus)
		if d == nil || d.edit == nil || r.Focus != d.edit.input.ID {
			return false
		}
		b, _ := d.buffer()
		return test(b)
	}
	r.DefineGuard("markdown-item-line", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool { return isItemLine(b.line) })
	})
	r.DefineGuard("markdown-item-text", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool {
			return isItemLine(b.line) && !(emptyItem(b.line) && strings.TrimSpace(b.after) == "")
		})
	})
	r.DefineGuard("markdown-heading-line", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool { return strings.HasPrefix(b.line, "#") && !strings.Contains(b.text, "\n") })
	})
	r.DefineGuard("markdown-quote-line", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool { return strings.HasPrefix(strings.TrimLeft(b.line, " "), ">") })
	})
	r.DefineGuard("markdown-table-line", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool { return strings.HasPrefix(strings.TrimLeft(b.line, " "), "|") })
	})
	r.DefineGuard("markdown-empty-item", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool {
			return emptyItem(b.line) && strings.TrimSpace(b.after) == "" && isItemLine(b.before)
		})
	})
	r.DefineGuard("markdown-empty-line", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return editing(r, ev, func(b buffer) bool { return strings.TrimSpace(b.line) == "" })
	})
	cursorIs := func(test func(d *Doc, e int) bool) ochazuke.Guard {
		return func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
			d := docOf(r, ev.Focus)
			if d == nil || d.edit != nil {
				return false
			}
			e, _ := d.cursor()
			return e >= 0 && test(d, e)
		}
	}
	r.DefineGuard("markdown-on-link", cursorIs(func(d *Doc, _ int) bool { _, ok := d.FocusedLink(); return ok }))
	r.DefineGuard("markdown-on-task", cursorIs(func(d *Doc, e int) bool { return d.elems[e].Kind == KindTask }))
	r.DefineGuard("markdown-on-item", cursorIs(func(d *Doc, e int) bool { return itemOf(d.elems, e) >= 0 }))
	r.DefineGuard("markdown-has-notes", cursorIs(func(d *Doc, e int) bool {
		item := itemOf(d.elems, e)
		for k := item + 1; item >= 0 && k <= subtreeEnd(d.elems, item); k++ {
			if d.elems[k].Kind == KindNote && d.elems[k].Item == item {
				return true
			}
		}
		return false
	}))
	r.Define(TaskNoun)
	r.Define(LineNoun)
	r.Define(NoteNoun)
	kinds := []string{"h", "t", "x", "l", "c", "q", "n", "p"}
	r.Define(DocNoun).
		Verb("move", on(func(d *Doc, a ochazuke.Arguments) error {
			return d.MoveCursor(ochazuke.IntArg(a, "delta", 1), steps(a))
		}), "Move by Markdown element, or drawn line with markdown.cursor=line; spill at the document's edges").Motion().
		Verb("link-open", plain((*Doc).OpenLink), "Open the focused Markdown link with the system URL handler").
		Verb("link-copy", plain((*Doc).CopyLink), "Yank the focused Markdown link URL").
		Verb("next-link", on(func(d *Doc, a ochazuke.Arguments) error { return d.MoveLink(1, steps(a)) }), "Jump to the next Markdown link").Motion().
		Verb("previous-link", on(func(d *Doc, a ochazuke.Arguments) error { return d.MoveLink(-1, steps(a)) }), "Jump to the previous Markdown link").Motion().
		Verb("toggle", plain((*Doc).Toggle), "Toggle the checklist item under the cursor: rewrites only its box character in the source, written at once").
		Verb("done-show", plain(func(d *Doc) error { return d.SetDone("show") }), "Show checked tasks in this document for the session; never changes the source or settings").
		Verb("done-hide", plain(func(d *Doc) error { return d.SetDone("hide") }), "Hide checked tasks and their subtrees in this document for the session").
		Verb("done-toggle", plain(func(d *Doc) error { return d.SetDone("toggle") }), "Toggle checked-task visibility in this document for the session").
		Verb("undo", plain((*Doc).Undo), "Undo the last edit (a toggle, a typed block, a line deleted or pasted); the undo is written like any edit").
		Verb("redo", plain((*Doc).Redo), "Redo the last undone edit").
		Verb("reload", plain((*Doc).Reload), "Read the Markdown source again").
		Verb("next-block", paced(func(d *Doc, a ochazuke.Arguments) error { return d.MoveComponent(steps(a)) }), "Move the cursor to the next component: a heading, a paragraph, a whole list, a table, a code block, a quote, an item's notes").Motion().
		Verb("previous-block", paced(func(d *Doc, a ochazuke.Arguments) error { return d.MoveComponent(-steps(a)) }), "Move the cursor to the start of the previous component").Motion().
		Verb("next-same", move(1, same, true, sameWhat), "Move the cursor to the next block of the same kind as the one it is on").Motion().
		Verb("previous-same", move(-1, same, true, sameWhat), "Move the cursor to the previous block of the same kind as the one it is on").Motion().
		Verb("next-kind", byKind(1), "Move the cursor to the next block of a kind: h heading, t table, x checklist item, l list item, c code, q quote, n note, p paragraph").Motion().
		Verb("previous-kind", byKind(-1), "Move the cursor to the previous block of a kind (see next-kind)").Motion().
		Verb("insert", on(func(d *Doc, a ochazuke.Arguments) error { return d.Insert(ochazuke.StringArg(a, "at", "cursor")) }), "Edit the block under the cursor as Markdown, in place: at the start of the cursor line's text (cursor), the end of the block (after), the start of the line (start) or its end (end); written as you type").
		Verb("open", on(func(d *Doc, a ochazuke.Arguments) error { return d.Open(ochazuke.StringArg(a, "where", "below")) }), "Open a new line below or above the block in insert mode, continuing its structure: a list item with the same marker, a table row, a note, else a paragraph").
		Verb("delete", plain((*Doc).Delete), "Delete the cursor's line or block (an item with its notes and nested items, a heading's line only); yanked, written at once").
		Verb("yank", plain((*Doc).Yank), "Yank the cursor's line or block as lines (what delete would take)").
		Verb("paste", on(func(d *Doc, a ochazuke.Arguments) error {
			return d.Paste(ochazuke.StringArg(a, "where", "below") != "above")
		}), "Paste the yanked lines below (or above) the block; written at once").
		Verb("note", plain((*Doc).AddNote), "Add a note under the list item: insert mode in a new note line after its notes, signed by the note author (the markdown.note-author setting) and dated today").
		Verb("resolve", plain((*Doc).Resolve), "Mark the note under the cursor resolved (✓ after its date), or open again; it stays in the file").
		Verb("fold", plain((*Doc).Fold), "Fold or unfold the list item's notes; a folded item shows how many are open").
		Verb("outline", plain((*Doc).Outline), "Jump to a heading: a chooser over the document's headings").
		Verb("leave", plain((*Doc).Leave), "Leave the block editor: the block is written, tidied (table pipes aligned, numbers renumbered) and drawn again").
		Verb("continue-item", plain((*Doc).ContinueItem), "Enter on a list item: end it at the cursor and start the next item below, the rest of the line in it").
		Verb("end-list", plain((*Doc).EndList), "Enter on an empty list item: drop it and start a paragraph after the list").
		Verb("next-row", plain((*Doc).NextRow), "Enter on a table row: start a new row below it").
		Verb("after-heading", plain((*Doc).AfterHeading), "Enter on a heading: start a paragraph below it").
		Verb("quote-line", plain((*Doc).QuoteLine), "Enter in a quote or note: break the line and quote the next").
		Verb("indent", plain(func(d *Doc) error { return d.Indent(1) }), "Nest the list item being edited under the item above it").
		Verb("outdent", plain(func(d *Doc) error { return d.Indent(-1) }), "Take the list item being edited out one level").
		Verb("cell", on(func(d *Doc, a ochazuke.Arguments) error { return d.Cell(ochazuke.IntArg(a, "delta", 1)) }), "Move to the next (delta 1) or previous (-1) table cell; past the last, a new row").Motion().
		Verb("unmark", plain((*Doc).Unmark), "Take the marker off an empty list item, leaving an indented line under the item above").
		Verb("insert-block", plain((*Doc).BlockChooser), "On an empty line: choose a block to insert (table, checklist, bullets, numbered list, heading 1 to 3, code block, quote, rule)")
	id := ochazuke.Parameter{Type: "string", Description: "The markdown instance (default the one on the focus path)"}
	count := ochazuke.Parameter{Type: "integer", Description: "Repeat count (3})"}
	params := map[string]map[string]ochazuke.Parameter{
		"next-link": {"steps": count}, "previous-link": {"steps": count},
		"move":       {"delta": {Type: "integer"}, "steps": count},
		"next-block": {"steps": count}, "previous-block": {"steps": count},
		"next-same": {"steps": count}, "previous-same": {"steps": count},
		"next-kind":     {"steps": count, "kind": {Type: "string", Required: true, Enum: kinds, Description: "h heading, t table, x checklist item, l list item, c code, q quote, n note, p paragraph"}},
		"previous-kind": {"steps": count, "kind": {Type: "string", Required: true, Enum: kinds, Description: "h heading, t table, x checklist item, l list item, c code, q quote, n note, p paragraph"}},
		"insert":        {"at": {Type: "string", Enum: []string{"cursor", "after", "start", "end"}}},
		"open":          {"where": {Type: "string", Enum: []string{"below", "above"}}},
		"paste":         {"where": {Type: "string", Enum: []string{"below", "above"}}},
		"cell":          {"delta": {Type: "integer"}},
	}
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
	if r.Settings() != nil {
		_ = r.DeclareSetting(ochazuke.Setting{Name: LinkStopsSetting, Owner: ochazuke.OwnerFramework, Type: "boolean", Default: true, Description: "Visit inline Markdown links as focus stops with j and k"})
		_ = r.DeclareSetting(ochazuke.Setting{Name: AuthorSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "owner", Description: "The author a new note in a Markdown document is signed with"})
		_ = r.DeclareSetting(ochazuke.Setting{Name: CursorSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "item", Enum: []string{"item", "line"}, Description: "Markdown cursor unit: whole elements, or individual drawn lines"})
		_ = r.DeclareSetting(ochazuke.Setting{Name: CursorWidthSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "pane", Enum: []string{"pane", "text"}, Description: "Markdown cursor highlight width: the whole pane, or only its text"})
		_ = r.DeclareSetting(ochazuke.Setting{Name: DoneSetting, Owner: ochazuke.OwnerFramework, Type: "string", Default: "show", Enum: []string{"show", "hide"}, Description: "Show checked Markdown tasks, or hide them with their nested subtrees"})
		_ = r.DeclareSetting(ochazuke.Setting{Name: DoneIndicatorSetting, Owner: ochazuke.OwnerFramework, Type: "boolean", Default: true, Description: "Show a dim count above a Markdown document when checked tasks are hidden"})
		for _, name := range []string{DoneSetting, DoneIndicatorSetting} {
			r.Settings().Observe(name, func(string, any) {
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
			})
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
