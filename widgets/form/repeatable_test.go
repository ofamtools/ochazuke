package form

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
)

func TestAddLabelFor(t *testing.T) {
	cases := []struct {
		spec core.ParamSpec
		want string
	}{
		{core.ParamSpec{Name: "tags", AddLabel: "Label"}, "Label"},
		{core.ParamSpec{Name: "tags"}, "tag"},
		{core.ParamSpec{Name: "paths"}, "path"},
		{core.ParamSpec{Name: "as"}, "as"},
		{core.ParamSpec{Name: "id"}, "id"},
		{core.ParamSpec{Name: ""}, "item"},
	}
	for _, tc := range cases {
		if got := AddLabelFor(tc.spec); got != tc.want {
			t.Errorf("AddLabelFor(%+v) = %q, want %q", tc.spec, got, tc.want)
		}
	}
}

func TestRepeatableNormalizationAndTypedValidation(t *testing.T) {
	specs := []core.ParamSpec{
		{Name: "tags", Type: core.ParamString, Repeatable: true, Required: true},
		{Name: "ports", Type: core.ParamInt, Repeatable: true},
		{Name: "levels", Type: core.ParamString, Enum: []string{"low", "high"}, Repeatable: true},
		{Name: "empty", Type: core.ParamString, Repeatable: true},
	}
	m := New("Norm", specs, map[string]any{
		"tags":   " alpha, beta , ",
		"ports":  []any{80, "443"},
		"levels": []string{"low", " ", "high"},
		"empty":  nil,
	})

	if got := m.Values["tags"]; !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("tags = %#v", got)
	}
	if got := m.Values["ports"]; !reflect.DeepEqual(got, []string{"80", "443"}) {
		t.Fatalf("ports = %#v", got)
	}
	if got := m.Values["empty"]; !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("empty = %#v", got)
	}

	vals, ok := m.Submit()
	if !ok {
		t.Fatalf("Submit errors: %v", m.Errors)
	}
	if !reflect.DeepEqual(vals["tags"], []string{"alpha", "beta"}) {
		t.Fatalf("typed tags = %#v", vals["tags"])
	}
	if !reflect.DeepEqual(vals["ports"], []string{"80", "443"}) {
		t.Fatalf("typed ports = %#v", vals["ports"])
	}
	if !reflect.DeepEqual(vals["levels"], []string{"low", "high"}) {
		t.Fatalf("typed levels = %#v", vals["levels"])
	}
	if _, has := vals["empty"]; has {
		t.Fatalf("empty optional repeatable should be omitted, got %v", vals["empty"])
	}

	// Invalid item fails validation; empty required fails with "required".
	m.SetValue("ports", []string{"80", "notanint"})
	m.SetValue("levels", []string{"bogus"})
	m.SetValue("tags", []string{"  ", ""})
	if m.Validate() {
		t.Fatal("expected validation failure")
	}
	if m.Errors["tags"] != "required" {
		t.Fatalf("tags error = %q, want required", m.Errors["tags"])
	}
	if m.Errors["ports"] == "" || m.Errors["levels"] == "" {
		t.Fatalf("expected ports and levels errors, got %v", m.Errors)
	}
}

