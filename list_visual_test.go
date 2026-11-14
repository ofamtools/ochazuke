package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// visualList is motionPage's list a (40 rows of noun row) whose rows take
// a delete (removes the row from a), a many-subject nest guarded to a
// selection (the o-json shape: available only while V is on) bound to >
// in visual-line, and a per-row zap bound to z in normal only.
func visualList(t *testing.T) (*Runtime, *Model, *Collection, *[]string) {
	t.Helper()
	r, m, a, _ := motionPage(t)
	var calls []string
	r.DefineGuard("row-selecting", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.Visual()
	})
	r.Define("row").
		Verb("delete", func(c Ctx) Result {
			return c.Each(func(one Ctx) Result {
				calls = append(calls, "delete "+one.Instance.ID)
				items := slices.DeleteFunc(a.Items(), func(it ListItem) bool { return it.ID == one.Instance.ID })
				a.SetItems(items)
				return Done(nil)
			})
		}, "delete rows").Many().
		Verb("nest", func(c Ctx) Result {
			calls = append(calls, "nest "+strings.Join(c.SubjectIDs(), ","))
			return Done(nil)
		}, "nest the selection").Many().When("row-selecting").
		Verb("zap", func(c Ctx) Result { calls = append(calls, "zap "+c.Instance.ID); return Done(nil) }, "zap a row").
		Mode("visual-line").Bind(">", Do("row.nest")).
		Mode("normal").Bind("Z", Do("row.zap"))
	r.EnsureConfigSettings()
	return r, m, a, &calls
}

// V enters visual-line, the line-shaped selection mode; esc leaves it and
// the marks go back to the ones V found.
func TestVisualIsAModeWithALineShape(t *testing.T) {
	r, m, a, _ := visualList(t)
	if got := r.SelectionShape("visual-line"); got != SelectionLine {
		t.Fatalf("visual-line shape %q", got)
	}
	if r.SelectionShape("visual") != SelectionChar || r.SelectionShape("visual-block") != SelectionBlock || r.SelectionShape("normal") != "" {
		t.Fatalf("shapes: %q %q %q", r.SelectionShape("visual"), r.SelectionShape("visual-block"), r.SelectionShape("normal"))
	}
	if r.SelectionMode(SelectionLine) != "visual-line" {
		t.Fatalf("line mode %q", r.SelectionMode(SelectionLine))
	}
	motionKeys(t, r, m, "space j V")
	if r.Mode.Name != "visual-line" || !a.Visual() {
		t.Fatalf("V: mode %s visual %v", r.Mode.Name, a.Visual())
	}
	motionKeys(t, r, m, "esc")
	if r.Mode.Name != "normal" || a.Visual() || strings.Join(ids(a.Marked()), " ") != "a1" {
		t.Fatalf("esc: mode %s visual %v marks %v", r.Mode.Name, a.Visual(), ids(a.Marked()))
	}
	// V again leaves it too; mode.switch visual-line enters it from anywhere
	// (the palette, the API): the list follows the mode.
	motionKeys(t, r, m, "V V")
	if r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("V V: mode %s visual %v", r.Mode.Name, a.Visual())
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.mode", Verb: "switch", Args: Arguments{"mode": "visual-line"}}); err != nil {
		t.Fatal(err)
	}
	if !a.Visual() {
		t.Fatal("mode.switch visual-line did not start the selection")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.mode", Verb: "switch", Args: Arguments{"mode": "normal"}}); err != nil {
		t.Fatal(err)
	}
	if a.Visual() {
		t.Fatal("mode.switch normal did not end the selection")
	}
}

// Motions extend the selection: j, a count, G and k; a focus moved out of
// the list ends it.
func TestVisualMotionsExtendTheSelection(t *testing.T) {
	r, m, a, _ := visualList(t)
	motionKeys(t, r, m, "V j")
	if strings.Join(ids(a.Marked()), " ") != "a1 a2" {
		t.Fatalf("V j: %v", ids(a.Marked()))
	}
	motionKeys(t, r, m, "3 j")
	if r.Mode.Name != "visual-line" || len(a.Marked()) != 5 || r.Focus != "a5" {
		t.Fatalf("3 j: mode %s marks %v focus %s", r.Mode.Name, ids(a.Marked()), r.Focus)
	}
	motionKeys(t, r, m, "k")
	if len(a.Marked()) != 4 {
		t.Fatalf("k: %v", ids(a.Marked()))
	}
	motionKeys(t, r, m, "G")
	if len(a.Marked()) != 40 || r.Mode.Name != "visual-line" {
		t.Fatalf("G: %d marks mode %s", len(a.Marked()), r.Mode.Name)
	}
	if err := r.SetFocus("b1"); err != nil {
		t.Fatal(err)
	}
	if a.Visual() || len(a.Marked()) != 0 || r.Mode.Name != "normal" {
		t.Fatalf("focus out: visual %v marks %v mode %s", a.Visual(), ids(a.Marked()), r.Mode.Name)
	}
}

