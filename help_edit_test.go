package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// The binding help page's edits as values, ported from the prototype's
// edits.test.cjs: what each edit writes to the user's file, what it takes
// out of and puts into the live tables, the diff, and the preview a walk
// gives of it. The runtime's writes and undo are in help_page_edit_test.go.

// bindingEditFixture is an editor over a small default layer (pane/normal: d a
// archive, enter attach, n new; vim/normal: v show) and a user file that
// already adds one row (pane/normal x pane.cut).
func bindingEditFixture() BindingEditor {
	row := func(noun, keys, action string) Binding {
		q, _ := actionRequest(action)
		return Binding{Noun: noun, Mode: "normal", Keys: keys, Request: q}
	}
	defaults := []Binding{row("pane", "d a", "pane.archive"), row("pane", "enter", "pane.attach"), row("pane", "n", "pane.new"), row("vim", "v", "vim.show")}
	user := BindingsDoc{Groups: []BindingGroup{{Noun: "pane", Mode: "normal", Rows: []Binding{row("pane", "x", "pane.cut")}}}}
	return BindingEditor{User: user, Defaults: defaults}
}

// planFileRows are a plan's file rows as "table key action" (unbind for an
// unbind row).
func planFileRows(p BindingPlan) []string {
	var out []string
	for _, g := range p.Doc.Groups {
		for _, b := range g.Rows {
			what := b.Request.Key()
			if b.Unbind {
				what = "unbind"
			}
			out = append(out, tableLabel(Binding{Noun: g.Noun, Instance: g.Instance, Mode: g.Mode})+" "+planKey(b)+" "+what)
		}
	}
	return out
}

func hasRows(t *testing.T, p BindingPlan, want ...string) {
	t.Helper()
	rows := planFileRows(p)
	for _, w := range want {
		if !slices.Contains(rows, w) {
			t.Errorf("%s: the file lacks %q: %q", p.Name, w, rows)
		}
	}
}

func TestBindingEditRebindAndAdd(t *testing.T) {
	e := bindingEditFixture()
	archive := e.Defaults[0]
	p := e.PlanRebind(archive, BindingRevision{Keys: "g a", SetKeys: true})
	hasRows(t, p, "pane/normal d a unbind", "pane/normal g a pane.archive", "pane/normal x pane.cut")
	if p.Name != "rebind pane.archive d a to g a" || !p.Changed || len(p.Drop) != 1 || planKey(p.Put[0]) != "g a" {
		t.Fatalf("rebind plan %+v", p)
	}
	// One identity is one row: a hint on a default replaces it in place,
	// with no unbind row.
	p = e.PlanRebind(e.Defaults[1], BindingRevision{Hint: "open it", SetHint: true})
	hasRows(t, p, "pane/normal enter pane.attach")
	if slices.Contains(planFileRows(p), "pane/normal enter unbind") || p.Doc.Groups[0].Rows[1].Hint != "open it" {
		t.Fatalf("same-identity edit wrote %q", planFileRows(p))
	}
	// Add: a first key for a verb, and a second key beside a default.
	p = e.PlanAdd(Binding{Noun: "pane", Mode: "normal"}, Request{Noun: "pane", Verb: "annotate"}, "b")
	hasRows(t, p, "pane/normal b pane.annotate")
	p = e.PlanAdd(Binding{Noun: "pane", Mode: "normal"}, archive.Request, " g  a ")
	hasRows(t, p, "pane/normal g a pane.archive")
	if slices.Contains(planFileRows(p), "pane/normal d a unbind") {
		t.Fatal("adding a key took the old one")
	}
}

