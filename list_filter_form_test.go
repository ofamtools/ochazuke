package ochazuke

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ofam.dev/ochazuke/keytest"
)

// filterFormFixture is a list of tasks: status repeats (chips), tags is a
// list prop (chips, contains), title is free text, score a number.
func filterFormFixture(t *testing.T) (*Runtime, *Model, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("task").Props(PropSpec{Name: "status"}, PropSpec{Name: "tags", Parameter: Parameter{Type: SettingList}}, PropSpec{Name: "title"}, PropSpec{Name: "score", Parameter: Parameter{Type: "integer"}})
	r.Instance("p")
	rows := []struct {
		id, status, title string
		tags              []string
		score             int
	}{
		{"t1", "todo", "Write notes", []string{"home"}, 1},
		{"t2", "doing", "Buy milk", []string{"shop", "home"}, 2},
		{"t3", "done", "Fix bike", []string{"home"}, 3},
		{"t4", "todo", "Call mum", []string{"phone"}, 4},
		{"t5", "cancelled", "Paint fence", []string{"home"}, 5},
		{"t6", "done", "File taxes", []string{"desk"}, 6},
	}
	var items []ListItem
	for _, row := range rows {
		items = append(items, ListItem{ID: row.id, Label: row.title, Noun: "task", Data: Thing{"status": row.status, "title": row.title, "tags": row.tags, "score": row.score}})
	}
	list, err := r.AddList("p/l", items, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("p", "P", func(r *Runtime, w, h int) Block { return Text(list.View(w, list.Height())) }, Focus("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 90, 30
	return r, m, list
}

func TestFilterFormFieldsAndChips(t *testing.T) {
	r, m, l := filterFormFixture(t)
	pressKeys(m, "z", "/")
	e := r.RecordEditor(listFilterFormID)
	if e == nil {
		t.Fatal("no filter form")
	}
	view := ansi.Strip(m.View().Content)
	t.Log(view)
	for _, want := range []string{"score · is", "status · is", "○ cancelled", "○ todo", "tags · is", "○ home", "title · is"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	kinds := map[string]bool{}
	for _, f := range e.fields {
		kinds[f.Name] = f.chips
	}
	if kinds["score"] || !kinds["status"] || !kinds["tags"] || kinds["title"] {
		t.Fatalf("kinds %v", kinds)
	}
	// j from score (a text field) lands on status's chips: cancelled first.
	pressKeys(m, "j", "space")
	if got := shownIDs(l); !slices.Equal(got, []string{"t5"}) {
		t.Fatalf("one chip: %v %v", got, l.propFilters)
	}
	pressKeys(m, "l", "l", "space") // doing? order is sorted: cancelled doing done todo
	if l.propFilters[0].Op != "in" || !slices.Equal(shownIDs(l), []string{"t3", "t5", "t6"}) {
		t.Fatalf("two chips: %v %v", l.propFilters, shownIDs(l))
	}
	pressKeys(m, "!")
	if l.propFilters[0].Op != "not-in" || !slices.Equal(shownIDs(l), []string{"t1", "t2", "t4"}) {
		t.Fatalf("inverted: %v %v", l.propFilters, shownIDs(l))
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "status · not") || !strings.Contains(view, "● done") {
		t.Fatal(view)
	}
	pressKeys(m, "u")
	if l.propFilters[0].Op != "in" {
		t.Fatalf("undo invert: %v", l.propFilters)
	}
	pressKeys(m, "esc")
	if r.RecordEditor(listFilterFormID) != nil && !r.RecordEditor(listFilterFormID).Closed() {
		t.Fatal("esc kept the form open")
	}
	if len(l.propFilters) != 1 {
		t.Fatalf("esc dropped the conditions: %v", l.propFilters)
	}
	// Reopening shows the conditions: the chips are on.
	pressKeys(m, "z", "/")
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "● cancelled") || !strings.Contains(view, "● done") {
		t.Fatal(view)
	}
	pressKeys(m, "C")
	if len(l.propFilters) != 0 || len(shownIDs(l)) != 6 {
		t.Fatalf("clear: %v", l.propFilters)
	}
}

// chooserChain turns the filter form off: the prop filter key opens the
// prop chooser, then the value chooser.
func chooserChain(t *testing.T, r *Runtime) {
	t.Helper()
	if err := r.Settings().Set(ListFilterFormSetting, false); err != nil {
		t.Fatal(err)
	}
}

// settleAll runs what the runtime scheduled and delivers every message it
// yields (pause timers, searches, writes) until nothing is left.
func settleAll(t *testing.T, r *Runtime) {
	t.Helper()
	for range 8 {
		var msgs []tea.Msg
		var run func(tea.Cmd)
		run = func(cmd tea.Cmd) {
			if cmd == nil {
				return
			}
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					run(c)
				}
				return
			}
			msgs = append(msgs, msg)
		}
		run(r.PendingCommands())
		if len(msgs) == 0 {
			return
		}
		for _, msg := range msgs {
			if _, err := r.Deliver(msg); err != nil {
				t.Fatalf("deliver %T: %v", msg, err)
			}
		}
	}
}

func pressRun(t *testing.T, r *Runtime, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, err := r.Dispatch(MacroKey{Key: k}); err != nil {
			t.Fatalf("key %s: %v", k, err)
		}
		settleAll(t, r)
	}
}

