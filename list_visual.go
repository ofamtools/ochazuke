package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Visual row selection: V on a cell-mode table starts a selection of rows
// anchored at the selected row. It is not a selection model of its own: it
// is the list's marks. While it lasts the marks are the marks V found plus
// every shown row between the anchor and the selected row, so j, k, counts,
// gg, G and the page motions extend it (they clamp instead of spilling),
// the * gutter draws it, and list.apply fans a verb out over it. The mode
// stays normal, so the rows' own keys still answer and the footer keeps
// them: a key whose row runs an application verb of the row's noun
// (x row.delete) runs it for every selected row through list.apply
// (runRow, visualApply), with one confirm for all of them when the verb has
// one. Running it ends the selection; esc (or V again) ends it without
// running anything. Either way the marks go back to the ones V found;
// space during the selection toggles those.

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
	if !confirmed {
		for _, row := range rows {
			if op, ok := r.Operations[Request{Verb: verb, Noun: row.Noun}.Key()]; ok && op.Confirm != "" {
				q := Request{Verb: "apply", Noun: "ochazuke.list", Args: Arguments{"id": l.id, "verb": verb, "confirmed": true}}
				if args != nil {
					q.Args["args"] = args
				}
				prompt := fmt.Sprintf("Run %s on %d rows?", r.DisplayKey(op.Key()), len(rows))
				if len(rows) == 1 {
					prompt = op.Confirm
				}
				return r.openConfirm(r.contextOf(row.ID), q, Operation{Noun: op.Noun, Verb: op.Verb, Confirm: prompt})
			}
		}
	}
	l.EndVisual()
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
// verb, a row outside a selection) is not replaced.
func (r *Runtime) visualApply(q Request, op Operation, focus string) (Request, bool) {
	owner := r.rows[focus]
	if owner == "" || q.Noun == "ochazuke.list" || len(q.Subjects) > 0 {
		return q, false
	}
	l := r.List(owner)
	inst := r.Instances[focus]
	if l == nil || !l.visual || inst == nil || inst.Noun != q.Noun || op.Kind != KindApplication || op.Motion {
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
		on, set := a["on"].(bool)
		if !set {
			on = !l.visual
		}
		if !on {
			l.EndVisual()
			return l.state(), nil
		}
		return l.state(), l.StartVisual()
	}
	// V and esc are bindings/list-modes.json: esc ends the selection
	// before the filter's esc (after it in the file) is asked, only while
	// one is active.
	r.Nouns["ochazuke.list"].Guards["visual"] = "table-visual"
	// esc's verb guard as well as its row guard: a row guarded off still
	// takes its key in the listings, so the row guard alone would hide an
	// outer esc's hint (a modal's esc modal.pop over a table) whenever no
	// selection is on. The row guard keeps it apart from the filter's esc
	// row (bound after this, unguarded), which would otherwise replace it.
	r.Nouns["ochazuke.list"].Guards["end-visual"] = "list-visual"
	r.Register(Operation{Verb: "end-visual", Noun: "ochazuke.list", Description: "End the visual row selection (esc): the marks go back to the ones it found", Parameters: map[string]Parameter{
		"id": {Type: "string", Description: "The list (default: the focused one)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		return visual(c, Arguments{"id": a["id"], "on": false})
	}})
	r.Register(Operation{Verb: "visual", Noun: "ochazuke.list", Description: "Start a visual row selection at the selected row (V), or end it (V again, esc): j/k extend it over the marks; a row's verb then runs for every selected row", Parameters: map[string]Parameter{
		"id": {Type: "string", Description: "The list (default: the focused one)"},
		"on": {Type: "boolean", Description: "true starts, false ends; omit to toggle"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		return visual(c, a)
	}})
}