func TestBindingEditRemoveResetRevise(t *testing.T) {
	e := bindingEditFixture()
	p := e.PlanRemove(e.Defaults[1])
	hasRows(t, p, "pane/normal enter unbind")
	if p.How != "unbound" {
		t.Fatalf("removing a default: %q", p.How)
	}
	p = e.PlanRemove(e.User.Groups[0].Rows[0])
	if p.How != "deleted" || len(p.Doc.Groups) != 0 {
		t.Fatalf("removing the user's own row: %q %q", p.How, planFileRows(p))
	}
	// Reset after a rebind: the unbind row goes, the default comes back.
	rebound := e.PlanRebind(e.Defaults[0], BindingRevision{Keys: "g a", SetKeys: true})
	e2 := e
	e2.User = rebound.Doc
	p = e2.PlanReset(e.Defaults[0])
	if slices.Contains(planFileRows(p), "pane/normal d a unbind") || len(p.Put) != 1 || p.Put[0].Request.Key() != "pane.archive" || p.How != "restored" {
		t.Fatalf("reset: %q put %+v", planFileRows(p), p.Put)
	}
	// Revise: arguments, guard and hidden; a new guard is a new identity,
	// so the default under the old one is unbound.
	p = e.PlanRebind(e.Defaults[2], BindingRevision{Args: Arguments{"at": "top"}, SetArgs: true, When: "has-project", SetWhen: true, Hidden: true, SetHidden: true})
	hasRows(t, p, "pane/normal n unbind", "pane/normal n pane.new")
	put := p.Put[0]
	if put.Request.Args["at"] != "top" || put.When != "has-project" || !put.Hidden {
		t.Fatalf("revised row %+v", put)
	}
	// Moving to another table and to the any-mode table.
	p = e.PlanRebind(e.Defaults[2], BindingRevision{Noun: "app", SetTable: true, Mode: "", SetMode: true})
	hasRows(t, p, "pane/normal n unbind", "app n pane.new")
}

func TestBindingEditUnifyAndDiff(t *testing.T) {
	e := bindingEditFixture()
	rows := []Binding{{Noun: "pane", Mode: "normal", Request: Request{Noun: "pane", Verb: "show"}}, e.Defaults[3]}
	p := e.PlanUnify("show", "s", rows, false)
	hasRows(t, p, "pane/normal s pane.show", "vim/normal s vim.show")
	if slices.Contains(planFileRows(p), "vim/normal v unbind") || p.Name != "add s to show on 2 nouns" {
		t.Fatalf("adding moved a key: %q (%s)", planFileRows(p), p.Name)
	}
	p = e.PlanUnify("show", "s", rows, true)
	hasRows(t, p, "vim/normal v unbind", "vim/normal s vim.show")
	// The diff: one rebind (an unbind and a row added), a changed row and
	// a removed one.
	step := e.PlanRebind(e.Defaults[0], BindingRevision{Keys: "g a", SetKeys: true})
	e.User = step.Doc
	step = e.PlanRebind(e.User.Groups[0].Rows[0], BindingRevision{Hint: "cut it", SetHint: true})
	e.User = step.Doc
	start := bindingEditFixture().User
	d := DiffBindings(start, step.Doc)
	if len(d.Added) != 2 || len(d.Changed) != 1 || len(d.Removed) != 0 {
		t.Fatalf("diff %+v", d)
	}
	fresh := bindingEditFixture()
	d = DiffBindings(start, fresh.PlanRemove(fresh.User.Groups[0].Rows[0]).Doc)
	if len(d.Removed) != 1 || !strings.HasPrefix(d.Lines()[0], `- pane/normal  {"key": "x"`) {
		t.Fatalf("diff after removing %+v", d.Lines())
	}
	if touched := DiffBindings(start, step.Doc).Touched(nil); !touched[sourceKey(Binding{Noun: "pane", Mode: "normal", Keys: "g a"})] {
		t.Fatalf("touched %v", touched)
	}
}

