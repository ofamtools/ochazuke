package ochazuke

import (
	"strings"
	"testing"
)

func classPage(t *testing.T) (*Runtime, *Model, *List) {
	t.Helper()
	r, m, a, _ := motionPage(t)
	r.Define("row").Classes(ClassDecl{Name: "merge", Key: "m", Title: "merge commit"}, ClassDecl{Name: "refs", Key: "r", Title: "decorated commit"}, ClassDecl{Name: "quiet"})
	items := a.Items()
	for _, i := range []int{2, 6, 9, 29} {
		items[i].Classes = []string{"merge"}
	}
	items[6].Classes = append(items[6].Classes, "refs")
	if err := a.SetItems(items); err != nil {
		t.Fatal(err)
	}
	return r, m, a
}

func TestListClassMotions(t *testing.T) {
	r, m, a := classPage(t)
	for _, step := range []struct{ keys, want string }{
		{"] m", "a3"}, {"] r", "a7"}, {"[ m", "a3"},
		{"3 ] m", "a30"}, {"] m", "a30"}, {"9 [ m", "a3"}, {"[ m", "a3"},
		{"G", "a40"}, {"] m", "a40"}, {"[ r", "a7"},
	} {
		motionKeys(t, r, m, step.keys)
		if r.Focus != step.want || a.selectedID() != step.want || r.Mode.Name != "normal" {
			t.Fatalf("%s: focus %s selection %s mode %s, want %s", step.keys, r.Focus, a.selectedID(), r.Mode.Name, step.want)
		}
	}
	if !strings.Contains(BindingHints(r, r.Focus), "list.next-class merge commit") {
		t.Fatal("class motion missing from hints")
	}
	if !r.Operations["ochazuke.list.next-class"].Motion || !r.Operations["ochazuke.list.previous-class"].Motion {
		t.Fatal("class verbs are not motions")
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}

func TestListClassMotionsFilterAndStatus(t *testing.T) {
	r, m, a := classPage(t)
	a.SetFilter("a1") // a1, a10..a19; hides the class rows a3, a7 and a30.
	motionKeys(t, r, m, "9 ] m")
	if r.Focus != "a10" {
		t.Fatalf("filtered count focus %s", r.Focus)
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	motionKeys(t, r, m, "] m")
	if r.Focus != "a10" || sink.LastInfo() != "No further row of class merge" || sink.LastError() != "" {
		t.Fatalf("focus %s, info %q error %q", r.Focus, sink.LastInfo(), sink.LastError())
	}
	a.SetFilter("nothing")
	motionKeys(t, r, m, "] m [ m")
	if a.Shown() != 0 || r.Focus != "a" {
		t.Fatalf("empty filtered list: %d %s", a.Shown(), r.Focus)
	}
}

func TestListClassMotionsPaletteOnlyAndTarget(t *testing.T) {
	r, _, a := classPage(t)
	items := a.Items()
	items[4].Classes = []string{"quiet"}
	if err := a.SetItems(items); err != nil {
		t.Fatal(err)
	}
	_ = r.SetFocus("b1")
	_, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "next-class", Args: Arguments{"id": "a", "class": "quiet"}})
	if err != nil || a.selectedID() != "a5" {
		t.Fatalf("palette-only class: %s, %v", a.selectedID(), err)
	}
	for _, row := range r.Noun("row").Bindings["normal"] {
		if row.Request.Args["class"] == "quiet" {
			t.Fatal("palette-only class generated keys")
		}
	}
}

func TestListClassBindingsUserOverrideAndHelp(t *testing.T) {
	r, m, _ := classPage(t)
	found := map[string]bool{}
	for _, row := range BindingHelp(r, r.Focus) {
		if row.Key == "] m" || row.Key == "[ m" {
			found[row.Key] = strings.Contains(row.Description, "merge commit")
		}
	}
	if !found["] m"] || !found["[ m"] {
		t.Fatalf("help missing class rows: %v", found)
	}
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, BindingsFile, `[{"noun":"row","mode":"normal","keys":"] m","request":{"noun":"ochazuke.list","verb":"next-class","args":{"class":"refs"}}},{"noun":"row","mode":"normal","keys":"[ m","request":null}]`)
	r.EnsureUserBindings()
	motionKeys(t, r, m, "] m")
	if r.Focus != "a7" {
		t.Fatalf("override focus %s", r.Focus)
	}
	for _, row := range BindingHelp(r, r.Focus) {
		if row.Key == "[ m" {
			t.Fatal("unbound class row remains in help")
		}
	}
	if err := r.ReloadUserBindings(); err != nil {
		t.Fatal(err)
	}
	motionKeys(t, r, m, "g g ] m")
	if r.Focus != "a7" {
		t.Fatalf("reload override focus %s", r.Focus)
	}
}

func TestListClassesCopyAndPatch(t *testing.T) {
	r, m, a := classPage(t)
	items := a.Items()
	items[2].Classes[0] = "changed"
	if a.Items()[2].Classes[0] != "merge" {
		t.Fatal("Items shares classes")
	}
	if err := a.Patch(ListPatch{Upsert: []ListItem{items[2]}}); err != nil {
		t.Fatal(err)
	}
	items[2].Classes[0] = "outside"
	motionKeys(t, r, m, "] m")
	if r.Focus != "a7" {
		t.Fatalf("patch class focus %s", r.Focus)
	}
	selected, _ := a.Selected()
	selected.Classes[0] = "outside"
	if a.Items()[6].Classes[0] != "merge" {
		t.Fatal("Selected shares classes")
	}
}

func TestListClassDeclarationsReplaceKeys(t *testing.T) {
	r, m, _ := classPage(t)
	r.Define("row").Classes(ClassDecl{Name: "merge", Key: "x"})
	if len(r.Noun("row").Classes) != 3 {
		t.Fatal("class replacement duplicated declaration")
	}
	for _, row := range BindingHelp(r, r.Focus) {
		if row.Key == "] m" || row.Key == "[ m" {
			t.Fatal("old class keys remain")
		}
	}
	motionKeys(t, r, m, "2 ] x")
	if r.Focus != "a7" {
		t.Fatalf("replacement count focus %s", r.Focus)
	}
	r.Define("row").Classes(ClassDecl{Name: "merge"})
	for _, row := range BindingHelp(r, r.Focus) {
		if row.Key == "] x" || row.Key == "[ x" {
			t.Fatal("palette-only replacement retains keys")
		}
	}
}

func TestListClassNoDeclarationsAddNoBindings(t *testing.T) {
	r, m, _, _ := motionPage(t)
	before := BindingHints(r, r.Focus)
	r.Define("row").Classes()
	if got := BindingHints(r, r.Focus); got != before {
		t.Fatalf("empty declaration changed hints: %s", got)
	}
	motionKeys(t, r, m, "] m")
	if r.Focus != "a1" {
		t.Fatalf("undeclared list moved: %s", r.Focus)
	}
	for _, row := range BindingHelp(r, r.Focus) {
		if strings.HasSuffix(row.Request, "-class") {
			t.Fatal("undeclared list has class binding")
		}
	}
}
