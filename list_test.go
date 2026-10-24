package ochazuke

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func listFixture(t *testing.T) (*Runtime, *List, *Input, []ListItem) {
	t.Helper()
	r := NewRuntime()
	for _, id := range []string{"page", "sidebar", "editor", "a", "b", "c", "other", "dialog"} {
		r.Layers[id] = &Layer{ID: id, Actions: map[string]Handler{}}
	}
	input := NewInput("note", "", false)
	input.ReadlineOnly = true
	if err := r.AddInput(input, []string{"app", "page", "editor"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"page", "other"} {
		page := &Page{ID: id, Title: id, Frame: &Frame{ID: id, Context: Context{Path: []string{"app", id}}}, Render: func(*Runtime, int, int) string { return "page" }}
		if err := r.RegisterPage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	items := []ListItem{{ID: "one", Label: "First item", Layer: "a"}, {ID: "two", Label: "Second item", Layer: "b"}, {ID: "three", Label: "Third item", Layer: "c"}}
	list, err := NewList(r, []string{"app", "page", "sidebar"}, items)
	if err != nil {
		t.Fatal(err)
	}
	return r, list, input, items
}

func TestListFocusSelectsItemActionsAndLeavesEditorAlone(t *testing.T) {
	r, list, input, _ := listFixture(t)
	r.Register(Operation{Verb: "inspect", Noun: "item", Run: func(*Runtime, Context, Arguments) (any, error) { return "application", nil }})
	for _, id := range []string{"a", "b"} {
		r.Layers[id].Actions["inspect.item"] = func(*Runtime, Context, Arguments) (any, error) { return id, nil }
	}
	if _, err := r.Execute(Request{Verb: "focus", Noun: "list", Args: Arguments{"id": "sidebar"}}); err != nil {
		t.Fatal(err)
	}
	value, err := r.Execute(Request{Verb: "inspect", Noun: "item"})
	if err != nil || value != "a" {
		t.Fatalf("first item action: %v, %v", value, err)
	}
	editorTestKeys(t, r, "j")
	if got := r.Context(); !reflect.DeepEqual(got, Context{Path: []string{"app", "page", "sidebar", "b"}}) {
		t.Fatalf("item layer was stacked instead of replaced: %+v", got)
	}
	value, err = r.Execute(Request{Verb: "inspect", Noun: "item"})
	if err != nil || value != "b" {
		t.Fatalf("second item action: %v, %v", value, err)
	}
	if err := r.Focus(input.ID); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "j", "k", "x")
	if string(input.Text) != "jkx" || list.Focused() {
		t.Fatalf("unfocused list intercepted editor: %q, focused=%v", input.Text, list.Focused())
	}
	list.Move(1)
	if got := r.Context(); !reflect.DeepEqual(got, Context{Path: []string{"app", "page", "editor"}, InputID: "note"}) {
		t.Fatalf("background selection stole editor focus: %+v", got)
	}
	value, err = r.Execute(Request{Verb: "inspect", Noun: "item"})
	if err != nil || value != "application" {
		t.Fatalf("old item action survived blur: %v, %v", value, err)
	}
}

func TestListRefreshRetainsIdentityAndRestoresHiddenContexts(t *testing.T) {
	r, list, _, items := listFixture(t)
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	if err := list.Select("two"); err != nil {
		t.Fatal(err)
	}
	if err := list.SetItems([]ListItem{items[2], items[0], items[1]}); err != nil {
		t.Fatal(err)
	}
	selected, _ := list.Selected()
	if selected.ID != "two" || r.Context().Path[3] != "b" {
		t.Fatal("refresh followed the index instead of identity")
	}
	if err := r.Push(&Frame{ID: "dialog", Context: Context{Path: []string{"app", "dialog"}}}); err != nil {
		t.Fatal(err)
	}
	if err := list.SetItems([]ListItem{items[2], items[0]}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Context().Path, []string{"app", "dialog"}) || list.Focused() {
		t.Fatal("refresh stole modal focus")
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Context().Path, []string{"app", "page", "sidebar", "a"}) {
		t.Fatalf("modal restored removed row: %+v", r.Context())
	}
	if err := r.OpenPage("other"); err != nil {
		t.Fatal(err)
	}
	if err := list.SetItems(nil); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err == nil || r.ActivePage != "other" {
		t.Fatal("list bypassed cross-page focus validation")
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Context().Path, []string{"app", "page", "sidebar"}) {
		t.Fatalf("empty hidden list restored an item layer: %+v", r.Context())
	}
	if _, ok := list.Selected(); ok {
		t.Fatal("empty list retained selection")
	}
	list.Move(math.MinInt)
	if err := list.SetItems(items[:1]); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Context().Path, []string{"app", "page", "sidebar", "a"}) {
		t.Fatal("repopulating a focused empty list did not attach its row")
	}
}

func TestListInvalidRefreshIsAtomicAndRowsAreOwned(t *testing.T) {
	r, list, _, items := listFixture(t)
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	before := list.Snapshot()
	context := r.Context()
	for _, bad := range [][]ListItem{
		{{ID: "", Layer: "a"}},
		{items[0], items[0]},
		{{ID: "missing", Layer: "unknown"}},
		{{ID: "cycle", Layer: "sidebar"}},
	} {
		if err := list.SetItems(bad); err == nil {
			t.Fatal("accepted invalid rows")
		}
		if !reflect.DeepEqual(list.Snapshot(), before) || !reflect.DeepEqual(r.Context(), context) {
			t.Fatal("invalid refresh changed state or focus")
		}
	}
	if err := list.Select("unknown"); err == nil {
		t.Fatal("accepted unknown item")
	}
	items[0].ID = "external mutation"
	snapshot := list.Snapshot()["items"].([]ListItem)
	snapshot[0].Layer = "unknown"
	selected, _ := list.Selected()
	if selected.ID != "one" || selected.Layer != "a" {
		t.Fatal("caller mutated owned rows")
	}
}

func TestListKeepsSelectionVisibleAcrossResizesAndExtremeMoves(t *testing.T) {
	_, list, _, _ := listFixture(t)
	items := make([]ListItem, 20)
	for i := range items {
		items[i] = ListItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("Row %02d", i), Layer: "a"}
	}
	if err := list.SetItems(items); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		delta, width, height int
		visible              string
	}{
		{math.MaxInt, 20, 5, "> Row 19"}, {-1, 20, 3, "> Row 18"}, {math.MinInt, 20, 10, "> Row 00"}, {12, 20, 1, "> Row 12"},
	} {
		list.Move(step.delta)
		view := list.View(step.width, step.height)
		if !strings.Contains(ansi.Strip(view), step.visible) {
			t.Fatalf("selection hidden:\n%s", view)
		}
		rows := strings.Split(view, "\n")
		if len(rows) != step.height {
			t.Fatal("list overflowed viewport height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != step.width {
				t.Fatal("list overflowed viewport width")
			}
		}
	}
}

func TestListSelectionCancelsPendingBindingEvenWithSharedItemLayer(t *testing.T) {
	r, list, _, _ := listFixture(t)
	if err := list.SetItems([]ListItem{{ID: "one", Layer: "a"}, {ID: "two", Layer: "a"}}); err != nil {
		t.Fatal(err)
	}
	r.Layers["a"].Bindings = []Binding{{Keys: "g g", Request: Request{Verb: "quit", Noun: "app"}}}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "g")
	if r.Pending == "" {
		t.Fatal("binding prefix was not pending")
	}
	list.Move(1)
	if r.Pending != "" {
		t.Fatal("old item's pending binding survived selection")
	}
}
