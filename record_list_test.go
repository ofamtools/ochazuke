package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// formListFixture is a project noun whose directories are a list of paths and
// whose note is a field below it, with the form placed (EditSpec) and the
// focus on the directories field.
func formListFixture(t *testing.T, dirs ...string) (*Runtime, *derivedStore, *RecordEditor, *Model) {
	t.Helper()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{{"id": "p1", "name": "Alpha", "directories": slices.Clone(dirs), "note": "n"}}}
	st := s.store()
	st.Undo = true
	r.Define("project").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "name", Role: RoleLabel, Parameter: Parameter{Required: true}},
		PropSpec{Name: "dirs", Aliases: nil, Parameter: Parameter{Type: SettingString}},
		PropSpec{Name: "directories", Parameter: Parameter{Type: SettingList, Items: &Parameter{Type: PathParameterType, Path: "dir", Description: "directory"}}},
		PropSpec{Name: "note"},
	).Store(st)
	spec, err := r.EditSpec("project", "p1")
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.AddRecordEditor("form", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(e.field("directories").id); err != nil {
		t.Fatal(err)
	}
	return r, s, e, NewModel(r)
}

func (s *derivedStore) dirs(t *testing.T) []string {
	t.Helper()
	switch v := s.find("p1")["directories"].(type) {
	case []string:
		return v
	case []any:
		var out []string
		for _, x := range v {
			out = append(out, x.(string))
		}
		return out
	case nil:
		return nil
	default:
		t.Fatalf("directories is %T", v)
		return nil
	}
}

func wantDirs(t *testing.T, s *derivedStore, want ...string) {
	t.Helper()
	if got := s.dirs(t); !slices.Equal(got, want) {
		t.Fatalf("directories %v, want %v", got, want)
	}
}

func TestListFieldIsOneFocusStopOfTheItemNoun(t *testing.T) {
	t.Parallel()
	r, _, e, _ := formListFixture(t, "/a", "/b")
	f := e.field("directories")
	if !f.list || r.Instances[f.id].Noun != FormItemNoun {
		t.Fatalf("list %v noun %q", f.list, r.Instances[f.id].Noun)
	}
	text := e.Block(40, 20).Text
	for _, want := range []string{"directories", "/a", "/b", "+ add directory"} {
		if !strings.Contains(text, want) {
			t.Fatalf("form lacks %q:\n%s", want, text)
		}
	}
}

func TestListFieldMoveDeleteYankAndUndo(t *testing.T) {
	t.Parallel()
	r, s, e, m := formListFixture(t, "/a", "/b", "/c")
	mustRun(t, r, Request{Noun: FormNoun, Target: e.field("directories").id, Verb: "item-move-down"})
	wantDirs(t, s, "/b", "/a", "/c")
	if f := e.field("directories"); f.item != 1 {
		t.Fatalf("the cursor stays on the moved item: %d", f.item)
	}
	mustRun(t, r, Request{Noun: FormNoun, Target: e.field("directories").id, Verb: "item-move-up"})
	wantDirs(t, s, "/a", "/b", "/c")
	if _, err := r.Execute(Request{Noun: FormNoun, Target: e.field("directories").id, Verb: "item-move-up"}); err == nil {
		t.Fatal("moving the first item up is refused")
	}
	mustRun(t, r, Request{Noun: FormNoun, Target: e.field("directories").id, Verb: "item-yank"})
	y, err := r.readYank("")
	if err != nil || y.Text != "/a" {
		t.Fatalf("yank %v %v", y, err)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "d")
	keytest.Press(m, "d")
	wantDirs(t, s, "/a", "/c")
	if f := e.field("directories"); f.item != 1 {
		t.Fatalf("the cursor stays on the next item: %d", f.item)
	}
	keytest.Press(m, "u")
	wantDirs(t, s, "/a", "/b", "/c")
	keytest.Press(m, "ctrl+r")
	wantDirs(t, s, "/a", "/c")
}

func TestListFieldEdges(t *testing.T) {
	t.Parallel()
	_, _, e, m := formListFixture(t, "/a", "/b", "/c")
	f := e.field("directories")
	for _, c := range []struct {
		keys []string
		want int
	}{
		{[]string{"G"}, 2},
		{[]string{"g", "g"}, 0},
		{[]string{"j"}, 1},
		{[]string{"G"}, 2},
	} {
		for _, k := range c.keys {
			keytest.Press(m, k)
		}
		if f.item != c.want {
			t.Fatalf("after %v the cursor is %d, want %d", c.keys, f.item, c.want)
		}
	}
}

