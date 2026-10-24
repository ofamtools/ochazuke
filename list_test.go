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
	for _, id := range []string{"page", "other", "dialog"} {
		r.Instance(id)
	}
	r.Instance("editor", Parent("page"))
	for _, noun := range []string{"a", "b", "c"} {
		r.Define(noun)
	}
	input := NewInput("note", "", false)
	if err := r.AddInput(input, Parent("editor"), Kind("prompt")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"page", "other"} {
		page := &Page{ID: id, Title: id, Render: func(*Runtime, int, int) Block { return Text("page") }}
		if err := r.RegisterPage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	items := []ListItem{{ID: "one", Label: "First item", Noun: "a"}, {ID: "two", Label: "Second item", Noun: "b"}, {ID: "three", Label: "Third item", Noun: "c"}}
	list, err := NewList(r, "sidebar", items, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	return r, list, input, items
}

func TestListFocusSelectsItemActionsAndLeavesEditorAlone(t *testing.T) {
	r, list, input, _ := listFixture(t)
	r.Register(Operation{Verb: "inspect", Noun: "item", Run: func(*Runtime, Context, Arguments) (any, error) { return "application", nil }})
	for _, id := range []string{"a", "b"} {
		r.Define(id).Action("item.inspect", func(*Runtime, Context, Arguments) (any, error) { return id, nil })
	}
	if _, err := r.Execute(Request{Verb: "focus", Noun: "list", Args: Arguments{"id": "sidebar"}}); err != nil {
		t.Fatal(err)
	}
	value, err := r.Execute(Request{Verb: "inspect", Noun: "item"})
	if err != nil || value != "a" {
		t.Fatalf("first item action: %v, %v", value, err)
	}
	editorTestKeys(t, r, "j")
	if got := r.Context(); !reflect.DeepEqual(got, Context{Path: []string{"app", "page", "sidebar", "two"}}) {
		t.Fatalf("row was stacked instead of replaced: %+v", got)
	}
	value, err = r.Execute(Request{Verb: "inspect", Noun: "item"})
	if err != nil || value != "b" {
		t.Fatalf("second item action: %v, %v", value, err)
	}
	if err := r.SetFocus(input.ID); err != nil {
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
	if selected.ID != "two" || r.Context().Path[3] != "two" {
		t.Fatal("refresh followed the index instead of identity")
	}
	if err := r.Push("dialog", "dialog"); err != nil {
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
	if !slices.Equal(r.Context().Path, []string{"app", "page", "sidebar", "one"}) {
		t.Fatalf("modal restored removed row: %+v", r.Context())
	}
	if r.Instances["two"] != nil || r.rows["two"] != "" || r.Instances["one"].Parent != "sidebar" || r.Instances["one"].Noun != "a" {
		t.Fatalf("rows are instances owned by SetItems: %+v %+v", r.Instances["one"], r.Instances["two"])
	}
	if err := r.OpenPage("other"); err != nil {
		t.Fatal(err)
	}
	if err := list.SetItems(nil); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err == nil || r.Page != "other" {
		t.Fatal("list bypassed cross-page focus validation")
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Context().Path, []string{"app", "page", "sidebar"}) {
		t.Fatalf("empty hidden list restored a row: %+v", r.Context())
	}
	if _, ok := list.Selected(); ok {
		t.Fatal("empty list retained selection")
	}
	list.Move(math.MinInt)
	if err := list.SetItems(items[:1]); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Context().Path, []string{"app", "page", "sidebar", "one"}) {
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
		{{ID: "", Noun: "a"}},
		{items[0], items[0]},
		{{ID: "missing", Noun: "unknown"}},
		{{ID: "sidebar", Noun: "a"}},
		{{ID: "note", Noun: "a"}},
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
	snapshot[0].Noun = "unknown"
	selected, _ := list.Selected()
	if selected.ID != "one" || selected.Noun != "a" {
		t.Fatal("caller mutated owned rows")
	}
}

func TestListKeepsSelectionVisibleAcrossResizesAndExtremeMoves(t *testing.T) {
	_, list, _, _ := listFixture(t)
	items := make([]ListItem, 20)
	for i := range items {
		items[i] = ListItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("Row %02d", i), Noun: "a"}
	}
	if err := list.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		delta, width, height int
		visible              string
	}{
		{math.MaxInt, 20, 5, "Row 19"}, {-1, 20, 3, "Row 18"}, {math.MinInt, 20, 10, "Row 00"}, {12, 20, 1, "Row 12"},
	} {
		list.Move(step.delta)
		view := list.View(step.width, step.height)
		if got := highlightedRows(view); len(got) != 1 || got[0] != step.visible {
			t.Fatalf("selection hidden (highlighted %q):\n%s", got, view)
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
	if err := list.SetItems([]ListItem{{ID: "one", Noun: "a"}, {ID: "two", Noun: "a"}}); err != nil {
		t.Fatal(err)
	}
	r.Define("a").Mode("").Bind("g g", Do("app.quit"))
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

func TestListItemDataReachesTheRowInstanceAndItsVerb(t *testing.T) {
	r, list, _, _ := listFixture(t)
	var seen any
	r.Define("a").Verb("open", func(c Ctx) Result { seen = c.Instance.Data; return Done(nil) }, "Open")
	if err := list.SetItems([]ListItem{{ID: "one", Label: "First", Noun: "a", Data: map[string]any{"id": "ext-1"}}, {ID: "two", Label: "Second", Noun: "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "open", Noun: "a", Target: "one"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, map[string]any{"id": "ext-1"}) || r.Instances["two"].Data != nil {
		t.Fatalf("data = %v, two = %v", seen, r.Instances["two"].Data)
	}
	if item, _ := list.Selected(); !reflect.DeepEqual(item.Data, map[string]any{"id": "ext-1"}) {
		t.Fatalf("selected = %+v", item)
	}
	// A retained row takes the new data.
	if err := list.SetItems([]ListItem{{ID: "one", Label: "First", Noun: "a", Data: "v2"}}); err != nil {
		t.Fatal(err)
	}
	if r.Instances["one"].Data != "v2" {
		t.Fatalf("retained row data = %v", r.Instances["one"].Data)
	}
}

func TestListItemsAndLen(t *testing.T) {
	_, list, _, items := listFixture(t)
	if list.Len() != 3 || !reflect.DeepEqual(list.Items(), items) {
		t.Fatalf("len %d, items %+v", list.Len(), list.Items())
	}
	got := list.Items()
	got[0].Label = "changed"
	if list.Items()[0].Label != "First item" {
		t.Fatal("Items exposed the list's own slice")
	}
	if err := list.SetItems(nil); err != nil || list.Len() != 0 || len(list.Items()) != 0 {
		t.Fatalf("empty: %v, len %d", err, list.Len())
	}
}

// highlightedRows are the stripped, trimmed rows of view drawn in reverse
// video: the focused list's selected row.
func highlightedRows(view string) []string {
	var out []string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "\x1b[7m") {
			out = append(out, strings.TrimSpace(ansi.Strip(row)))
		}
	}
	return out
}

// Only the focused list shows its selected row, by the highlight alone: no
// list draws a cursor marker, and an unfocused list's rows are plain. Marked
// rows take a "* " gutter, which every row gets while the list has a mark.
func TestListOnlyTheFocusedListHighlightsAndNoneDrawsAMarker(t *testing.T) {
	r, list, _, items := listFixture(t)
	other, err := NewList(r, "other-list", []ListItem{{ID: "x", Label: "Ex", Noun: "a"}, {ID: "y", Label: "Why", Noun: "a"}}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	if got := highlightedRows(list.View(30, 3)); !reflect.DeepEqual(got, []string{"First item"}) {
		t.Fatalf("focused list highlights %q", got)
	}
	plain := other.View(30, 2)
	if got := highlightedRows(plain); got != nil || ansi.Strip(plain) != plain {
		t.Fatalf("unfocused list is styled: %q", plain)
	}
	if rows := strings.Split(ansi.Strip(list.View(30, 3)+"\n"+plain), "\n"); !strings.HasPrefix(rows[0], "First item") || !strings.HasPrefix(rows[3], "Ex") {
		t.Fatalf("rows do not start at the left edge or carry a marker: %q", rows)
	}
	if err := list.ToggleMark(items[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(ansi.Strip(list.View(30, 3)), "\n"); strings.TrimRight(got[0], " ") != "  First item" || strings.TrimRight(got[1], " ") != "* Second item" {
		t.Fatalf("marked gutter: %q", got)
	}
	if err := other.Focus(); err != nil {
		t.Fatal(err)
	}
	if highlightedRows(list.View(30, 3)) != nil || !reflect.DeepEqual(highlightedRows(other.View(30, 2)), []string{"Ex"}) {
		t.Fatal("the highlight did not follow the focus")
	}
}

// BenchmarkFocusNextOverManyRows is } across lists of thousands of rows:
// the focus walk indexes children once per walk, not once per node.
func BenchmarkFocusNextOverManyRows(b *testing.B) {
	r := NewRuntime()
	r.Define("a")
	if err := r.RegisterPage(&Page{ID: "page", Title: "page", Render: func(*Runtime, int, int) Block { return Text("") }}); err != nil {
		b.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		b.Fatal(err)
	}
	for l := 0; l < 4; l++ {
		items := make([]ListItem, 1000)
		for i := range items {
			items[i] = ListItem{ID: fmt.Sprintf("l%d/%d", l, i), Label: "row", Noun: "a"}
		}
		if _, err := NewList(r, fmt.Sprintf("l%d", l), items, Parent("page")); err != nil {
			b.Fatal(err)
		}
	}
	if err := r.List("l0").Focus(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Execute(Request{Noun: "focus", Verb: "next"}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestListWindowKeepsScrolloffAroundTheSelection(t *testing.T) {
	for _, c := range []struct {
		name                                   string
		offset, selected, visible, total, want int
	}{
		{"moving down stops two rows short of the bottom edge", 0, 5, 7, 50, 1},
		{"moving up stops two rows short of the top edge", 20, 21, 7, 50, 19},
		{"a selection inside the margins does not scroll", 10, 13, 7, 50, 10},
		{"the margin relaxes at the first row", 5, 0, 7, 50, 0},
		{"the margin relaxes at the last row", 0, 49, 7, 50, 43},
		{"near the bottom the window stops at the last row", 40, 48, 7, 50, 43},
		{"a 3-row window caps the margin at one", 0, 3, 3, 50, 2},
		{"a 4-row window caps the margin at one", 0, 3, 4, 50, 1},
		{"a 1-row window shows the selection alone", 0, 7, 1, 50, 7},
		{"a list shorter than its window never scrolls", 0, 4, 7, 5, 0},
	} {
		if got := windowOffset(c.offset, c.selected, c.visible, c.total, 2); got != c.want {
			t.Errorf("%s: windowOffset(%d, %d, %d, %d, 2) = %d, want %d", c.name, c.offset, c.selected, c.visible, c.total, got, c.want)
		}
	}
}

func TestListViewKeepsTwoRowsBelowTheSelection(t *testing.T) {
	_, list, _, _ := listFixture(t)
	items := make([]ListItem, 30)
	for i := range items {
		items[i] = ListItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("Row %02d", i), Noun: "a"}
	}
	if err := list.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	list.Move(10)
	rows := strings.Split(list.View(20, 8), "\n")
	if got := strings.TrimSpace(ansi.Strip(rows[4])); got != "Row 10" {
		t.Fatalf("selected row on line 4 = %q, want Row 10:\n%s", got, strings.Join(rows, "\n"))
	}
	if got := strings.TrimSpace(ansi.Strip(rows[6])); got != "Row 12" {
		t.Fatalf("last window row = %q, want Row 12", got)
	}
}