func TestFilterFormTextFieldContainsAndInvert(t *testing.T) {
	r, m, l := filterFormFixture(t)
	pressKeys(m, "z", "/", "j", "j", "j") // score, status, tags, title
	if e := r.RecordEditor(listFilterFormID); e.fieldOn(r.Focus).Name != "title" {
		t.Fatalf("focus %s", r.Focus)
	}
	pressRun(t, r, "i", "i", "k", "esc") // title contains ik: Fix bike
	if len(l.propFilters) != 1 || l.propFilters[0].Op != "contains" || !slices.Equal(shownIDs(l), []string{"t3"}) {
		t.Fatalf("contains: %v %v", l.propFilters, shownIDs(l))
	}
	pressRun(t, r, "!")
	if l.propFilters[0].Op != "not-contains" || !slices.Equal(shownIDs(l), []string{"t1", "t2", "t4", "t5", "t6"}) {
		t.Fatalf("not-contains: %v %v", l.propFilters, shownIDs(l))
	}
	// A number field takes =; a value that is not a number is not written.
	pressRun(t, r, "k", "k", "k", "i", "x", "esc")
	if e := r.RecordEditor(listFilterFormID); e.Problem("score") == "" || len(l.propFilters) != 1 {
		t.Fatalf("bad number written: %v", l.propFilters)
	}
}

func TestFilterFormListPropChipsAreContainsEach(t *testing.T) {
	_, m, l := filterFormFixture(t)
	pressKeys(m, "z", "/", "j", "j") // tags: desk home phone shop
	pressKeys(m, "l", "space", "$", "space")
	if len(l.propFilters) != 2 || l.propFilters[0].Op != "contains" || !slices.Equal(shownIDs(l), []string{"t2"}) {
		t.Fatalf("home and shop: %v %v", l.propFilters, shownIDs(l))
	}
	pressKeys(m, "!")
	if l.propFilters[1].Op != "not-contains" || !slices.Equal(shownIDs(l), []string{"t4", "t6"}) {
		t.Fatalf("neither: %v %v", l.propFilters, shownIDs(l))
	}
}

func TestFilterFormChipEdgesAndSpill(t *testing.T) {
	r, m, _ := filterFormFixture(t)
	pressKeys(m, "z", "/", "j")
	e := r.RecordEditor(listFilterFormID)
	f := e.fieldOn(r.Focus)
	pressKeys(m, "$")
	if f.choices[f.chip].ID != "todo" {
		t.Fatalf("last-col %d", f.chip)
	}
	pressKeys(m, "0")
	if f.chip != 0 {
		t.Fatal("first-col")
	}
	pressKeys(m, "G")
	if f.choices[f.chip].ID != "todo" {
		t.Fatal("last")
	}
	pressKeys(m, "g", "g")
	if f.chip != 0 {
		t.Fatal("first")
	}
	pressKeys(m, "h")
	if f.chip != 0 || e.fieldOn(r.Focus) != f {
		t.Fatal("h at the first chip left it")
	}
	pressKeys(m, "k") // one line: k spills to score
	if e.fieldOn(r.Focus).Name != "score" {
		t.Fatalf("k spill: %s", r.Focus)
	}
	pressKeys(m, "j", "j")
	if e.fieldOn(r.Focus).Name != "tags" {
		t.Fatalf("j spill: %s", r.Focus)
	}
}

func TestFilterFormManyValuesComplete(t *testing.T) {
	r, m, l := filterFormFixture(t)
	if err := r.Settings().Set(ListFilterChipMaxSetting, 3); err != nil {
		t.Fatal(err)
	}
	r.Completer().Debounce = time.Nanosecond
	pressKeys(m, "z", "/", "j") // status: 4 values > 3, a text field
	e := r.RecordEditor(listFilterFormID)
	if f := e.fieldOn(r.Focus); f.Name != "status" || f.chips || f.input == nil {
		t.Fatalf("status is not a text field: %s", r.Focus)
	}
	pressRun(t, r, "i", "d")
	c := r.Completer()
	var shown []string
	for _, s := range c.Suggestions() {
		shown = append(shown, s.Text)
	}
	if !slices.Equal(shown, []string{"doing", "done", "cancelled", "todo"}) {
		t.Fatalf("suggestions %v", shown)
	}
	pressRun(t, r, "ctrl+n", "ctrl+n", "ctrl+p", "enter") // doing, done, doing
	if got := string(e.field("status").input.Text); got != "doing" {
		t.Fatalf("accepted %q", got)
	}
	pressRun(t, r, ",", "space", "t", "tab", "esc")
	if got := string(e.field("status").input.Text); got != "doing, todo" {
		t.Fatalf("second value %q", got)
	}
	settleAll(t, r)
	if len(l.propFilters) != 1 || l.propFilters[0].Op != "in" || !slices.Equal(shownIDs(l), []string{"t1", "t2", "t4"}) {
		t.Fatalf("in: %v %v", l.propFilters, shownIDs(l))
	}
}

