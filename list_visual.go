package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Visual row selection: V on a list, a table or a tree with marks starts a
// selection of rows anchored at the selected row. It is not a selection
// model of its own: it is the list's marks. While it lasts the marks are
// the marks V found plus every shown row between the anchor and the
// selected row, so j, k, counts, gg, G and the page motions extend it
// (they clamp instead of spilling), the * gutter draws it, and list.apply
// fans a verb out over it.
//
// The selection is a mode: V enters visual-line, the line-shaped selection
// mode (Runtime.SelectionShape; the editor's V is the same mode), and the
// list follows the mode (ModeChanged): entering a line-shaped mode on one
// of its rows starts the selection, leaving it there ends it, and a modal
// opened over it (a confirm, the palette, a chooser) holds it until the
// mode the modal restores resumes it. In visual-line a key reads the
// visual-line tables: the list's own (d, y, esc, V; it extends normal, so
// the motions and space still answer) and the row noun's, where an
// application binds single keys for its row verbs (> nest, p put). A key
// whose row runs an application verb of the row's noun runs it for every
// selected row through list.apply (runRow, visualApply), with one confirm
// for all of them when the verb has one; the selection then ends (the
// mode back to normal) or stays, as the visual.after-action setting says.
// esc (or V again) ends it without running anything. Ending puts the
// marks back to the ones V found; space during the selection toggles
// those. The visual.list setting's marks keeps the older selection in
// normal mode (no mode, the row noun's normal keys act on it).

// Visual reports whether a V selection is active on the list.
func (l *List) Visual() bool { return l.visual }

// StartVisual starts a visual row selection at the selected row; a list
// without rows or one added with NoMarks refuses.
func (l *List) StartVisual() error {
	if l.noMarks {
		return fmt.Errorf("list %s takes no marks", l.id)
	}
	selected, ok := l.Selected()
	if !ok {
		return errors.New("list has no selected row")
	}
	if !l.visual {
		l.visual, l.base = true, maps.Clone(l.marks)
		if l.base == nil {
			l.base = map[string]bool{}
		}
	}
	l.anchor = selected.ID
	l.syncVisual()
	return nil
}

// EndVisual ends a visual selection: the marks go back to the ones it
// started over.
func (l *List) EndVisual() {
	if !l.visual {
		return
	}
	l.visual, l.anchor = false, ""
	l.marks, l.base = l.base, nil
}

// ModeChanged follows the global mode (ModeObserver): a line-shaped
// selection mode entered with the focus on one of the list's rows starts
// the selection there; leaving it with the focus on the list ends it. A
// mode left for a modal opened over the selection holds it: the mode the
// modal restores on close resumes it, anchor and marks as they were.
func (l *List) ModeChanged(from, to string) {
	line := l.r.SelectionShape(to) == SelectionLine
	switch {
	case line && !l.visual && l.Focused():
		_ = l.StartVisual() // a list that cannot select (no rows, NoMarks) stays as it is
	case !line && l.visual && (l.Focused() || l.r.TopModal() == ""):
		l.EndVisual()
	}
}

// endSelection ends l's visual selection and the line mode it is held in:
// the mode goes back to normal while the focus is on the list, and a modal
// opened during the selection (a chooser a verb asked) closes onto normal
// rather than resuming it.
func (r *Runtime) endSelection(l *List) {
	l.EndVisual()
	normal := func(m ModeState) bool { return r.SelectionShape(m.Name) != SelectionLine }
	for _, id := range r.modals {
		if inst := r.Instances[id]; inst != nil && l.holds(inst.Payload.Return) && !normal(inst.returnMode) {
			inst.returnMode = ModeState{Name: "normal"}
		}
	}
	if l.Focused() && !normal(ModeState{Name: r.BaseMode()}) {
		_ = r.SetMode("normal")
	}
}

// afterVisualAction is what a verb run over l's visual selection leaves:
// the selection ended (visual.after-action end, the default, as in Vim)
// or kept over the rows still shown (keep).
func (r *Runtime) afterVisualAction(l *List) {
	if r.visualKeeps() {
		l.syncVisual()
		return
	}
	r.endSelection(l)
}

// visualAsMode reports whether V enters visual-line (visual.list mode, the
// default) rather than selecting in normal mode (marks).
func (r *Runtime) visualAsMode() bool {
	v, _ := r.settingText(VisualListSetting)
	return v != VisualListMarks
}

// visualKeeps reports whether visual.after-action is keep.
func (r *Runtime) visualKeeps() bool {
	v, _ := r.settingText(VisualAfterActionSetting)
	return v == VisualAfterKeep
}

// applicable is list.apply's availability: a row it would act on (the
// marked rows shown, else the selected row) whose noun has the verb and
// where the verb is available, judged as things are now, a visual
// selection still on.
func (l *List) applicable(verb string, args Arguments) bool {
	rows := l.markedShown()
	if len(rows) == 0 {
		if selected, ok := l.Selected(); ok {
			rows = []ListItem{selected}
		}
	}
	for _, row := range rows {
		q := Request{Verb: verb, Noun: row.Noun, Args: args}
		if _, ok := l.r.Operations[q.Key()]; ok && l.r.Available(q, row.ID, l.r.Mode) {
			return true
		}
	}
	return false
}