func TestListFieldMotionsSpillAtTheEnds(t *testing.T) {
	t.Parallel()
	r, _, e, m := formListFixture(t, "/a")
	f := e.field("directories")
	keytest.Press(m, "j") // /a to the add row
	if f.item != 1 || r.Focus != f.id {
		t.Fatalf("cursor %d focus %s", f.item, r.Focus)
	}
	keytest.Press(m, "j") // past the add row: the field below
	if r.Focus != e.field("note").id {
		t.Fatalf("focus %s, want the note field", r.Focus)
	}
	keytest.Press(m, "k") // back up: lands on the add row
	if r.Focus != f.id || f.item != 1 {
		t.Fatalf("focus %s cursor %d", r.Focus, f.item)
	}
	keytest.Press(m, "k")
	keytest.Press(m, "k") // above the first row: the field above
	if r.Focus != e.field("dirs").id {
		t.Fatalf("focus %s, want the field above", r.Focus)
	}
}

func TestListFieldEmptyListAddsFromTheAddRow(t *testing.T) {
	t.Parallel()
	r, s, e, m := formListFixture(t)
	e.field("directories").Items.Type, e.field("directories").Items.Path = SettingString, ""
	keytest.Press(m, "enter") // the add row: add at the end
	if c := r.Chooser(FormItemKind); c == nil {
		t.Fatalf("no item picker for a text item; modal %q", r.TopModal())
	}
	_ = e
	// A plain item type takes any typed text, spaces and all, through the
	// picker's create row.
	keytest.Press(m, "one two")
	keytest.Press(m, "enter")
	wantDirs(t, s, "one two")
}

func TestListFieldTextItemsAddAfterAndEdit(t *testing.T) {
	t.Parallel()
	r, s, e, m := formListFixture(t, "/a", "/c")
	e.field("directories").Items.Type = SettingString
	e.field("directories").Items.Path = ""
	keytest.Press(m, "n") // after /a
	keytest.Press(m, "/b")
	keytest.Press(m, "enter")
	wantDirs(t, s, "/a", "/b", "/c")
	if f := e.field("directories"); f.item != 1 {
		t.Fatalf("cursor %d", f.item)
	}
	keytest.Press(m, "enter") // edit /b: the picker opens on its text
	if r.Chooser(FormItemKind) == nil {
		t.Fatal("edit opened no picker")
	}
	keytest.Press(m, "x")
	keytest.Press(m, "enter")
	wantDirs(t, s, "/a", "/bx", "/c")
	keytest.Press(m, "n")
	keytest.Press(m, "esc")
	wantDirs(t, s, "/a", "/bx", "/c")
}

func TestListFieldPathItemsOpenThePathChooser(t *testing.T) {
	t.Parallel()
	r, _, _, m := formListFixture(t, "/a")
	keytest.Press(m, "enter")
	if r.Chooser(PathChooserKind) == nil {
		t.Fatalf("no path chooser; modal %q", r.TopModal())
	}
	mustRun(t, r, Request{Noun: "ochazuke.picker", Verb: "cancel"})
	if r.Chooser(PathChooserKind) != nil {
		t.Fatal("esc left the path chooser open")
	}
	keytest.Press(m, "n")
	if r.Chooser(PathChooserKind) == nil {
		t.Fatal("add opened no path chooser")
	}
}

func TestListFieldItemVerbsRefuseOffAList(t *testing.T) {
	t.Parallel()
	r, _, e, _ := formListFixture(t, "/a")
	if err := r.SetFocus(e.field("note").id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: FormNoun, Target: e.field("note").id, Verb: "item-delete"}); err == nil {
		t.Fatal("delete off a list field")
	}
}

func TestListFieldSurvivesAnOutsideWrite(t *testing.T) {
	t.Parallel()
	r, s, e, _ := formListFixture(t, "/a")
	mustRun(t, r, Request{Noun: "project", Verb: "set", Args: Arguments{"id": "p1", "props": map[string]any{"directories": []string{"/x", "/y"}}}})
	if got := e.field("directories").listItems(); !reflect.DeepEqual(got, []string{"/x", "/y"}) {
		t.Fatalf("form items %v", got)
	}
	wantDirs(t, s, "/x", "/y")
}

func TestListFieldChoiceItemsPickFromTheirChoices(t *testing.T) {
	t.Parallel()
	r, s, e, m := formListFixture(t, "red")
	it := e.field("directories").Items
	it.Type, it.Path, it.Enum = SettingString, "", []string{"red", "green", "blue"}
	keytest.Press(m, "n")
	if r.Chooser(FormItemKind) == nil {
		t.Fatalf("no picker; modal %q", r.TopModal())
	}
	keytest.Press(m, "gre")
	keytest.Press(m, "enter")
	wantDirs(t, s, "red", "green")
	f := e.field("directories")
	f.value = ItemsValue([]string{"red", "mauve"})
	if e.problem(f) == "" {
		t.Fatal("an item outside the choices is not written")
	}
}