func TestFilterFormSavedFilterRoundTrip(t *testing.T) {
	r, m, l := filterFormFixture(t)
	if err := l.SetPropFilters([]PropFilter{{Prop: "status", Op: "not-in", Value: []any{"done", "cancelled"}}, {Prop: "score", Op: "!=", Value: 4}, {Prop: "title", Op: "in", Value: []any{"Buy milk"}}}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveFilter("open"); err != nil {
		t.Fatal(err)
	}
	_ = l.SetPropFilters(nil)
	if err := l.ApplyFilter("open"); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "z", "/")
	e := r.RecordEditor(listFilterFormID)
	values := e.Values()
	if values["status"] != `["cancelled","done"]` || !e.field("status").not || values["score"] != "4" || !e.field("score").not {
		t.Fatalf("form %v", values)
	}
	// title in [...] is not a text field's condition: it is kept as it is.
	pressKeys(m, "j", "space") // status: cancelled off
	want := []PropFilter{{Prop: "title", Op: "in", Value: []any{"Buy milk"}}, {Prop: "score", Op: "!=", Value: 4}, {Prop: "status", Op: "!=", Value: "done"}}
	if got := l.PropFilters(); len(got) != 3 || got[0].Prop != "title" || got[2].Op != want[2].Op {
		t.Fatalf("conditions %v", got)
	}
	if err := l.SaveFilter("open"); err != nil {
		t.Fatal(err)
	}
	_ = l.SetPropFilters(nil)
	if err := l.ApplyFilter("open"); err != nil || !slices.Equal(shownIDs(l), []string{"t2"}) {
		t.Fatalf("reapplied %v %v", err, shownIDs(l))
	}
}

func TestFilterFormInChooser(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	keytest.Press(m, "ctrl+f")
	if r.TopModal() != listFilterFormID {
		t.Fatalf("top %s", r.TopModal())
	}
	pressKeys(m, "space") // by (agent, owner) is the first field
	if got := chooserIDs(c); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("rows %v", got)
	}
	pressKeys(m, "!")
	if got := chooserIDs(c); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("inverted rows %v", got)
	}
	pressKeys(m, "esc")
	if r.TopModal() != "entries" || len(c.PropFilters()) != 1 {
		t.Fatalf("top %s filters %v", r.TopModal(), c.PropFilters())
	}
}

func TestFilterFormOptOut(t *testing.T) {
	r, m, _ := filterFormFixture(t)
	chooserChain(t, r)
	pressKeys(m, "z", "/")
	if r.TopModal() != "list-filter-prop" {
		t.Fatalf("top %s", r.TopModal())
	}
}

func TestCompletionEveryTrigger(t *testing.T) {
	none := func(context.Context, string) ([]Suggestion, error) { return nil, nil }
	if err := validTriggers([]CompletionTrigger{{Every: true, Search: none}}); err != nil {
		t.Fatal(err)
	}
	if err := validTriggers([]CompletionTrigger{{Trigger: "@", Every: true, Search: none}}); err == nil {
		t.Fatal("every with a trigger")
	}
	tok, _, ok := FindCompletionTokenIn([]rune("done, to"), 8, []CompletionTrigger{{Every: true, Boundaries: ","}})
	if !ok || tok.Query != "to" || tok.Start != 6 {
		t.Fatalf("%+v %v", tok, ok)
	}
}

func TestFilterFormTree(t *testing.T) {
	r, tree := propTreeFixture(t)
	if err := r.SetFocus("tree"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop", Args: Arguments{"id": "tree"}}); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor(listFilterFormID)
	if e == nil || !e.field("by").chips {
		t.Fatal("no chips for by")
	}
	pressRun(t, r, "l", "space") // owner
	if !tree.List().shows("deep") || tree.List().shows("other") {
		t.Fatalf("tree filter %v", tree.List().propFilters)
	}
}

func TestFilterFormChipLinesWrap(t *testing.T) {
	r, m, _ := filterFormFixture(t)
	pressKeys(m, "z", "/", "j")
	e := r.RecordEditor(listFilterFormID)
	f := e.fieldOn(r.Focus)
	f.layoutChips(26) // cancelled doing | done todo
	if len(f.lines) != 2 {
		t.Fatalf("lines %v", f.lines)
	}
	f.chip = 1
	if err := e.chipLine(f, 1); err != nil || f.chip != 3 {
		t.Fatalf("down keeps the column: %d", f.chip)
	}
	if err := f.chipEdge(EdgeFirstColumn); err != nil || f.chip != 2 {
		t.Fatalf("first-col of the second line: %d", f.chip)
	}
	if err := e.chipLine(f, -1); err != nil || f.chip != 0 || e.fieldOn(r.Focus) != f {
		t.Fatalf("up a line: %d", f.chip)
	}
}