// syncVisual redraws the selection into the marks: the marks V found plus
// the shown rows between the anchor and the selection. An anchor the
// filter hides or SetItems removed moves to the selected row.
func (l *List) syncVisual() {
	if !l.visual || l.selected < 0 {
		return
	}
	at := -1
	for i, item := range l.items {
		if item.ID == l.anchor {
			at = i
			break
		}
	}
	if at < 0 {
		at, l.anchor = l.selected, l.items[l.selected].ID
	}
	marks := maps.Clone(l.base)
	for i := min(at, l.selected); i <= max(at, l.selected); i++ {
		marks[l.items[i].ID] = true
	}
	l.marks = marks
}

// apply is list.apply: the verb for every marked row shown (the selected
// row when none is), one request per row noun with those rows as its
// subjects (Request.Subjects) and args as its arguments: a verb that takes
// many (Operation.Many) runs once for all of them, any other once per row
// (Runtime.eachSubject). A verb with a confirm opens one confirm for every
// row first; its y runs list.apply again, confirmed.
func (l *List) apply(c Context, a Arguments) (any, error) {
	r := l.r
	rows := l.markedShown()
	if len(rows) == 0 {
		if selected, ok := l.Selected(); ok {
			rows = []ListItem{selected}
		}
	}
	verb := StringArg(a, "verb", "")
	var args Arguments
	switch v := a["args"].(type) {
	case Arguments:
		args = v
	case map[string]any:
		args = v
	}
	confirmed, _ := a["confirmed"].(bool)
	visual := l.visual
	if !confirmed {
		for _, row := range rows {
			if op, ok := r.Operations[Request{Verb: verb, Noun: row.Noun}.Key()]; ok && op.Confirm != "" {
				// The rows' own request: what the prompt is about.
				rq := Request{Verb: verb, Noun: row.Noun, Args: args}
				for _, other := range rows {
					if other.Noun == row.Noun {
						rq.Subjects = append(rq.Subjects, other.ID)
					}
				}
				// The verb's ConfirmFor, told every row's key, decides
				// what it asks (a delete that will only archive), ""
				// nothing.
				ask := op.Confirm
				if op.ConfirmFor != nil {
					if ask = op.ConfirmFor(r, r.contextOf(row.ID), rq, ask); ask == "" {
						continue
					}
				}
				// One row asks its own prompt; several ask the generic
				// count unless the store builds the prompt itself
				// (Store.ConfirmFor), which then says what all of them
				// cost, {{label}} naming the first row and counting the
				// rest.
				if n := r.Nouns[row.Noun]; len(rows) > 1 && (n == nil || n.Store == nil || n.Store.ConfirmFor == nil) {
					ask = fmt.Sprintf("Run %s on %d rows?", r.DisplayKey(op.Key()), len(rows))
				}
				q := Request{Verb: "apply", Noun: "ochazuke.list", Args: Arguments{"id": l.id, "verb": verb, "confirmed": true}}
				if args != nil {
					q.Args["args"] = args
				}
				// Its fields read the rows' request, never list.apply's
				// own arguments; y runs list.apply again, confirmed.
				return r.openConfirmAbout(r.contextOf(row.ID), rq, q, Operation{Noun: op.Noun, Verb: op.Verb, Confirm: ask})
			}
		}
	}
	// The selection holds while the rows' requests run, so a verb
	// available only over a selection is available; applying keeps them
	// from being replaced by list.apply again (visualApply). It ends, or
	// stays, after them (afterVisualAction).
	l.applying = true
	defer func() {
		l.applying = false
		if visual && l.visual {
			r.afterVisualAction(l)
		}
	}()
	// The rows by noun, in display order; each noun's request targets the
	// selected row when it is one of them (a chooser or a confirm names
	// it), else the noun's first row.
	var nouns []string
	byNoun := map[string][]string{}
	for _, row := range rows {
		if _, ok := byNoun[row.Noun]; !ok {
			nouns = append(nouns, row.Noun)
		}
		byNoun[row.Noun] = append(byNoun[row.Noun], row.ID)
	}
	selected, _ := l.Selected()
	applied, skipped := []string{}, []string{}
	var failure error
	for _, noun := range nouns {
		ids := byNoun[noun]
		q := Request{Verb: verb, Noun: noun, Target: ids[0], Args: maps.Clone(args), Confirmed: confirmed}
		if slices.Contains(ids, selected.ID) {
			q.Target = selected.ID
		}
		if len(ids) > 1 {
			q.Subjects = ids
		}
		if _, ok := r.Operations[q.Key()]; !ok {
			for _, id := range ids {
				skipped = append(skipped, id+": "+q.Key()+" is not registered")
			}
			continue
		}
		value, err := r.Execute(q)
		if report, ok := value.(map[string]any); ok && len(ids) > 1 && report["applied"] != nil {
			done, _ := report["applied"].([]string)
			left, _ := report["skipped"].([]string)
			applied, skipped = append(applied, done...), append(skipped, left...)
		} else if err != nil {
			for _, id := range ids {
				skipped = append(skipped, id+": "+err.Error())
			}
		} else {
			applied = append(applied, ids...)
		}
		if err != nil && failure == nil {
			failure = err
		}
	}
	return map[string]any{"verb": verb, "applied": applied, "skipped": skipped}, failure
}

