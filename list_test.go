package ochazuke

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
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
	if err := r.AddInput(input, Parent("editor"), Kind("ochazuke.prompt")); err != nil {
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
	if _, err := r.Execute(Request{Verb: "focus", Noun: "ochazuke.list", Args: Arguments{"id": "sidebar"}}); err != nil {
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

// A list shows its selected row by the highlight alone: no list draws a
// cursor marker, and a page list keeps its selected row reversed while
// another list has the focus (without a Model there is no fade). Marked
// rows take a "* " gutter, which every row gets while the list has a mark.
func TestListSelectedRowIsTheHighlightAloneAndNoListDrawsAMarker(t *testing.T) {
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
	unfocused := other.View(30, 2)
	if got := highlightedRows(unfocused); !reflect.DeepEqual(got, []string{"Ex"}) {
		t.Fatalf("unfocused page list highlights %q, want its selected row", got)
	}
	if rows := strings.Split(ansi.Strip(list.View(30, 3)+"\n"+unfocused), "\n"); !strings.HasPrefix(rows[0], "First item") || !strings.HasPrefix(rows[3], "Ex") || !strings.HasPrefix(rows[4], "Why") {
		t.Fatalf("rows do not start at the left edge or carry a marker: %q", rows)
	}
	if err := list.ToggleMark(items[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(ansi.Strip(list.View(30, 3)), "\n"); strings.TrimRight(got[0], " ") != "  First item" || strings.TrimRight(got[1], " ") != "* Second item" {
		t.Fatalf("marked gutter: %q", got)
	}
	if err := other.Select("y"); err != nil {
		t.Fatal(err)
	}
	if got := highlightedRows(other.View(30, 2)); !reflect.DeepEqual(got, []string{"Why"}) {
		t.Fatalf("unfocused list after a select highlights %q", got)
	}
	if err := other.Focus(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(highlightedRows(list.View(30, 3)), []string{"First item"}) || !reflect.DeepEqual(highlightedRows(other.View(30, 2)), []string{"Why"}) {
		t.Fatal("a list lost its selected row when the focus moved")
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
		if _, err := r.Execute(Request{Noun: "ochazuke.focus", Verb: "next"}); err != nil {
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

// Focusing a row instance selects it in its list, however the focus got
// there (focus.set, RetargetFocus, a FocusOnClose landing): the highlight,
// Selected and the keys that answer agree.
func TestFocusingARowSelectsIt(t *testing.T) {
	r := NewRuntime()
	r.Define("a")
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	var items []ListItem
	for _, id := range []string{"r1", "r2", "r3", "r4"} {
		items = append(items, ListItem{ID: id, Label: "row " + id, Noun: "a"})
	}
	list, err := r.AddList("rows", items, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	selected := func(want string) {
		t.Helper()
		if got, _ := list.Selected(); got.ID != want || r.Focus != want {
			t.Fatalf("selected %q, focus %q, want %q", got.ID, r.Focus, want)
		}
		if got := highlightedRows(list.View(30, 4)); !reflect.DeepEqual(got, []string{"row " + want}) {
			t.Fatalf("highlighted %q, want row %s", got, want)
		}
	}
	if err := r.SetFocus("r3"); err != nil {
		t.Fatal(err)
	}
	selected("r3")
	if _, err := r.Execute(Request{Noun: "ochazuke.focus", Verb: "set", Args: Arguments{"id": "r2"}}); err != nil {
		t.Fatal(err)
	}
	selected("r2")
	r.RetargetFocus(func(id string) bool { return id == "r2" }, "r4")
	selected("r4")
	// A modal that lands back on a row when it closes selects that row.
	r.Instance("sheet", Parent("page"), FocusOnClose("r1"))
	if err := r.SetFocus("sheet"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("sheet"); err != nil {
		t.Fatal(err)
	}
	selected("r1")
}

// A list (or tree) added with NoMarks is a read-only view: space does not
// mark its rows (list.mark is guarded off, so the row is skipped and left
// off the hints), list.mark refuses, and a plain list still marks.
func TestNoMarksListTakesNoMarks(t *testing.T) {
	r := NewRuntime()
	r.Define("a")
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	view, err := r.AddList("view", []ListItem{{ID: "v1", Label: "v", Noun: "a"}}, Parent("page"), NoMarks())
	if err != nil {
		t.Fatal(err)
	}
	marks, err := r.AddList("marks", []ListItem{{ID: "m1", Label: "m", Noun: "a"}}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := r.AddTree("tree", []TreeNode{{ID: "t1", Label: "t"}}, Parent("page"), NoMarks())
	if err != nil {
		t.Fatal(err)
	}
	answers := func(focus string) bool {
		return slices.ContainsFunc(r.ListBindings(focus), func(b Binding) bool { return b.Keys == "space" })
	}
	for _, tc := range []struct {
		focus string
		list  *List
		marks bool
	}{{"v1", view, false}, {"m1", marks, true}, {tree.FocusTarget(), tree.List(), false}} {
		if err := r.SetFocus(tc.focus); err != nil {
			t.Fatal(err)
		}
		if answers(tc.focus) != tc.marks {
			t.Fatalf("%s: space listed = %v, want %v", tc.focus, !tc.marks, tc.marks)
		}
		if _, err := r.Dispatch(MacroKey{Key: "space"}); err != nil && tc.marks {
			t.Fatalf("%s: space: %v", tc.focus, err)
		}
		if got := len(tc.list.Marked()) > 0; got != tc.marks {
			t.Fatalf("%s: marked = %v, want %v", tc.focus, got, tc.marks)
		}
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "mark", Args: Arguments{"id": "view", "item": "v1"}}); err == nil {
		t.Fatal("list.mark marked a NoMarks list")
	}
}

// reverseRows classifies view's rows that hold reverse video: full is the
// stripped rows whose reverse is at full strength, faint those whose
// reverse is under the faint attribute (an unfocused list's selected row
// in mono), following the SGR state along each row.
func reverseRows(view string) (full, faint []string) {
	for _, row := range strings.Split(view, "\n") {
		dim, isFull, isFaint := false, false, false
		for rest := row; ; {
			i := strings.Index(rest, "\x1b[")
			if i < 0 {
				break
			}
			rest = rest[i+2:]
			end := strings.IndexByte(rest, 'm')
			if end < 0 || strings.ContainsFunc(rest[:end], func(c rune) bool { return (c < '0' || c > '9') && c != ';' }) {
				continue
			}
			for _, p := range strings.Split(rest[:end], ";") {
				switch p {
				case "", "0", "22":
					dim = false
				case "2":
					dim = true
				case "7":
					isFull, isFaint = isFull || !dim, isFaint || dim
				}
			}
			rest = rest[end+1:]
		}
		label := strings.TrimSpace(ansi.Strip(row))
		if isFull {
			full = append(full, label)
		} else if isFaint {
			faint = append(faint, label)
		}
	}
	return full, faint
}

// dimFixture is a Model in the mono theme over a page with two lists, the
// first focused, and a settings directory so the dim settings can be set.
func dimFixture(t *testing.T) (*Runtime, *Model, *List, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	r.Define("item")
	r.Instance("main", Parent("app"), Kind("ochazuke.page"))
	left, err := r.AddList("left", []ListItem{{ID: "a", Label: "Alpha", Noun: "item"}, {ID: "b", Label: "Beta", Noun: "item"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := r.AddList("right", []ListItem{{ID: "x", Label: "Ex", Noun: "item"}, {ID: "y", Label: "Why", Noun: "item"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPage(&Page{ID: "main", Title: "Main", Focus: "left", Render: func(r *Runtime, w, h int) Block {
		return Text(r.List("left").View(w, 2) + "\n" + r.List("right").View(w, 2))
	}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 40, 12
	m.Init()
	m.Appearance.Theme = "mono"
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("a"); err != nil {
		t.Fatal(err)
	}
	return r, m, left, right
}

// An unfocused page list keeps its selected row, faded with the list
// (faint in mono), so the focused list's row is the one at full strength;
// dim-unfocused 0 turns the fade off and the row is plain reverse.
func TestUnfocusedPageListKeepsItsRowFaded(t *testing.T) {
	r, m, left, right := dimFixture(t)
	if full, faint := reverseRows(left.View(30, 2)); len(full) != 1 || full[0] != "Alpha" || faint != nil {
		t.Fatalf("focused list: full %q faint %q", full, faint)
	}
	if full, faint := reverseRows(right.View(30, 2)); full != nil || len(faint) != 1 || faint[0] != "Ex" {
		t.Fatalf("unfocused list: full %q faint %q", full, faint)
	}
	if !strings.Contains(right.View(30, 2), "\x1b[2m") {
		t.Fatal("the unfocused list is not faint")
	}
	if full, faint := reverseRows(m.View().Content); len(full) != 1 || full[0] != "Alpha" || len(faint) != 1 || faint[0] != "Ex" {
		t.Fatalf("frame: full %q faint %q", full, faint)
	}
	if err := r.Settings().Set(DimUnfocusedSetting, 0); err != nil {
		t.Fatal(err)
	}
	view := right.View(30, 2)
	if full, faint := reverseRows(view); len(full) != 1 || full[0] != "Ex" || faint != nil || strings.Contains(view, "\x1b[2m") {
		t.Fatalf("dim-unfocused 0: full %q faint %q in %q", full, faint, view)
	}
	if err := r.Settings().Set(DimUnfocusedSetting, MaxDim+1); err == nil {
		t.Fatal("dim-unfocused took a value over MaxDim")
	}
}

func TestUnfocusedSelectionPredicate(t *testing.T) {
	for _, table := range []bool{false, true} {
		name := "list"
		if table {
			name = "table"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, _, _, right := dimFixture(t)
			if table {
				right.SetColumns(Column{Title: "Item"})
			}
			show := false
			right.UnfocusedSelection = func() bool { return show }
			view := right.View(30, 4)
			if full, faint := reverseRows(view); full != nil || faint != nil {
				t.Fatalf("hidden remembered cursor: full %q faint %q", full, faint)
			}
			if plain := ansi.Strip(view); !strings.Contains(plain, "Ex") || !strings.Contains(plain, "Why") {
				t.Fatalf("hiding the cursor hid rows: %q", plain)
			}
			if selected, ok := right.Selected(); !ok || selected.ID != "x" || r.Focus != "a" {
				t.Fatalf("render changed selection/focus: selected=%+v focus=%q", selected, r.Focus)
			}
			show = true
			if full, faint := reverseRows(right.View(30, 4)); full != nil || len(faint) != 1 || faint[0] != "Ex" {
				t.Fatalf("shown remembered cursor: full %q faint %q", full, faint)
			}
			show = false
			if err := r.SetFocus("y"); err != nil {
				t.Fatal(err)
			}
			if full, faint := reverseRows(right.View(30, 4)); len(full) != 1 || full[0] != "Why" || faint != nil {
				t.Fatalf("focused cursor must remain visible: full %q faint %q", full, faint)
			}
			if err := r.SetFocus("a"); err != nil {
				t.Fatal(err)
			}
			if full, faint := reverseRows(right.View(30, 4)); full != nil || faint != nil {
				t.Fatalf("leaving focus must hide the cursor: full %q faint %q", full, faint)
			}
			right.UnfocusedSelection = nil
			if full, faint := reverseRows(right.View(30, 4)); full != nil || len(faint) != 1 || faint[0] != "Why" {
				t.Fatalf("restoring default must retain the selected row: full %q faint %q", full, faint)
			}
		})
	}
}

// A list in a modal keeps the older rule: its selected row is plain until
// the list has the focus, so a modal shows one cursor.
func TestUnfocusedModalListDrawsPlain(t *testing.T) {
	r, _, _, _ := dimFixture(t)
	if _, err := r.OpenModal("pick", Payload{}); err != nil {
		t.Fatal(err)
	}
	first, err := r.AddList("pick/first", []ListItem{{ID: "p1", Label: "One", Noun: "item"}}, Parent("pick"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.AddList("pick/second", []ListItem{{ID: "q1", Label: "Uno", Noun: "item"}}, Parent("pick"))
	if err != nil {
		t.Fatal(err)
	}
	first.UnfocusedSelection = func() bool { return false }
	second.UnfocusedSelection = func() bool { return true }
	if err := r.SetFocus("p1"); err != nil {
		t.Fatal(err)
	}
	if full, _ := reverseRows(first.View(30, 1)); len(full) != 1 || full[0] != "One" {
		t.Fatalf("focused modal list: %q", full)
	}
	if view := second.View(30, 1); ansi.Strip(view) != view {
		t.Fatalf("unfocused modal list is styled: %q", view)
	}
}

// A tree whose line is being edited holds the focus in its input, not in
// its list, yet it is the list being typed in: it is not faded. A tree
// whose focus is elsewhere is.
func TestTreeEditingALineIsNotFaded(t *testing.T) {
	r, m, _, _ := dimFixture(t)
	tree, err := r.AddTree("outline", []TreeNode{{ID: "n1", Label: "one"}, {ID: "n2", Label: "two"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	tree.OnChange = func(TreeChange) error { return nil }
	if !strings.Contains(tree.View(30, 3), "\x1b[2m") {
		t.Fatal("an unfocused tree is not faded")
	}
	if err := r.SetFocus("n1"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "o")
	if tree.Editing() == "" || r.Focus == "n1" {
		t.Fatalf("o: editing %q focus %q status %q", tree.Editing(), r.Focus, m.Status)
	}
	if view := tree.View(30, 3); strings.Contains(view, "\x1b[2m") {
		t.Fatalf("the tree being edited is faded: %q", view)
	}
}