// V j d deletes the two rows in one step (the list's d runs the rows'
// delete through collection.apply), and visual ends; V j y yanks the two.
func TestVisualLineDeleteAndYank(t *testing.T) {
	r, m, a, calls := visualList(t)
	motionKeys(t, r, m, "V j d")
	if strings.Join(*calls, ", ") != "delete a1, delete a2" || a.Len() != 38 {
		t.Fatalf("V j d: %v, %d rows", *calls, a.Len())
	}
	if r.Mode.Name != "normal" || a.Visual() || len(a.Marked()) != 0 {
		t.Fatalf("after d: mode %s visual %v marks %v", r.Mode.Name, a.Visual(), ids(a.Marked()))
	}
	motionKeys(t, r, m, "V j y")
	y, _ := r.ReadYank("")
	if y.Text != "a3\na4\n" || y.Shape != "line" || r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("V j y: %+v mode %s visual %v", y, r.Mode.Name, a.Visual())
	}
}

// A verb available only over a selection (guard row-selecting) answers in
// visual-line and runs once for every selected row: the selection is
// still on while its availability is read (the ordering o-json hit).
func TestVisualSelectionOnlyVerbIsAvailable(t *testing.T) {
	r, m, a, calls := visualList(t)
	motionKeys(t, r, m, "j V j j >")
	if strings.Join(*calls, ", ") != "nest a2,a3,a4" {
		t.Fatalf("V j j >: %v", *calls)
	}
	if r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("after >: mode %s visual %v", r.Mode.Name, a.Visual())
	}
	// collection.apply itself reads the selection's availability: over a
	// selection the guarded verb applies, without one it is refused.
	motionKeys(t, r, m, "V j")
	if _, err := r.Execute(Request{Noun: "ochazuke.collection", Verb: "apply", Args: Arguments{"id": "a", "verb": "nest"}}); err != nil {
		t.Fatalf("collection.apply nest over a selection: %v", err)
	}
	// Without one the key stands aside; an explicit request runs (D3).
	if r.Available(Request{Noun: "ochazuke.collection", Verb: "apply", Args: Arguments{"id": "a", "verb": "nest"}}, r.Focus, r.Mode) {
		t.Fatal("collection.apply nest without a selection is available")
	}
}

// d over rows whose noun has no delete is not available, so the key goes
// on out instead of reporting every row skipped.
func TestVisualDeleteUnavailableWithoutTheVerb(t *testing.T) {
	r, m, a, _ := motionPage(t)
	motionKeys(t, r, m, "V j")
	if ok, _ := r.Dispatch(MacroKey{Key: "d"}); ok {
		t.Fatal("d answered over rows without a delete")
	}
	if !a.Visual() || r.Mode.Name != "visual-line" {
		t.Fatalf("d changed the selection: visual %v mode %s", a.Visual(), r.Mode.Name)
	}
}

// visual.after-action keep keeps the selection (and visual-line) after a
// key acted on it, over the rows still shown; end, the default, ends it.
func TestVisualAfterActionSetting(t *testing.T) {
	r, m, a, calls := visualList(t)
	if err := r.Settings().Set(VisualAfterActionSetting, VisualAfterKeep); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "V j >")
	if r.Mode.Name != "visual-line" || !a.Visual() || strings.Join(ids(a.Marked()), " ") != "a1 a2" {
		t.Fatalf("keep: mode %s visual %v marks %v", r.Mode.Name, a.Visual(), ids(a.Marked()))
	}
	motionKeys(t, r, m, "> y")
	if strings.Join(*calls, ", ") != "nest a1,a2, nest a1,a2" || !a.Visual() {
		t.Fatalf("keep: a second key acts on the same rows: %v visual %v", *calls, a.Visual())
	}
	motionKeys(t, r, m, "d")
	if a.Len() != 38 || !a.Visual() || r.Mode.Name != "visual-line" || len(a.Marked()) != 1 {
		t.Fatalf("keep after d: %d rows visual %v mode %s marks %v", a.Len(), a.Visual(), r.Mode.Name, ids(a.Marked()))
	}
	motionKeys(t, r, m, "esc")
	if err := r.Settings().Set(VisualAfterActionSetting, VisualAfterEnd); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "V j >")
	if r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("end: mode %s visual %v", r.Mode.Name, a.Visual())
	}
}