// visualApply is list.apply standing in for q when q is the own verb of a
// row in a list's visual selection (an application verb of the row's
// noun, not a motion): the verb then acts on every selected row. focus is
// the instance execute resolved q to. Anything else (a motion, a framework
// verb: its noun is not the row's; a row outside a selection; a request
// list.apply itself runs) is not replaced.
func (r *Runtime) visualApply(q Request, op Operation, focus string) (Request, bool) {
	owner := r.rows[focus]
	if owner == "" || len(q.Subjects) > 0 {
		return q, false
	}
	l := r.List(owner)
	inst := r.Instances[focus]
	if l == nil || !l.visual || l.applying || inst == nil || inst.Noun != q.Noun || op.Kind != KindApplication || op.Motion {
		return q, false
	}
	args := Arguments{"id": l.id, "verb": q.Verb}
	if len(q.Args) > 0 {
		args["args"] = q.Args
	}
	if q.Confirmed {
		args["confirmed"] = true
	}
	return Request{Verb: "apply", Noun: "ochazuke.list", Target: focus, Args: args}, true
}

func registerListVisual(r *Runtime) {
	r.DefineGuard("list-visual", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.visual
	})
	r.DefineGuard("table-visual", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && len(l.columns) > 0 && !l.noMarks // any table with marks, cell-mode or not
	})
	// list-selects: V's guard, any list, table or tree that takes marks.
	r.DefineGuard("list-selects", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && !l.noMarks
	})
	// list-applies: list.apply's, a row it would act on takes the verb
	// (List.applicable), so d over rows without a delete passes the key on.
	r.DefineGuard("list-applies", func(r *Runtime, ev KeyEvent) bool {
		var l *List
		if id := StringArg(ev.Args, "id", ""); id != "" {
			l = r.List(id)
		} else {
			l = r.focusedList(ev.Focus)
		}
		if l == nil {
			return false
		}
		var args Arguments
		switch v := ev.Args["args"].(type) {
		case Arguments:
			args = v
		case map[string]any:
			args = v
		}
		return l.applicable(StringArg(ev.Args, "verb", ""), args)
	})
	r.Nouns["ochazuke.list"].Guards["apply"] = "list-applies"
	visual := func(c Context, a Arguments) (any, error) {
		var l *List
		if id := StringArg(a, "id", ""); id != "" {
			l = r.List(id)
		} else if c.InputID == "" {
			for _, id := range r.Ancestry(focusOf(c)) {
				if l = r.List(id); l != nil {
					break
				}
			}
		}
		if l == nil {
			return nil, errors.New("no focused list")
		}
		if _, ok := l.Selected(); !ok {
			return l.state(), nil
		}
		on, set := a["on"].(bool)
		if !set {
			on = !l.visual
		}
		if !on {
			r.endSelection(l)
			return l.state(), nil
		}
		if !l.visual {
			if err := l.StartVisual(); err != nil {
				return l.state(), err
			}
		}
		if mode := r.SelectionMode(SelectionLine); r.visualAsMode() && mode != "" && l.Focused() && r.BaseMode() != mode {
			if err := r.SetMode(mode); err != nil {
				return l.state(), err
			}
		}
		return l.state(), nil
	}
	// V and esc are bindings/list-modes.json: esc ends the selection
	// before the filter's esc (after it in the file) is asked, only while
	// one is active.
	r.Nouns["ochazuke.list"].Guards["visual"] = "list-selects"
	// esc's verb guard as well as its row guard: a row guarded off still
	// takes its key in the listings, so the row guard alone would hide an
	// outer esc's hint (a modal's esc modal.pop over a table) whenever no
	// selection is on. The row guard keeps it apart from the filter's esc
	// row (bound after this, unguarded), which would otherwise replace it.
	r.Nouns["ochazuke.list"].Guards["end-visual"] = "list-visual"
	r.Register(Operation{Verb: "end-visual", Noun: "ochazuke.list", Description: "End the visual row selection (esc), and visual-line with it: the marks go back to the ones it found", Parameters: map[string]Parameter{
		"id": {Type: "string", Description: "The list (default: the focused one)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		return visual(c, Arguments{"id": a["id"], "on": false})
	}})
	r.Register(Operation{Verb: "visual", Noun: "ochazuke.list", Description: "Start a visual row selection at the selected row (V, in visual-line), or end it (V again, esc): j/k extend it; a key then acts on every selected row", Parameters: map[string]Parameter{
		"id": {Type: "string", Description: "The list (default: the focused one)"},
		"on": {Type: "boolean", Description: "true starts, false ends; omit to toggle"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		return visual(c, a)
	}})
}