func TestRepeatableAddRemoveEditAndSetValue(t *testing.T) {
	specs := []core.ParamSpec{
		{Name: "title", Type: core.ParamString},
		{Name: "paths", Type: core.ParamPath, Repeatable: true, AddLabel: "path"},
		{Name: "refs", Type: core.ParamRef, RefKinds: []string{"task"}, Repeatable: true},
	}
	m := New("Items", specs, map[string]any{"title": "demo"})
	m.SubmitRow = true

	// Layout: 0=title, 1=paths[+Add], 2=refs[+Add], 3=submit.
	m.Down() // idx 1 -> paths add row
	if !m.OnAddRow() || m.ActiveFieldName() != "paths" || m.ActiveItemIndex() != -1 {
		t.Fatalf("expected paths add row: onAdd=%v name=%q item=%d", m.OnAddRow(), m.ActiveFieldName(), m.ActiveItemIndex())
	}

	vBefore := m.View("f")
	if vBefore.Facts["repeatable"] != true || vBefore.Facts["onAddRow"] != true || vBefore.Facts["itemIndex"] != -1 {
		t.Fatalf("unexpected facts on add row: %+v", vBefore.Facts)
	}

	// AddItem via CmdItemAdd appends "", focuses item 0, enters Editing.
	m.HandleCommand(core.Command{ID: CmdItemAdd})
	if m.OnAddRow() || !m.Editing || m.ActiveItemIndex() != 0 || !m.Dirty {
		t.Fatalf("after AddItem: onAdd=%v editing=%v item=%d dirty=%v", m.OnAddRow(), m.Editing, m.ActiveItemIndex(), m.Dirty)
	}
	m.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "/tmp/one"}})
	m.HandleCommand(core.Command{ID: input.CmdLineStart})
	m.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "root:"}})
	m.HandleCommand(core.Command{ID: CmdStop})
	if got := m.Values["paths"]; !reflect.DeepEqual(got, []string{"root:/tmp/one"}) {
		t.Fatalf("after edit item 0: %#v", got)
	}

	// Move down to [+ Add] and activate via CmdEdit to add a second item.
	m.Down()
	if !m.OnAddRow() {
		t.Fatalf("expected add row at idx %d", m.ActiveIdx)
	}
	m.HandleCommand(core.Command{ID: CmdEdit})
	if m.ActiveItemIndex() != 1 || !m.Editing {
		t.Fatalf("expected new item 1 in Editing: item=%d editing=%v", m.ActiveItemIndex(), m.Editing)
	}
	m.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "/tmp/two"}})
	m.HandleCommand(core.Command{ID: CmdStop})

	// Scalar SetValue while focused on item 1 replaces item 1; while focused elsewhere appends.
	m.SetValue("paths", "/picked/two")
	if got := m.Values["paths"]; !reflect.DeepEqual(got, []string{"root:/tmp/one", "/picked/two"}) {
		t.Fatalf("scalar SetValue on active item: %#v", got)
	}
	m.SetValue("refs", core.Ref{Kind: "task", ID: "42"})
	if got := m.Values["refs"]; !reflect.DeepEqual(got, []string{"task:42"}) {
		t.Fatalf("scalar SetValue off-field should append: %#v", got)
	}

	// CacheKey distinguishes ["a b"] vs ["a", "b"].
	m.SetValue("paths", []string{"a b"})
	k1 := m.View("f").CacheKey
	m.SetValue("paths", []string{"a", "b"})
	k2 := m.View("f").CacheKey
	if k1 == k2 {
		t.Fatal("CacheKey collided between [\"a b\"] and [\"a\", \"b\"]")
	}

	// RemoveItem removes focused item and clamps cleanly; removing last item lands on rowAdd.
	m.FocusField("paths")
	if m.ActiveItemIndex() != 0 {
		t.Fatalf("FocusField(paths) landed on item %d", m.ActiveItemIndex())
	}
	m.HandleCommand(core.Command{ID: CmdItemRemove})
	if got := m.Values["paths"]; !reflect.DeepEqual(got, []string{"b"}) || m.ActiveItemIndex() != 0 || m.Editing {
		t.Fatalf("after first RemoveItem: vals=%#v item=%d editing=%v", got, m.ActiveItemIndex(), m.Editing)
	}
	m.HandleCommand(core.Command{ID: CmdItemRemove})
	if got := m.Values["paths"]; !reflect.DeepEqual(got, []string{}) || !m.OnAddRow() {
		t.Fatalf("after last RemoveItem: vals=%#v onAdd=%v", got, m.OnAddRow())
	}
}