// A confirm opened over the selection holds it: n returns to visual-line
// with the same rows, y runs on them and ends it.
func TestVisualConfirmHoldsTheSelection(t *testing.T) {
	r, m, a, calls := visualList(t)
	if err := r.SetConfirm("row.delete", "Delete {{label}}?"); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "V j d")
	if r.TopModal() == "" || len(*calls) != 0 {
		t.Fatalf("d: modal %q calls %v", r.TopModal(), *calls)
	}
	motionKeys(t, r, m, "n")
	if r.TopModal() != "" || r.Mode.Name != "visual-line" || strings.Join(ids(a.Marked()), " ") != "a1 a2" {
		t.Fatalf("n: modal %q mode %s marks %v", r.TopModal(), r.Mode.Name, ids(a.Marked()))
	}
	motionKeys(t, r, m, "d y")
	if strings.Join(*calls, ", ") != "delete a1, delete a2" || r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("d y: %v mode %s visual %v", *calls, r.Mode.Name, a.Visual())
	}
}

// visual.list marks keeps the older selection: V selects in normal mode
// and the rows' normal keys act on it.
func TestVisualListMarksSetting(t *testing.T) {
	r, m, a, calls := visualList(t)
	if err := r.Settings().Set(VisualListSetting, VisualListMarks); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "V j")
	if r.Mode.Name != "normal" || !a.Visual() {
		t.Fatalf("marks: mode %s visual %v", r.Mode.Name, a.Visual())
	}
	motionKeys(t, r, m, "Z")
	if strings.Join(*calls, ", ") != "zap a1, zap a2" || a.Visual() {
		t.Fatalf("marks z: %v visual %v", *calls, a.Visual())
	}
}

// A mode's selection shape is data: a bindings file's "modes" declares
// it, the export writes it back, and an input's visual modes read it.
func TestSelectionShapeIsAModeProperty(t *testing.T) {
	r := NewRuntime()
	if err := r.LoadDefaultBindings("test", []byte(`{"modes": {"pick-rows": {"selection": "line"}}, "bindings": []}`)); err != nil {
		t.Fatal(err)
	}
	if r.SelectionShape("pick-rows") != SelectionLine {
		t.Fatalf("pick-rows %q", r.SelectionShape("pick-rows"))
	}
	if doc := r.BindingsDoc(); doc.Modes["pick-rows"].Selection != SelectionLine || doc.Modes["visual-block"].Selection != SelectionBlock {
		t.Fatalf("export: %+v", doc.Modes)
	}
	if err := r.DefineSelection("nope", SelectionLine); err == nil {
		t.Fatal("an unknown mode took a shape")
	}
	in := NewInput("x", "ab", false)
	in.Mode = "visual-line"
	if in.shape() != SelectionLine {
		t.Fatalf("an input with no runtime: %q", in.shape())
	}
}

