package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// visualList is motionPage's list a (40 rows of noun row) whose rows take
// a delete (removes the row from a), a many-subject nest guarded to a
// selection (the o-json shape: available only while V is on) bound to >
// in visual-line, and a per-row zap bound to z in normal only.
func visualList(t *testing.T) (*Runtime, *Model, *List, *[]string) {
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
		Mode("normal").Bind("z", Do("row.zap"))
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
	// V again leaves it too; mode.set visual-line enters it from anywhere
	// (the palette, the API): the list follows the mode.
	motionKeys(t, r, m, "V V")
	if r.Mode.Name != "normal" || a.Visual() {
		t.Fatalf("V V: mode %s visual %v", r.Mode.Name, a.Visual())
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.mode", Verb: "set", Args: Arguments{"mode": "visual-line"}}); err != nil {
		t.Fatal(err)
	}
	if !a.Visual() {
		t.Fatal("mode.set visual-line did not start the selection")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.mode", Verb: "set", Args: Arguments{"mode": "normal"}}); err != nil {
		t.Fatal(err)
	}
	if a.Visual() {
		t.Fatal("mode.set normal did not end the selection")
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
// delete through list.apply), and visual ends; V j y yanks the two.
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
	// list.apply itself reads the selection's availability: over a
	// selection the guarded verb applies, without one it is refused.
	motionKeys(t, r, m, "V j")
	if _, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "apply", Args: Arguments{"id": "a", "verb": "nest"}}); err != nil {
		t.Fatalf("list.apply nest over a selection: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "apply", Args: Arguments{"id": "a", "verb": "nest"}}); err == nil {
		t.Fatal("list.apply nest without a selection was not refused")
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
	motionKeys(t, r, m, "z")
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