// previewWalk is a walk by hand: tile (x cut, enter open), panel (x clear,
// y copy), app (q quit).
func previewWalk() HelpWalk {
	row := func(noun, keys, action string) Binding {
		q, _ := actionRequest(action)
		return Binding{Noun: noun, Mode: "normal", Keys: keys, Request: q}
	}
	level := func(i int, noun string, rows ...Binding) HelpLevel {
		return HelpLevel{Index: i, Noun: noun, Tables: []WalkTable{{Kind: TableMode, Noun: noun, Mode: "normal", Rows: rows}}}
	}
	w := HelpWalk{Place: NounPlace{Noun: "tile"}, Mode: "normal", Levels: []HelpLevel{
		level(0, "tile", row("tile", "x", "tile.cut"), row("tile", "enter", "tile.open")),
		level(1, "panel", row("panel", "x", "panel.clear"), row("panel", "y", "panel.copy")),
		level(2, "app", row("app", "q", "app.quit")),
	}, Declared: map[string]bool{}}
	w.collect(func(b Binding) string { return b.When })
	return w
}

func TestBindingEditPreview(t *testing.T) {
	w := previewWalk()
	e := BindingEditor{Defaults: w.rowsOf()}
	open := w.Levels[0].Tables[0].Rows[1]
	text := func(p BindingPreview) string { return strings.Join(p.Texts(), "\n") }
	// A free key on a nearer level newly shadows the outer row, which
	// keeps no key.
	pv := w.Preview(e.PlanRebind(open, BindingRevision{Keys: "y", SetKeys: true}), nil, nil)
	for _, want := range []string{"y runs now: panel.copy (at panel)", "After this change, y runs: tile.open (at tile)", "Shadows: panel.copy (y) at panel", "panel.copy keeps y (none answers from this focus)"} {
		if !strings.Contains(text(pv), want) {
			t.Errorf("preview lacks %q:\n%s", want, text(pv))
		}
	}
	if !slices.Equal(pv.Orphans, []string{"panel.copy"}) {
		t.Errorf("orphans %q", pv.Orphans)
	}
	// A key on the same table replaces its row; a row it shadowed already
	// is said to be shadowed before too.
	pv = w.Preview(e.PlanRebind(open, BindingRevision{Keys: "x", SetKeys: true}), nil, nil)
	for _, want := range []string{"Replaces tile.cut on x in tile/normal", "tile.cut keeps no key on this walk", "panel.clear (x) at panel (it was shadowed before too)"} {
		if !strings.Contains(text(pv), want) {
			t.Errorf("preview lacks %q:\n%s", want, text(pv))
		}
	}
	// A nearer row takes the key first: the edit is kept, and said so.
	pv = w.Preview(e.PlanAdd(Binding{Noun: "panel", Mode: "normal"}, Request{Noun: "panel", Verb: "copy"}, "x"), nil, nil)
	if !strings.Contains(text(pv), "A nearer row answers x first: tile.cut at tile") {
		t.Errorf("preview:\n%s", text(pv))
	}
	// Off the walk.
	pv = w.Preview(e.PlanAdd(Binding{Noun: "elsewhere", Mode: "normal"}, Request{Noun: "elsewhere", Verb: "go"}, "z"), nil, nil)
	if !strings.Contains(text(pv), "not on this walk") {
		t.Errorf("preview:\n%s", text(pv))
	}
	// The walk after: the new row answers and the outer row is shadowed by
	// it; the trace goes through it.
	after := w.With(nil, []Binding{{Noun: "tile", Mode: "normal", Keys: "y", Request: open.Request}}, nil)
	mine := slices.IndexFunc(after.Entries, func(en HelpEntry) bool { return en.Row.Keys == "y" && en.Row.Noun == "tile" })
	outer := slices.IndexFunc(after.Entries, func(en HelpEntry) bool { return en.Row.Keys == "y" && en.Row.Noun == "panel" })
	if after.Entries[mine].State != StatusAnswers || after.Entries[outer].By != mine || after.Trace("y").Answer.Request.Key() != "tile.open" {
		t.Fatalf("walk after: %+v", after.Entries)
	}
}

// rowsOf are every row of the walk's tables (the preview fixture's
// default layer).
func (w HelpWalk) rowsOf() []Binding {
	var out []Binding
	for _, l := range w.Levels {
		for _, t := range l.Tables {
			out = append(out, t.Rows...)
		}
	}
	return out
}