func TestRepeatableRenderAndJSONRoundTrip(t *testing.T) {
	specs := []core.ParamSpec{
		{Name: "name", Type: core.ParamString, Required: true},
		{Name: "tags", Type: core.ParamString, Repeatable: true, Required: true},
	}
	m := New("Deploy", specs, map[string]any{
		"name": "svc",
		"tags": []string{"web", "prod"},
	})
	m.SubmitRow = true
	m.Down() // tags[0]
	m.Down() // tags[1]
	m.Down() // tags [+ Add tag]

	node := m.View("f")
	frame, _ := ui.Render(node, ui.DefaultDarkTheme(), "f", 60, 16)
	for _, want := range []string{"name", "tags", "web", "[2]", "prod", "[+ Add tag]"} {
		if !strings.Contains(frame.ANSI, want) {
			t.Errorf("rendered form missing %q:\n%s", want, frame.ANSI)
		}
	}

	// Verify ViewNode renders cleanly after JSON round-trip ([]string -> []any).
	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal ViewNode: %v", err)
	}
	var decoded ui.ViewNode
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal ViewNode: %v", err)
	}
	frame2, _ := ui.Render(decoded, ui.DefaultDarkTheme(), "f", 60, 16)
	for _, want := range []string{"web", "[2]", "prod", "[+ Add tag]"} {
		if !strings.Contains(frame2.ANSI, want) {
			t.Errorf("round-tripped form missing %q:\n%s", want, frame2.ANSI)
		}
	}
}

func TestRepeatablePickOnAddAndItemRows(t *testing.T) {
	specs := []core.ParamSpec{
		{Name: "paths", Type: core.ParamPath, Repeatable: true, AddLabel: "path"},
		{Name: "refs", Type: core.ParamRef, RefKinds: []string{"task", "doc"}, SourceQuery: "items", Repeatable: true},
		{Name: "singleRef", Type: core.ParamRef, RefKinds: []string{"task"}},
	}
	m := New("Pick", specs, map[string]any{
		"refs": []string{"task:1"},
	})
	m.SubmitRow = true

	// Row 0: paths [+ Add path]
	if !m.OnAddRow() || m.ActiveKind() != KindPath {
		t.Fatalf("expected KindPath add row: onAdd=%v kind=%q", m.OnAddRow(), m.ActiveKind())
	}
	res, ok := m.HandleCommand(core.Command{ID: CmdPick})
	if !ok || res.PickPath == nil || res.PickPath.Field != "paths" || !res.PickPath.Repeatable {
		t.Fatalf("CmdPick on repeatable path add row = %+v (ok=%v)", res, ok)
	}

	// Row 1: refs item 0 ("task:1")
	m.Down()
	if m.OnAddRow() || m.ActiveItemIndex() != 0 || m.ActiveKind() != KindRef {
		t.Fatalf("expected refs item 0: onAdd=%v item=%d kind=%q", m.OnAddRow(), m.ActiveItemIndex(), m.ActiveKind())
	}
	res, ok = m.HandleCommand(core.Command{ID: CmdPick})
	if !ok || res.PickRef == nil || res.PickRef.Field != "refs" || !res.PickRef.Repeatable || res.PickRef.SourceQuery != "items" || len(res.PickRef.RefKinds) != 2 {
		t.Fatalf("CmdPick on repeatable ref item row = %+v (ok=%v)", res, ok)
	}

	// Row 2: refs [+ Add ref]
	m.Down()
	if !m.OnAddRow() || m.ActiveKind() != KindRef {
		t.Fatalf("expected KindRef add row: onAdd=%v kind=%q", m.OnAddRow(), m.ActiveKind())
	}
	res, ok = m.HandleCommand(core.Command{ID: CmdPick})
	if !ok || res.PickRef == nil || res.PickRef.Field != "refs" || !res.PickRef.Repeatable || res.PickRef.SourceQuery != "items" {
		t.Fatalf("CmdPick on repeatable ref add row = %+v (ok=%v)", res, ok)
	}

	// Row 3: singleRef (non-repeatable)
	m.Down()
	res, ok = m.HandleCommand(core.Command{ID: CmdPick})
	if !ok || res.PickRef == nil || res.PickRef.Field != "singleRef" || res.PickRef.Repeatable {
		t.Fatalf("CmdPick on non-repeatable ref = %+v (ok=%v)", res, ok)
	}

	// Row 4: SubmitRow emits neither PickPath nor PickRef.
	m.Down()
	res, ok = m.HandleCommand(core.Command{ID: CmdPick})
	if !ok || res.PickPath != nil || res.PickRef != nil {
		t.Fatalf("CmdPick on submit row should not emit pick request: %+v", res)
	}
}