// The palette opens over a selection (: in visual-line) and holds it: a
// row verb it runs acts on every selected row.
func TestVisualPaletteActsOnTheSelection(t *testing.T) {
	r, m, a, calls := visualList(t)
	motionKeys(t, r, m, "V j :")
	if r.TopModal() == "" || !a.Visual() {
		t.Fatalf(": modal %q visual %v", r.TopModal(), a.Visual())
	}
	if _, err := r.Dispatch(MacroKey{Key: "[zap]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "enter")
	if strings.Join(*calls, ", ") != "zap a1, zap a2" || a.Visual() || r.Mode.Name != "normal" {
		t.Fatalf("palette zap: %v visual %v mode %s modal %q", *calls, a.Visual(), r.Mode.Name, r.TopModal())
	}
}

// A mode that extends another (ModeExtends, a bindings file's "modes"
// extends) falls back to its rows for every noun with no table of its own
// there: o-json's query mode binds its own keys once and the nouns keep
// their normal rows. A noun's own table (rows, an empty group, or its own
// extends) replaces the fallback for that noun.
func TestModeExtendsFallsBackForNounsWithoutATable(t *testing.T) {
	r, m, _, calls := visualList(t)
	if err := r.DefineMode("query", false, ModeExtends("normal")); err != nil {
		t.Fatal(err)
	}
	if r.ModeParent("query") != "normal" || r.BindingsDoc().Modes["query"].Extends != "normal" {
		t.Fatalf("query parent %q", r.ModeParent("query"))
	}
	if err := r.DefineMode("bad", false, ModeExtends("nope")); err == nil {
		t.Fatal("a mode extended an unknown mode")
	}
	if err := r.SetMode("query"); err != nil {
		t.Fatal(err)
	}
	// row and the list bind nothing in query: their normal rows answer.
	motionKeys(t, r, m, "j Z")
	if r.Focus != "a2" || strings.Join(*calls, ", ") != "zap a2" || r.Mode.Name != "query" {
		t.Fatalf("query: focus %s calls %v mode %s", r.Focus, *calls, r.Mode.Name)
	}
	// A table of its own stops the fallback for that noun only.
	r.Define("row").Mode("query").Bind("x", Do("row.zap"))
	if ok, _ := r.Dispatch(MacroKey{Key: "Z"}); ok {
		t.Fatal("Z answered: row has a query table of its own")
	}
	motionKeys(t, r, m, "x j")
	if strings.Join(*calls, ", ") != "zap a2, zap a2" || r.Focus != "a3" {
		t.Fatalf("own query table: calls %v focus %s", *calls, r.Focus)
	}
	// visual-line extends normal: row has a visual-line table of its own
	// (> nest), so its normal z does not answer there; a noun without one
	// keeps its normal keys, which then act on the selection.
	_ = r.SetMode("normal")
	motionKeys(t, r, m, "V j")
	if ok, _ := r.Dispatch(MacroKey{Key: "Z"}); ok || r.ModeParent("visual-line") != "normal" {
		t.Fatal("Z answered in visual-line over a noun with its own table")
	}
	r2, m2, a2, _ := motionPage(t)
	var zapped []string
	r2.Define("row").Verb("zap", func(c Ctx) Result { zapped = append(zapped, c.Instance.ID); return Done(nil) }, "zap").
		Mode("normal").Bind("Z", Do("row.zap"))
	motionKeys(t, r2, m2, "V j Z")
	if strings.Join(zapped, " ") != "a1 a2" || a2.Visual() || r2.Mode.Name != "normal" {
		t.Fatalf("normal z in visual-line: %v visual %v mode %s", zapped, a2.Visual(), r2.Mode.Name)
	}
}

// G16: a visual selection fans a standard verb out only when its row
// takes a subject and many of them (standardVerb.fansOut). create, list
// and get over two selected rows run once, on the focus, and answer as
// themselves; set (Many) still writes every selected row.
func TestVisualFanOutReadsTheVerbRow(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	counts := map[string]int{}
	things := []Thing{}
	var items []ListItem
	for i := 1; i <= 4; i++ {
		id := "a" + string(rune('0'+i))
		things = append(things, Thing{"id": id, "title": id, "status": "todo"})
		items = append(items, ListItem{ID: id, Label: id, Noun: "row", Data: map[string]any{"id": id}})
	}
	find := func(id string) Thing {
		for _, t := range things {
			if t["id"] == id {
				return t
			}
		}
		return nil
	}
	r.Define("row").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}, PropSpec{Name: "status"}).
		Store(Store{
			Get:  func(_ Ctx, id string) (Thing, error) { counts["get"]++; return find(id), nil },
			List: func(Ctx, ListQuery) ([]Thing, error) { counts["list"]++; return things, nil },
			Create: func(_ Ctx, v Thing) (Thing, error) {
				counts["create"]++
				t := Thing{"id": fmt.Sprintf("n%d", counts["create"]), "title": v["title"]}
				things = append(things, t)
				return t, nil
			},
			Patch: func(_ Ctx, id string, c Thing) (Thing, error) {
				counts["set"]++
				maps.Copy(find(id), c)
				return nil, nil
			},
		}).
		Mode("visual-line").
		Bind("&", Do("row.create")).
		Bind("%", Do("row.get")).
		Bind("~", Do("row.list")).
		Bind("!", Binding{Request: Request{Noun: "row", Verb: "set", Args: Arguments{"prop": "status", "value": "done"}}})
	if _, err := r.AddPage("p", "P", func(_ *Runtime, w, h int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	a, err := r.AddList("a", items, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	if err := a.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 40, 24
	r.EnsureConfigSettings()
	applies := func() int {
		n := 0
		for _, e := range slices.Concat(r.history.entries, r.history.fetches) {
			if e.Request.Key() == "ochazuke.collection.apply" {
				n++
			}
		}
		return n
	}
	for _, c := range []struct{ key, verb string }{{"&", "create"}, {"%", "get"}, {"~", "list"}} {
		clear(counts)
		before := applies()
		motionKeys(t, r, m, "g g V j "+c.key)
		if counts[c.verb] != 1 {
			t.Fatalf("%s over two selected rows ran %d times, want once: %v", c.verb, counts[c.verb], counts)
		}
		if applies() != before {
			t.Fatalf("%s went through collection.apply", c.verb)
		}
		if a.Visual() {
			motionKeys(t, r, m, "esc")
		}
	}
	clear(counts)
	motionKeys(t, r, m, "g g V j !")
	if counts["set"] != 2 || find("a1")["status"] != "done" || find("a2")["status"] != "done" {
		t.Fatalf("set over two selected rows: %v, a1 %v a2 %v", counts, find("a1")["status"], find("a2")["status"])
	}
}
